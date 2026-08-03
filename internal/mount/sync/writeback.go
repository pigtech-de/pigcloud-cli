package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/vfs"
)

const (
	debounceDelay = 2 * time.Second

	maxRetries = 3

	processBatchSize = 10
)

type WritebackProcessor struct {
	vfs      *vfs.VFS
	client   *api.Client
	cacheDB  *cache.DB
	store    *cache.Store
	syncDir  string
	activity ActivityFunc

	cancel context.CancelFunc
}

func (w *WritebackProcessor) SetActivityCallback(fn ActivityFunc) {
	w.activity = fn
}

func NewWritebackProcessor(v *vfs.VFS, client *api.Client, cacheDB *cache.DB, store *cache.Store, syncDir string) *WritebackProcessor {
	return &WritebackProcessor{
		vfs:     v,
		client:  client,
		cacheDB: cacheDB,
		store:   store,
		syncDir: syncDir,
	}
}

func (w *WritebackProcessor) localPath(remotePath string) (string, bool) {
	if w.syncDir == "" || remotePath == "" {
		return "", false
	}
	rel := remotePath
	if base := w.vfs.RemoteBase; base != "" {
		rel = strings.TrimPrefix(remotePath, base+"/")
	}
	p := filepath.Join(w.syncDir, filepath.FromSlash(rel))
	r, err := filepath.Rel(w.syncDir, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

func (w *WritebackProcessor) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(ctx)
}

func (w *WritebackProcessor) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *WritebackProcessor) FlushAll(timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	flushed := 0
	for {
		entries, err := w.cacheDB.DequeueWriteback(processBatchSize, 0)
		if err != nil {
			return flushed, err
		}
		if len(entries) == 0 {
			break
		}

		var held []*cache.WritebackEntry
		for i, entry := range entries {
			if ctx.Err() != nil {
				w.requeue(entries[i:])
				w.requeue(held)
				return flushed, fmt.Errorf("flush timeout with entries remaining")
			}
			if w.heldByConflict(entry) {
				held = append(held, entry)
				continue
			}
			if w.processEntry(ctx, entry) {
				flushed++
			}
		}
		w.requeue(held)
		if len(held) == len(entries) {
			break
		}
	}
	return flushed, nil
}

func (w *WritebackProcessor) heldByConflict(entry *cache.WritebackEntry) bool {
	if entry.Action != "upload" {
		return false
	}
	inode, err := w.cacheDB.GetInode(entry.InodeID)
	return err == nil && inode != nil && inode.SyncStatus == cache.StatusConflict
}

func (w *WritebackProcessor) requeue(entries []*cache.WritebackEntry) {
	for _, e := range entries {
		w.cacheDB.UpdateWriteback(e.ID, "pending", e.LastError, e.Attempts)
	}
}

func (w *WritebackProcessor) loop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("writeback PANIC: %v\n%s", r, buf[:n])
		}
	}()

	ticker := time.NewTicker(debounceDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *WritebackProcessor) processBatch(ctx context.Context) {
	claimBefore := time.Now().Add(-debounceDelay).Unix()
	entries, err := w.cacheDB.DequeueWriteback(processBatchSize, claimBefore)
	if err != nil || len(entries) == 0 {
		return
	}

	var held []*cache.WritebackEntry
	for i, entry := range entries {
		if ctx.Err() != nil {
			w.requeue(entries[i:])
			w.requeue(held)
			return
		}
		if w.heldByConflict(entry) {
			held = append(held, entry)
			continue
		}
		w.processEntry(ctx, entry)
	}
	w.requeue(held)
}

func (w *WritebackProcessor) processEntry(ctx context.Context, entry *cache.WritebackEntry) bool {
	var preNode *vfs.Node
	var preGen uint64
	if entry.Action == "upload" {
		if n := w.vfs.NodeByID(entry.InodeID); n != nil {
			n.Mu.RLock()
			preNode = n
			preGen = n.WriteGen
			n.Mu.RUnlock()
		}
	}

	var uploadBytes int64
	if entry.Action == "upload" {
		if inode, gerr := w.cacheDB.GetInode(entry.InodeID); gerr == nil && inode != nil {
			uploadBytes = inode.Size
		}
	}

	var err error
	switch entry.Action {
	case "upload":
		err = w.processUpload(ctx, entry)
	case "mkdir":
		err = w.processMkdir(ctx, entry)
	case "delete":
		err = w.processDelete(ctx, entry)
	case "rename":
		err = w.processRename(ctx, entry)
	default:
		err = fmt.Errorf("unknown action: %s", entry.Action)
	}

	if err != nil {
		attempts := entry.Attempts + 1
		if attempts >= maxRetries {
			w.cacheDB.UpdateWriteback(entry.ID, "failed", err.Error(), attempts)
			w.cacheDB.SetSyncStatus(entry.InodeID, cache.StatusFailed, err.Error())
			if node := w.vfs.NodeByID(entry.InodeID); node != nil {
				node.Mu.Lock()
				node.SyncStatus = cache.StatusFailed
				node.StatusReason = err.Error()
				node.Mu.Unlock()
			}
			w.emitUpload(entry, uploadBytes, err)
		} else {
			w.cacheDB.UpdateWriteback(entry.ID, "pending", err.Error(), attempts)
		}
		return false
	}

	w.cacheDB.DeleteWriteback(entry.ID)
	w.emitUpload(entry, uploadBytes, nil)

	if preNode != nil {
		preNode.Mu.Lock()
		if preNode.WriteGen != preGen {
			rp := preNode.RemotePath
			preNode.Mu.Unlock()
			if err := w.cacheDB.EnqueueWriteback(entry.InodeID, "upload", rp, ""); err != nil {
				log.Printf("writeback re-enqueue failed (inode %d): %v", entry.InodeID, err)
				w.cacheDB.SetSyncStatus(entry.InodeID, cache.StatusPending, "re-enqueue failed")
				preNode.Mu.Lock()
				preNode.Dirty = true
				preNode.SyncStatus = cache.StatusPending
				preNode.StatusReason = "re-enqueue failed"
				preNode.Mu.Unlock()
			}
			return true
		}
		preNode.Dirty = false
		preNode.SyncStatus = cache.StatusSynced
		preNode.StatusReason = ""
		preNode.Data = nil
		preNode.Mu.Unlock()
	}

	w.cacheDB.MarkSynced(entry.InodeID, "")
	return true
}

func (w *WritebackProcessor) emitUpload(entry *cache.WritebackEntry, bytes int64, err error) {
	if w.activity == nil || entry.Action != "upload" {
		return
	}
	w.activity(entry.RemotePath, "upload", bytes, err)
}

func (w *WritebackProcessor) processUpload(ctx context.Context, entry *cache.WritebackEntry) error {
	var plaintext []byte
	var name, remotePath string

	node := w.vfs.NodeByID(entry.InodeID)
	if node != nil {
		node.Mu.RLock()
		name = node.Name
		remotePath = node.RemotePath
		node.Mu.RUnlock()
	}

	if w.syncDir == "" && node != nil {
		node.Mu.RLock()
		data := node.Data
		cached := node.Cached
		hash := node.ContentHash
		node.Mu.RUnlock()
		if data != nil {
			plaintext = data
		} else if cached && hash != "" {
			b, err := w.store.Get(hash)
			if err != nil {
				return fmt.Errorf("read cache: %w", err)
			}
			plaintext = b
		}
	}

	if plaintext == nil {
		inode, err := w.cacheDB.GetInode(entry.InodeID)
		if err != nil || inode == nil {
			return fmt.Errorf("inode %d not found", entry.InodeID)
		}
		if name == "" {
			name = inode.DisplayName
		}
		if remotePath == "" {
			remotePath = inode.RemotePath
		}
		if inode.ContentHash != "" {
			if b, err := w.store.Get(inode.ContentHash); err == nil {
				plaintext = b
			}
		}
	}

	if plaintext == nil {
		if lp, ok := w.localPath(remotePath); ok {
			if b, err := os.ReadFile(lp); err == nil {
				plaintext = b
			}
		}
	}

	if plaintext == nil {
		return fmt.Errorf("no content to upload")
	}

	dataKey, err := crypto.GenerateDataKey()
	if err != nil {
		return fmt.Errorf("generate data key: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "pigcloud-mount-ul-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPlainPath := tmpFile.Name()
	if _, err := tmpFile.Write(plaintext); err != nil {
		tmpFile.Close()
		os.Remove(tmpPlainPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpPlainPath)

	tmpEncFile, err := os.CreateTemp("", "pigcloud-mount-enc-*")
	if err != nil {
		return fmt.Errorf("create encrypted temp file: %w", err)
	}
	tmpEncPath := tmpEncFile.Name()
	tmpEncFile.Close()
	defer os.Remove(tmpEncPath)

	encMeta, err := crypto.EncryptFile(tmpPlainPath, tmpEncPath, dataKey)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	sealedKey, err := crypto.SealDataKey(dataKey, w.vfs.PublicKey)
	if err != nil {
		return fmt.Errorf("seal data key: %w", err)
	}

	metaJSON, err := json.Marshal(encMeta)
	if err != nil {
		return fmt.Errorf("marshal encryption meta: %w", err)
	}

	signPub := w.vfs.SigningPublicKey
	signPriv := w.vfs.SigningPrivateKey
	if signPub == nil || signPriv == nil {
		return fmt.Errorf("signing keys unavailable; unlock with 'pc uk' and restart the mount")
	}
	encFile, err := os.Open(tmpEncPath)
	if err != nil {
		return fmt.Errorf("open encrypted file for signing: %w", err)
	}
	sigEd, sigMldsa, signErr := crypto.SignFileBytes(encFile, signPriv)
	encFile.Close()
	if signErr != nil {
		return fmt.Errorf("sign upload: %w", signErr)
	}

	e2eeOpts := map[string]string{
		"sealed_key":         base64.StdEncoding.EncodeToString(sealedKey),
		"encryption_meta":    base64.StdEncoding.EncodeToString(metaJSON),
		"_original_name":     name,
		"signature_ed25519":  base64.StdEncoding.EncodeToString(sigEd),
		"signature_mldsa":    base64.StdEncoding.EncodeToString(sigMldsa),
		"signing_pk_ed25519": base64.StdEncoding.EncodeToString(signPub.Ed25519[:]),
		"signing_pk_mldsa":   base64.StdEncoding.EncodeToString(signPub.Mldsa),
	}

	teeKeys := cmdutil.FetchTeeEnclaveKeySet()
	if teeKeys == nil {
		return fmt.Errorf("TEE enclave key unavailable; scanner cannot decrypt the upload")
	}
	teeSealed, err := crypto.SealDataKey(dataKey, teeKeys)
	if err != nil {
		return fmt.Errorf("seal data key to enclave: %w", err)
	}
	e2eeOpts["tee_sealed_key"] = base64.StdEncoding.EncodeToString(teeSealed)
	if w.vfs.NameKey != nil {
		if h, err := crypto.ComputePlaintextHmac(encMeta.PlaintextSHA256, w.vfs.NameKey); err == nil {
			e2eeOpts["plaintext_hmac"] = h
		}
	}

	sealedName, err := crypto.SealDisplayName(name, w.vfs.PublicKey)
	if err == nil {
		e2eeOpts["e2ee_display_name"] = base64.StdEncoding.EncodeToString(sealedName)
	}
	pathToken, err := crypto.ComputePathToken(w.vfs.NameKey, remotePath)
	if err == nil {
		e2eeOpts["e2ee_path_token"] = fmt.Sprintf("%x", pathToken)
	}

	addPathTokens(e2eeOpts, remotePath, w.vfs.NameKey)

	uploadDir := "/"
	if idx := strings.LastIndex(remotePath, "/"); idx >= 0 {
		uploadDir = "/" + remotePath[:idx]
	}
	resp, err := w.client.Upload(ctx, tmpEncPath, uploadDir, nil, e2eeOpts)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if resp == nil || !resp.Success {
		msg := "unknown error"
		if resp != nil && resp.Message != "" {
			msg = resp.Message
		}
		return fmt.Errorf("upload rejected: %s", msg)
	}

	return nil
}

func (w *WritebackProcessor) processMkdir(ctx context.Context, entry *cache.WritebackEntry) error {
	options := map[string]string{
		"source": "/" + entry.RemotePath,
	}
	addPathTokens(options, entry.RemotePath, w.vfs.NameKey)

	name := entry.RemotePath
	if idx := strings.LastIndex(entry.RemotePath, "/"); idx >= 0 {
		name = entry.RemotePath[idx+1:]
	}
	if sealedName, err := crypto.SealDisplayName(name, w.vfs.PublicKey); err == nil {
		options["e2ee_display_name"] = base64.StdEncoding.EncodeToString(sealedName)
	}
	if token, err := crypto.ComputePathToken(w.vfs.NameKey, entry.RemotePath); err == nil {
		options["e2ee_path_token"] = fmt.Sprintf("%x", token)
	}

	resp, err := w.client.Execute(ctx, "mk", options)
	if err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("mkdir: %s", resp.Message)
	}
	return nil
}

func (w *WritebackProcessor) processDelete(ctx context.Context, entry *cache.WritebackEntry) error {
	options := map[string]string{
		"source": "/" + entry.RemotePath,
	}
	addPathTokens(options, entry.RemotePath, w.vfs.NameKey)

	resp, err := w.client.Execute(ctx, "rm", options)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("delete: %s", resp.Message)
	}
	return nil
}

func (w *WritebackProcessor) processRename(ctx context.Context, entry *cache.WritebackEntry) error {
	var extra struct {
		Target string `json:"target"`
	}
	if entry.ExtraJSON != "" {
		json.Unmarshal([]byte(entry.ExtraJSON), &extra)
	}
	if extra.Target == "" {
		return fmt.Errorf("rename: missing target")
	}

	options := map[string]string{
		"source": "/" + entry.RemotePath,
		"target": "/" + extra.Target,
	}
	addPathTokens(options, entry.RemotePath, w.vfs.NameKey)
	addPathTokens(options, extra.Target, w.vfs.NameKey)

	resp, err := w.client.Execute(ctx, "mv", options)
	if err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("rename: %s", resp.Message)
	}
	return nil
}
