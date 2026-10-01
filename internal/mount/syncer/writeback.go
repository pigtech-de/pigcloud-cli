package syncer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/crypto"
	"pigcloud/internal/e2ee"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/mlog"
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
	done   chan struct{}
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
	if w.readOnly() {
		mlog.Infof("writeback: read-only mount, processor not started")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		w.loop(ctx)
	}()
}

func (w *WritebackProcessor) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	awaitLoopExit(w.done, "writeback")
}

func (w *WritebackProcessor) readOnly() bool {
	return w.cacheDB != nil && w.cacheDB.WritebackDisabled()
}

func (w *WritebackProcessor) FlushAll(timeout time.Duration) (int, error) {
	if w.readOnly() {
		return 0, cache.ErrReadOnlyMount
	}
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
	defer mlog.RecoverPanic("writeback")

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
	var uploaded string
	switch entry.Action {
	case "upload":
		uploaded, err = w.processUpload(ctx, entry)
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
		if attempts >= maxRetries || isPermanent(err) {
			w.cacheDB.UpdateWriteback(entry.ID, "failed", err.Error(), attempts)
			w.cacheDB.SetSyncStatus(entry.InodeID, cache.StatusFailed, err.Error())
			if entry.Action == "upload" {
				f := &cache.SyncFailure{
					InodeID:   entry.InodeID,
					Kind:      cache.FailureUpload,
					Permanent: isPermanent(err),
					Attempts:  attempts,
					LastError: err.Error(),
				}
				if !f.Permanent {
					f.NextRetryAt = time.Now().Add(writebackRetryDelay(err, attempts)).Unix()
				}
				w.cacheDB.RecordSyncFailure(f)
			}
			if node := w.vfs.NodeByID(entry.InodeID); node != nil {
				node.Mu.Lock()
				node.SyncStatus = cache.StatusFailed
				node.StatusReason = err.Error()
				node.Mu.Unlock()
			}
			w.emitUpload(entry, uploadBytes, err)
		} else {
			delay := writebackRetryDelay(err, attempts)
			if api.IsRateLimited(err) {
				mlog.Warnf("writeback: rate limited on %s, next attempt in %v (attempt %d)", entry.RemotePath, delay, attempts)
			}
			w.cacheDB.SetSyncStatus(entry.InodeID, cache.StatusPending, err.Error())
			w.cacheDB.DeferWriteback(entry.ID, err.Error(), attempts, time.Now().Add(delay).Unix())
		}
		return false
	}

	w.cacheDB.DeleteWriteback(entry.ID)
	if entry.Action == "upload" {
		w.cacheDB.ClearSyncFailure(entry.InodeID, cache.FailureUpload)
		w.recordUploaded(entry, uploaded)
	}
	w.emitUpload(entry, uploadBytes, nil)

	if preNode != nil {
		preNode.Mu.Lock()
		if preNode.WriteGen != preGen {
			rp := preNode.RemotePath
			preNode.Mu.Unlock()
			if err := w.cacheDB.EnqueueWriteback(entry.InodeID, "upload", rp, ""); err != nil {
				mlog.Errorf("writeback re-enqueue failed (inode %d): %v", entry.InodeID, err)
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

func (w *WritebackProcessor) recordUploaded(entry *cache.WritebackEntry, digest string) {
	if digest == "" {
		return
	}
	var mtime int64
	if lp, ok := w.localPath(entry.RemotePath); ok {
		if fi, err := os.Stat(lp); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	w.cacheDB.SetLocalContent(entry.InodeID, digest, mtime)
}

func (w *WritebackProcessor) emitUpload(entry *cache.WritebackEntry, bytes int64, err error) {
	if w.activity == nil || entry.Action != "upload" {
		return
	}
	w.activity(entry.RemotePath, "upload", bytes, err)
}

func writeTempPlaintext(data []byte) (string, func(), error) {
	tmp, err := os.CreateTemp("", "pigcloud-mount-ul-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp file: %w", err)
	}
	return name, cleanup, nil
}

func (w *WritebackProcessor) streamStoreToTemp(hash string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "pigcloud-mount-ul-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := w.store.WriteTo(hash, tmp); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp file: %w", err)
	}
	return name, cleanup, nil
}

func (w *WritebackProcessor) processUpload(ctx context.Context, entry *cache.WritebackEntry) (string, error) {
	var name, remotePath, contentHash string
	var virtualBuf []byte

	node := w.vfs.NodeByID(entry.InodeID)
	if node != nil {
		node.Mu.RLock()
		name = node.Name
		remotePath = node.RemotePath
		if w.syncDir == "" {
			if node.Data != nil {
				virtualBuf = node.Data
			} else if node.Cached && node.ContentHash != "" {
				contentHash = node.ContentHash
			}
		}
		node.Mu.RUnlock()
	}

	if virtualBuf == nil && contentHash == "" {
		inode, err := w.cacheDB.GetInode(entry.InodeID)
		if err != nil || inode == nil {
			return "", fmt.Errorf("inode %d not found", entry.InodeID)
		}
		if name == "" {
			name = inode.DisplayName
		}
		if remotePath == "" {
			remotePath = inode.RemotePath
		}
		contentHash = inode.ContentHash
	}

	var plainPath string
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	if virtualBuf != nil {
		p, cl, err := writeTempPlaintext(virtualBuf)
		if err != nil {
			return "", err
		}
		plainPath, cleanup = p, cl
	}

	if plainPath == "" && contentHash != "" {
		p, cl, err := w.streamStoreToTemp(contentHash)
		if err != nil {
			if w.syncDir == "" {
				return "", fmt.Errorf("read cache: %w", err)
			}
		} else {
			plainPath, cleanup = p, cl
		}
	}

	if plainPath == "" {
		if lp, ok := w.localPath(remotePath); ok {
			if _, err := os.Stat(lp); err == nil {
				plainPath = lp
			}
		}
	}

	if plainPath == "" {
		return "", fmt.Errorf("no content to upload")
	}

	dataKey, err := crypto.GenerateDataKey()
	if err != nil {
		return "", fmt.Errorf("generate data key: %w", err)
	}

	tmpEncFile, err := os.CreateTemp("", "pigcloud-mount-enc-*")
	if err != nil {
		return "", fmt.Errorf("create encrypted temp file: %w", err)
	}
	tmpEncPath := tmpEncFile.Name()
	tmpEncFile.Close()
	defer os.Remove(tmpEncPath)

	encMeta, err := crypto.EncryptFile(plainPath, tmpEncPath, dataKey)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	sealedKey, err := crypto.SealDataKey(dataKey, w.vfs.PublicKey)
	if err != nil {
		return "", fmt.Errorf("seal data key: %w", err)
	}

	metaJSON, err := json.Marshal(encMeta)
	if err != nil {
		return "", fmt.Errorf("marshal encryption meta: %w", err)
	}

	signPub := w.vfs.SigningPublicKey
	signPriv := w.vfs.SigningPrivateKey
	if signPub == nil || signPriv == nil {
		return "", fmt.Errorf("signing keys unavailable; unlock with 'pc uk' and restart the mount")
	}
	encFile, err := os.Open(tmpEncPath)
	if err != nil {
		return "", fmt.Errorf("open encrypted file for signing: %w", err)
	}
	sigEd, sigMldsa, signErr := crypto.SignFileBytes(encFile, signPriv)
	encFile.Close()
	if signErr != nil {
		return "", fmt.Errorf("sign upload: %w", signErr)
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

	teeKeys := e2ee.FetchTeeEnclaveKeySet()
	if teeKeys == nil && !e2ee.TeeScannerDisabledByServer() {
		return "", fmt.Errorf("TEE enclave key unavailable; scanner cannot decrypt the upload")
	}
	if teeKeys != nil {
		teeSealed, err := crypto.SealDataKey(dataKey, teeKeys)
		if err != nil {
			return "", fmt.Errorf("seal data key to enclave: %w", err)
		}
		e2eeOpts["tee_sealed_key"] = base64.StdEncoding.EncodeToString(teeSealed)
	}
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

	if key := cache.UploadKeyFromExtra(entry.ExtraJSON); key != "" {
		e2eeOpts["upload_idempotency_key"] = key
	}

	uploadDir := "/"
	if idx := strings.LastIndex(remotePath, "/"); idx >= 0 {
		uploadDir = "/" + remotePath[:idx]
	}
	resp, err := w.client.Upload(ctx, tmpEncPath, uploadDir, nil, e2eeOpts)
	if err != nil {
		return "", classifyUploadError(err)
	}
	if resp == nil || !resp.Success {
		msg := "unknown error"
		if resp != nil && resp.Message != "" {
			msg = resp.Message
		}
		return "", fmt.Errorf("upload rejected: %s", msg)
	}

	return encMeta.PlaintextSHA256, nil
}

func classifyUploadError(err error) error {
	if api.IsPermanent(err) {
		return permanent(fmt.Errorf("upload: %w", err))
	}
	return fmt.Errorf("upload: %w", err)
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
