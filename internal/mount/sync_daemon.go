package mount

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/drivemap"
	msync "pigcloud/internal/mount/sync"
	"pigcloud/internal/mount/vfs"
)

type SyncDaemon struct {
	vfs        *vfs.VFS
	cacheDB    *cache.DB
	store      *cache.Store
	evictor    *cache.Evictor
	poller     *msync.Poller
	writeback  *msync.WritebackProcessor
	watcher    *msync.Watcher
	downloader *msync.Downloader
	reconciler *msync.Reconciler
	mapper     drivemap.Mapper

	syncDir    string
	mountPoint string
	remotePath string
	cacheMax   int64
	startedAt  time.Time
	token      string
	activity   *activityRing
	mountInfo  *MountInfo

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

func ServeSyncDaemon(cfg *DaemonConfig) error {
	logFile = filepath.Join(configDir(), "mount.log")
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err == nil {
		log.SetOutput(lf)
		os.Stderr = lf
		defer lf.Close()
	}
	log.Printf("sync daemon starting: remote=%s mountpoint=%s", cfg.RemotePath, cfg.MountPoint)

	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("PANIC: %v\n%s", r, buf[:n])
		}
	}()

	remotePath := NormalizeRemotePath(cfg.RemotePath)
	ownerID := cfg.ownerID()
	syncPaths := LoadSyncPaths()
	syncDir := syncPaths.GetSyncDir(ownerID, remotePath)
	metaDir := filepath.Join(syncDir, ".pigcloud")

	if err := os.MkdirAll(metaDir, 0700); err != nil {
		return fmt.Errorf("create sync dir: %w", err)
	}

	if err := ClaimSyncDir(syncDir, ownerID); err != nil {
		return err
	}

	hideDir(metaDir)

	cacheDB, err := cache.Open(metaDir)
	if err != nil {
		return fmt.Errorf("open cache DB: %w", err)
	}

	store, err := cache.NewStore(metaDir)
	if err != nil {
		cacheDB.Close()
		return fmt.Errorf("create cache store: %w", err)
	}

	if n, err := cacheDB.RequeueInProgress(); err == nil && n > 0 {
		log.Printf("sync daemon: requeued %d stranded writeback entries", n)
	}
	if n, err := cache.GCOrphans(cacheDB, store); err == nil && n > 0 {
		log.Printf("sync daemon: removed %d orphan cache blobs", n)
	}

	evictor := cache.NewEvictor(cacheDB, store, cfg.CacheSize)
	client := api.NewClient()

	v := vfs.New(cfg.RemotePath, cacheDB, store, evictor, client,
		cfg.PublicKey, cfg.PrivateKey, cfg.NameKey, cfg.SigningPublicKey, cfg.SigningPrivateKey)
	v.ReadOnly = cfg.ReadOnly

	poller := msync.NewPoller(v, client, cacheDB, cfg.PollInterval)
	writeback := msync.NewWritebackProcessor(v, client, cacheDB, store, syncDir)

	suppress := &sync.Map{}

	watcher, err := msync.NewWatcher(syncDir, remotePath, cacheDB, store, evictor, v, suppress)
	if err != nil {
		store.Close()
		cacheDB.Close()
		return fmt.Errorf("create watcher: %w", err)
	}

	downloader := msync.NewDownloader(syncDir, remotePath, v, client, cacheDB, suppress)

	poller.SetLocalDelete(downloader.RemoveLocal)

	reconciler := msync.NewReconciler(syncDir, remotePath, cacheDB, 0)

	mapper := drivemap.New()

	sd := &SyncDaemon{
		vfs:        v,
		cacheDB:    cacheDB,
		store:      store,
		evictor:    evictor,
		poller:     poller,
		writeback:  writeback,
		watcher:    watcher,
		downloader: downloader,
		reconciler: reconciler,
		mapper:     mapper,
		syncDir:    syncDir,
		mountPoint: cfg.MountPoint,
		remotePath: remotePath,
		cacheMax:   cfg.CacheSize,
		startedAt:  time.Now(),
		activity:   &activityRing{},
		shutdownCh: make(chan struct{}),
	}
	sd.wireActivity()

	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	sd.token = hex.EncodeToString(tokenBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		sd.cleanup()
		return fmt.Errorf("start IPC server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	if err := mapper.Map(syncDir, cfg.MountPoint); err != nil {
		listener.Close()
		sd.cleanup()
		return fmt.Errorf("map drive: %w", err)
	}

	info := &MountInfo{
		Port:       port,
		Token:      sd.token,
		PID:        os.Getpid(),
		MountPoint: cfg.MountPoint,
		RemotePath: cfg.RemotePath,
		CacheDir:   metaDir,
		SyncDir:    syncDir,
		Mode:       ModeSync,
		Owner:      ownerID,
		StartedAt:  sd.startedAt,
	}
	if err := WriteMountEntry(info); err != nil {
		mapper.Unmap(cfg.MountPoint)
		listener.Close()
		sd.cleanup()
		return fmt.Errorf("write mount entry: %w", err)
	}
	sd.mountInfo = info

	go sd.acceptIPC(listener)

	sigCtx, sigStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigStop()
	go func() {
		<-sigCtx.Done()
		log.Printf("sync daemon: shutdown signal received")
		sd.shutdown()
	}()

	ctx, cancel := context.WithTimeout(sigCtx, 30*time.Minute)
	if err := downloader.InitialSync(ctx); err != nil {
		log.Printf("sync daemon: initial sync error (continuing): %v", err)
	}
	cancel()

	select {
	case <-sd.shutdownCh:
		listener.Close()
		log.Printf("sync daemon exiting (shutdown during initial sync)")
		return nil
	default:
	}

	poller.Start()
	downloader.Start()
	if cfg.ReadOnly {
		log.Printf("sync daemon: read-only mount, write-back disabled")
	} else {
		writeback.Start()
		if err := watcher.Start(); err != nil {
			log.Printf("sync daemon: watcher start error: %v", err)
		}
		reconciler.Start()
	}

	log.Printf("sync daemon ready: pid=%d port=%d mountpoint=%s syncdir=%s",
		os.Getpid(), port, cfg.MountPoint, syncDir)

	<-sd.shutdownCh

	listener.Close()
	log.Printf("sync daemon exiting")
	return nil
}

func (sd *SyncDaemon) acceptIPC(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-sd.shutdownCh:
				return
			default:
				continue
			}
		}
		go sd.handleConn(conn)
	}
}

func (sd *SyncDaemon) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req DaemonRequest
	if err := dec.Decode(&req); err != nil {
		enc.Encode(DaemonResponse{Error: "invalid request"})
		return
	}

	if req.Token != sd.token {
		enc.Encode(DaemonResponse{Error: "unauthorized"})
		return
	}

	if req.Action == "flush" {
		conn.SetDeadline(time.Now().Add(flushDeadline))
	}

	switch req.Action {
	case "ping":
		enc.Encode(DaemonResponse{OK: true})

	case "status":
		sd.handleStatus(enc)

	case "shutdown":
		enc.Encode(DaemonResponse{OK: true})
		go func() {
			time.Sleep(100 * time.Millisecond)
			sd.shutdown()
		}()

	case "pin":
		if req.Path != "" {
			sd.cacheDB.SetPinned(req.Path, true)
		}
		enc.Encode(DaemonResponse{OK: true})

	case "unpin":
		if req.Path != "" {
			sd.cacheDB.SetPinned(req.Path, false)
		}
		enc.Encode(DaemonResponse{OK: true})

	case "flush":
		flushed, err := sd.writeback.FlushAll(30 * time.Second)
		if err != nil {
			enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(DaemonResponse{OK: true, PendingCount: flushed})
		}

	case "clean":
		count, err := sd.vfs.CleanRejected()
		if err != nil {
			enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(DaemonResponse{OK: true, Cleaned: count})
		}

	case "resolve":
		if err := sd.resolveConflict(req.Path, req.Choice); err != nil {
			enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(DaemonResponse{OK: true})
		}

	case "files":
		sd.handleFiles(enc)

	case "conflicts":
		sd.handleConflicts(enc)

	case "activity":
		enc.Encode(DaemonResponse{OK: true, Activity: sd.activity.snapshot()})

	default:
		enc.Encode(DaemonResponse{Error: "unknown action"})
	}
}

func (sd *SyncDaemon) resolveConflict(remotePath, choice string) error {
	inode, err := sd.cacheDB.GetInodeByPath(remotePath)
	if err != nil || inode == nil {
		return fmt.Errorf("no such path: %s", remotePath)
	}
	if inode.SyncStatus != cache.StatusConflict {
		return fmt.Errorf("%s is not in conflict (status: %s)", remotePath, inode.SyncStatus)
	}

	node := sd.vfs.NodeByID(inode.ID)
	setNode := func(status cache.SyncStatus, dirty bool) {
		if node == nil {
			return
		}
		node.Mu.Lock()
		node.SyncStatus = status
		node.StatusReason = ""
		node.Dirty = dirty
		if !dirty {
			node.Cached = false
			node.ContentHash = ""
			node.Data = nil
		}
		node.Mu.Unlock()
	}
	discardLocal := func() error {
		localPath, ok := sd.downloader.LocalPath(remotePath)
		if !ok {
			return fmt.Errorf("unsafe path: %s", remotePath)
		}
		sd.downloader.SuppressPath(localPath, 5*time.Second)
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		sd.cacheDB.DeleteWritebackByInode(inode.ID)
		sd.cacheDB.MarkSynced(inode.ID, "")
		sd.cacheDB.InvalidateCache(inode.ID)
		setNode(cache.StatusSynced, false)
		return nil
	}

	switch choice {
	case "local":
		sd.cacheDB.DeleteWritebackByInode(inode.ID)
		sd.cacheDB.MarkDirty(inode.ID)
		if err := sd.cacheDB.EnqueueWriteback(inode.ID, "upload", remotePath, ""); err != nil {
			return err
		}
		setNode(cache.StatusPending, true)
		return nil

	case "remote":
		return discardLocal()

	case "both":
		localPath, ok := sd.downloader.LocalPath(remotePath)
		if !ok {
			return fmt.Errorf("unsafe path: %s", remotePath)
		}
		sd.downloader.SuppressPath(localPath, 5*time.Second)
		if err := os.Rename(localPath, conflictCopyPath(localPath)); err != nil {
			return err
		}
		sd.cacheDB.DeleteWritebackByInode(inode.ID)
		sd.cacheDB.MarkSynced(inode.ID, "")
		sd.cacheDB.InvalidateCache(inode.ID)
		setNode(cache.StatusSynced, false)
		return nil

	default:
		return fmt.Errorf("unknown choice %q (want local, remote, or both)", choice)
	}
}

func conflictCopyPath(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stamp := time.Now().Format("2006-01-02 150405")
	return filepath.Join(dir, fmt.Sprintf("%s (conflict %s)%s", stem, stamp, ext))
}

func (sd *SyncDaemon) handleStatus(enc *json.Encoder) {
	cacheUsed, _ := sd.cacheDB.TotalCacheSize()
	pending, _ := sd.cacheDB.PendingWritebackCount()
	failed, _ := sd.cacheDB.FailedWritebackCount()

	lastPoll := sd.poller.LastPoll()
	lastPollStr := ""
	if !lastPoll.IsZero() {
		lastPollStr = fmt.Sprintf("%ds ago", int(time.Since(lastPoll).Seconds()))
	}

	uptime := time.Since(sd.startedAt).Round(time.Second).String()

	enc.Encode(DaemonResponse{
		OK:           true,
		Online:       sd.poller.IsOnline(),
		MountPoint:   sd.mountPoint,
		RemotePath:   "/" + sd.remotePath,
		Mode:         ModeSync,
		SyncDir:      sd.syncDir,
		CacheUsed:    cacheUsed,
		CacheMax:     sd.cacheMax,
		PendingCount: pending,
		FailedCount:  failed,
		LastPoll:     lastPollStr,
		Uptime:       uptime,
	})
}

func (sd *SyncDaemon) wireActivity() {
	record := func(path, direction string, bytes int64, err error) {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		sd.activity.add(ActivityEvent{
			Path:      path,
			Direction: direction,
			Bytes:     bytes,
			Timestamp: time.Now().Unix(),
			Error:     msg,
		})
	}
	sd.writeback.SetActivityCallback(record)
	sd.downloader.SetActivityCallback(record)
}

func (sd *SyncDaemon) handleFiles(enc *json.Encoder) {
	entries, err := buildFileEntries(sd.cacheDB)
	if err != nil {
		enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		return
	}
	enc.Encode(DaemonResponse{OK: true, Files: entries})
}

func (sd *SyncDaemon) handleConflicts(enc *json.Encoder) {
	entries, err := buildConflictEntries(sd.cacheDB)
	if err != nil {
		enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		return
	}
	enc.Encode(DaemonResponse{OK: true, Files: entries})
}

func fileEntryFromInode(in *cache.Inode) FileEntry {
	return FileEntry{
		Path:   in.RemotePath,
		Status: string(in.SyncStatus),
		Dirty:  in.Dirty,
		Size:   in.Size,
		Pinned: in.Pinned,
		Reason: in.StatusReason,
	}
}

func buildFileEntries(db *cache.DB) ([]FileEntry, error) {
	inodes, err := db.AllInodes()
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(inodes))
	for _, in := range inodes {
		entries = append(entries, fileEntryFromInode(in))
	}
	return entries, nil
}

func buildConflictEntries(db *cache.DB) ([]FileEntry, error) {
	issues, err := db.ListIssues()
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0)
	for _, in := range issues {
		if in.SyncStatus == cache.StatusConflict {
			entries = append(entries, fileEntryFromInode(in))
		}
	}
	return entries, nil
}

func (sd *SyncDaemon) shutdown() {
	sd.shutdownOnce.Do(func() {
		log.Printf("sync daemon shutting down")

		sd.writeback.FlushAll(30 * time.Second)
		sd.writeback.Stop()
		sd.watcher.Stop()
		sd.reconciler.Stop()
		sd.downloader.Stop()
		sd.poller.Stop()

		if err := sd.mapper.Unmap(sd.mountPoint); err != nil {
			log.Printf("sync daemon: unmap warning: %v", err)
		}

		sd.cleanup()
		close(sd.shutdownCh)
	})
}

func (sd *SyncDaemon) cleanup() {
	sd.store.Close()
	sd.cacheDB.Close()
	EvictMountEntry(sd.mountInfo)
}
