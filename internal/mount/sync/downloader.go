package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/cmdutil"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/vfs"
)

type Downloader struct {
	syncDir    string
	remotePath string
	vfs        *vfs.VFS
	client     *api.Client
	cacheDB    *cache.DB
	suppress   *gosync.Map
	activity   ActivityFunc

	dlSem chan struct{}

	cancel context.CancelFunc
}

func (d *Downloader) SetActivityCallback(fn ActivityFunc) {
	d.activity = fn
}

func NewDownloader(syncDir, remotePath string, v *vfs.VFS, client *api.Client,
	cacheDB *cache.DB, suppress *gosync.Map) *Downloader {
	return &Downloader{
		syncDir:    syncDir,
		remotePath: remotePath,
		vfs:        v,
		client:     client,
		cacheDB:    cacheDB,
		suppress:   suppress,
		dlSem:      make(chan struct{}, 4),
	}
}

func (d *Downloader) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go d.loop(ctx)
}

func (d *Downloader) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *Downloader) InitialSync(ctx context.Context) error {
	log.Printf("downloader: starting initial sync for /%s", d.remotePath)

	cleaned := d.cleanupStaleEntries()
	if cleaned > 0 {
		log.Printf("downloader: cleaned %d stale entries", cleaned)
	}

	if err := d.populateRemoteTree(ctx, d.vfs.Root); err != nil {
		return fmt.Errorf("initial sync: populate remote tree: %w", err)
	}

	var downloaded, skipped int64
	var wg gosync.WaitGroup
	walkErr := d.walkAndSync(ctx, d.vfs.Root, &downloaded, &skipped, &wg)
	wg.Wait()
	if walkErr != nil {
		return walkErr
	}

	uploaded := d.scanLocalNewFiles()

	log.Printf("downloader: initial sync complete: %d downloaded, %d skipped, %d queued for upload",
		downloaded, skipped, uploaded)
	return nil
}

func (d *Downloader) cleanupStaleEntries() int {
	cleaned := 0

	failedWBCount := d.cacheDB.DeleteFailedWritebacks()
	if failedWBCount > 0 {
		log.Printf("downloader: purged %d failed writeback entries", failedWBCount)
	}

	issues, err := d.cacheDB.ListIssues()
	if err != nil {
		return cleaned
	}

	for _, inode := range issues {
		if inode.RemotePath == "" {
			d.cacheDB.DeleteWritebackByInode(inode.ID)
			d.cacheDB.DeleteInode(inode.RemotePath)
			cleaned++
			continue
		}

		localPath := d.toLocalPath(inode.RemotePath)
		_, statErr := os.Stat(localPath)
		localExists := statErr == nil

		shouldClean := false
		switch inode.SyncStatus {
		case cache.StatusFailed:
			if !localExists {
				shouldClean = true
			}
		case cache.StatusRejected:
			if !localExists {
				shouldClean = true
			}
		}

		if shouldClean {
			d.cacheDB.DeleteWritebackByInode(inode.ID)
			d.cacheDB.DeleteInode(inode.RemotePath)
			cleaned++
		}
	}
	return cleaned
}

func (d *Downloader) loop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("downloader PANIC: %v\n%s", r, buf[:n])
		}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.downloadPending(ctx)
		}
	}
}

func (d *Downloader) downloadPending(ctx context.Context) {
	var wg gosync.WaitGroup
	d.walkForDownloads(ctx, d.vfs.Root, &wg)
	wg.Wait()
}

func (d *Downloader) dispatchDownload(ctx context.Context, node *vfs.Node, wg *gosync.WaitGroup, onDone func(*vfs.Node, error)) {
	select {
	case d.dlSem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { <-d.dlSem }()
		err := d.downloadFile(ctx, node)
		if onDone != nil {
			onDone(node, err)
		}
	}()
}

func (d *Downloader) walkForDownloads(ctx context.Context, node *vfs.Node, wg *gosync.WaitGroup) {
	if ctx.Err() != nil {
		return
	}

	if !node.IsDir {
		node.Mu.RLock()
		needsDownload := !node.Cached && !node.Dirty
		remotePath := node.RemotePath
		nodeID := node.ID
		node.Mu.RUnlock()

		if needsDownload && d.inodeDirty(nodeID) {
			needsDownload = false
		}

		if needsDownload {
			localPath := d.toLocalPath(remotePath)
			if info, err := os.Stat(localPath); err == nil {
				node.Mu.RLock()
				sameSize := info.Size() == node.Size
				node.Mu.RUnlock()
				if sameSize {
					node.Mu.Lock()
					node.Cached = true
					node.Mu.Unlock()
					return
				}
			}
			d.dispatchDownload(ctx, node, wg, func(n *vfs.Node, err error) {
				if err != nil {
					log.Printf("downloader: %s: %v", n.RemotePath, err)
				}
			})
		}
		return
	}

	if !node.Loaded {
		return
	}

	for _, child := range node.ListChildren() {
		d.walkForDownloads(ctx, child, wg)
	}
}

func (d *Downloader) downloadFile(ctx context.Context, node *vfs.Node) (retErr error) {
	node.Mu.RLock()
	remotePath := node.RemotePath
	size := node.Size
	node.Mu.RUnlock()

	var gotBytes int64
	defer func() {
		if d.activity != nil {
			d.activity(remotePath, "download", gotBytes, retErr)
		}
	}()

	log.Printf("downloader: downloading %s (%d bytes)", remotePath, size)

	opts := map[string]string{}
	d.vfs.AddPathTokensPublic(opts, []string{remotePath})

	var encryptedData []byte
	var dlResult *api.DownloadResult

	for attempt := 0; attempt < 4; attempt++ {
		dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)

		var err error
		encryptedData, dlResult, err = d.client.DownloadToMemory(
			dlCtx, "/"+remotePath, opts)
		cancel()

		if err == nil {
			break
		}

		errMsg := err.Error()
		if strings.Contains(errMsg, "Try again") || strings.Contains(errMsg, "too many") ||
			strings.Contains(errMsg, "Too many") || strings.Contains(errMsg, "rate") {
			delay := time.Duration(15*(attempt+1)) * time.Second
			log.Printf("downloader: rate limited on %s, retrying in %v (attempt %d)", remotePath, delay, attempt+1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		return fmt.Errorf("download %s: %w", remotePath, err)
	}

	if encryptedData == nil || dlResult == nil {
		return fmt.Errorf("download %s: exhausted retries", remotePath)
	}

	if !dlResult.E2EE || dlResult.SealedKey == "" {
		return fmt.Errorf("non-E2EE download not supported for %s", remotePath)
	}

	sealedKeyBytes, err := base64.StdEncoding.DecodeString(dlResult.SealedKey)
	if err != nil {
		return fmt.Errorf("decode sealed key: %w", err)
	}

	dataKey, err := crypto.UnsealDataKey(sealedKeyBytes, d.vfs.PrivateKey)
	if err != nil {
		return fmt.Errorf("unseal data key: %w", err)
	}

	var encMeta crypto.EncryptionMetadata
	if dlResult.EncryptionMeta != "" {
		metaBytes, err := base64.StdEncoding.DecodeString(dlResult.EncryptionMeta)
		if err != nil {
			return fmt.Errorf("decode encryption meta: %w", err)
		}
		if err := json.Unmarshal(metaBytes, &encMeta); err != nil {
			return fmt.Errorf("parse encryption meta: %w", err)
		}
	}

	if err := cmdutil.VerifyDownloadIntegrityWithSigningKey(bytes.NewReader(encryptedData), dlResult, d.vfs.SigningPrivateKey); err != nil {
		return fmt.Errorf("verify %s: %w", remotePath, err)
	}
	plaintext, err := crypto.DecryptBytes(encryptedData, dataKey, &encMeta)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", remotePath, err)
	}
	gotBytes = int64(len(plaintext))

	localPath, ok := d.localPathWithin(remotePath)
	if !ok {
		return fmt.Errorf("refusing unsafe local path for %s", remotePath)
	}

	d.suppress.Store(localPath, true)
	defer func() {
		time.AfterFunc(3*time.Second, func() {
			d.suppress.Delete(localPath)
		})
	}()

	if err := os.MkdirAll(filepath.Dir(localPath), 0700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".pig-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", localPath, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(plaintext); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp for %s: %w", localPath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp for %s: %w", localPath, err)
	}
	tmp.Close()
	if err := os.Rename(tmpName, localPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("commit %s: %w", localPath, err)
	}

	node.Mu.RLock()
	mtime := node.Mtime
	node.Mu.RUnlock()
	os.Chtimes(localPath, mtime, mtime)

	node.Mu.Lock()
	node.Cached = true
	node.SealedKey = dlResult.SealedKey
	node.EncMeta = dlResult.EncryptionMeta
	node.Size = int64(len(plaintext))
	node.Mu.Unlock()

	d.cacheDB.MarkCached(node.ID, "")
	d.cacheDB.SetSyncStatus(node.ID, cache.StatusSynced, "")

	log.Printf("downloader: saved %s (%d bytes)", remotePath, len(plaintext))
	return nil
}

func (d *Downloader) populateRemoteTree(ctx context.Context, parent *vfs.Node) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, err := d.vfs.Readdir(parent); err != nil {
		return err
	}

	for _, child := range parent.ListChildren() {
		if child.IsDir {
			if err := d.populateRemoteTree(ctx, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Downloader) walkAndSync(ctx context.Context, node *vfs.Node, downloaded, skipped *int64, wg *gosync.WaitGroup) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if node.IsDir {
		if node != d.vfs.Root {
			localDir, ok := d.localPathWithin(node.RemotePath)
			if !ok {
				log.Printf("downloader: skipping unsafe remote dir %q", node.RemotePath)
				return nil
			}
			d.suppress.Store(localDir, true)
			os.MkdirAll(localDir, 0700)
			time.AfterFunc(3*time.Second, func() { d.suppress.Delete(localDir) })
		}

		for _, child := range node.ListChildren() {
			if err := d.walkAndSync(ctx, child, downloaded, skipped, wg); err != nil {
				return err
			}
		}
		return nil
	}

	localPath := d.toLocalPath(node.RemotePath)
	if info, err := os.Stat(localPath); err == nil {
		node.Mu.RLock()
		remoteSize := node.Size
		remoteMtime := node.Mtime
		node.Mu.RUnlock()

		if info.Size() == remoteSize {
			node.Mu.Lock()
			node.Cached = true
			node.Mu.Unlock()
			d.cacheDB.SetSyncStatus(node.ID, cache.StatusSynced, "")
			atomic.AddInt64(skipped, 1)
			return nil
		}

		if info.ModTime().After(remoteMtime) {
			d.enqueueLocalUpload(node, localPath, info)
			atomic.AddInt64(skipped, 1)
			return nil
		}
	}

	d.dispatchDownload(ctx, node, wg, func(n *vfs.Node, err error) {
		if err != nil {
			log.Printf("downloader: initial sync: %s: %v", n.RemotePath, err)
			d.cacheDB.SetSyncStatus(n.ID, cache.StatusFailed, err.Error())
			return
		}
		atomic.AddInt64(downloaded, 1)
	})
	return nil
}

func (d *Downloader) enqueueLocalUpload(node *vfs.Node, localPath string, info os.FileInfo) {
	ok, reason := vfs.ValidateFile(filepath.Base(localPath), info.Size())
	status := cache.StatusPending
	if !ok {
		status = cache.StatusRejected
	}

	var parentID int64
	if node.Parent != nil {
		parentID = node.Parent.ID
	}

	node.Mu.Lock()
	node.Dirty = true
	node.Cached = false
	node.Size = info.Size()
	node.Mu.Unlock()

	inode := &cache.Inode{
		RemotePath:   node.RemotePath,
		DisplayName:  node.Name,
		IsDir:        false,
		Size:         info.Size(),
		Mtime:        info.ModTime().Unix(),
		Dirty:        true,
		SyncStatus:   status,
		StatusReason: reason,
		ParentID:     parentID,
	}
	id, err := d.cacheDB.UpsertInode(inode)
	if err != nil {
		log.Printf("downloader: track local edit %s: %v", node.RemotePath, err)
		return
	}
	node.ID = id
	if ok {
		d.cacheDB.EnqueueWriteback(id, "upload", node.RemotePath, "")
		log.Printf("downloader: keeping local edit of %s (queued upload)", node.RemotePath)
	}
}

func (d *Downloader) RemoveLocal(remotePath string) {
	localPath, ok := d.localPathWithin(remotePath)
	if !ok {
		return
	}
	fi, err := os.Lstat(localPath)
	if err != nil {
		return
	}
	var delBytes int64
	if !fi.IsDir() {
		delBytes = fi.Size()
	}

	rel := remotePath
	if d.remotePath != "" && d.remotePath != "/" {
		rel = strings.TrimPrefix(remotePath, d.remotePath+"/")
	}
	trashPath := filepath.Join(d.syncDir, ".pigcloud", "trash", filepath.FromSlash(rel))

	d.suppress.Store(localPath, true)
	time.AfterFunc(3*time.Second, func() { d.suppress.Delete(localPath) })

	if err := os.MkdirAll(filepath.Dir(trashPath), 0700); err != nil {
		log.Printf("downloader: trash dir for %s: %v", remotePath, err)
		return
	}
	dst := trashPath
	for i := 1; ; i++ {
		if _, err := os.Lstat(dst); err != nil {
			break
		}
		dst = fmt.Sprintf("%s.%d", trashPath, i)
	}
	if err := os.Rename(localPath, dst); err != nil {
		log.Printf("downloader: trash local %s: %v", remotePath, err)
		return
	}
	if d.activity != nil {
		d.activity(remotePath, "delete", delBytes, nil)
	}
}

func (d *Downloader) LocalPath(remotePath string) (string, bool) {
	return d.localPathWithin(remotePath)
}

func (d *Downloader) SuppressPath(localPath string, dur time.Duration) {
	d.suppress.Store(localPath, true)
	time.AfterFunc(dur, func() { d.suppress.Delete(localPath) })
}

func (d *Downloader) scanLocalNewFiles() int {
	queued := 0
	filepath.WalkDir(d.syncDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if shouldIgnore(name) || name == ".pigcloud" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		remotePath := d.pathToRemote(path)
		if remotePath == "" || remotePath == d.remotePath {
			return nil
		}

		existing, _ := d.cacheDB.GetInodeByPath(remotePath)
		if existing != nil {
			return nil
		}

		if entry.IsDir() {
			inode := &cache.Inode{
				RemotePath:  remotePath,
				DisplayName: name,
				IsDir:       true,
				Mtime:       time.Now().Unix(),
				SyncStatus:  cache.StatusPending,
			}
			id, err := d.cacheDB.UpsertInode(inode)
			if err == nil {
				d.cacheDB.EnqueueWriteback(id, "mkdir", remotePath, "")
				queued++
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		ok, reason := vfs.ValidateFile(name, info.Size())
		status := cache.StatusPending
		if !ok {
			status = cache.StatusRejected
		}

		inode := &cache.Inode{
			RemotePath:   remotePath,
			DisplayName:  name,
			IsDir:        false,
			Size:         info.Size(),
			Mtime:        info.ModTime().Unix(),
			Dirty:        true,
			SyncStatus:   status,
			StatusReason: reason,
		}
		id, err := d.cacheDB.UpsertInode(inode)
		if err != nil {
			return nil
		}

		if ok {
			d.cacheDB.EnqueueWriteback(id, "upload", remotePath, "")
			queued++
		}
		return nil
	})
	return queued
}

func (d *Downloader) inodeDirty(id int64) bool {
	if id == 0 {
		return false
	}
	inode, err := d.cacheDB.GetInode(id)
	return err == nil && inode != nil && inode.Dirty
}

func (d *Downloader) toLocalPath(remotePath string) string {
	rel := remotePath
	if d.remotePath != "" && d.remotePath != "/" {
		rel = strings.TrimPrefix(remotePath, d.remotePath+"/")
	}
	return filepath.Join(d.syncDir, filepath.FromSlash(rel))
}

func (d *Downloader) localPathWithin(remotePath string) (string, bool) {
	p := d.toLocalPath(remotePath)
	rel, err := filepath.Rel(d.syncDir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

func (d *Downloader) pathToRemote(localPath string) string {
	rel, err := filepath.Rel(d.syncDir, localPath)
	if err != nil {
		return filepath.Base(localPath)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return d.remotePath
	}
	if d.remotePath == "" || d.remotePath == "/" {
		return rel
	}
	return d.remotePath + "/" + rel
}
