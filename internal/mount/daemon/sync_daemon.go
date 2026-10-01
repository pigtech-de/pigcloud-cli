package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/mount"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/drivemap"
	"pigcloud/internal/mount/mlog"
	"pigcloud/internal/mount/syncer"
	"pigcloud/internal/mount/vfs"
	"pigcloud/internal/netutil"
)

type SyncDaemon struct {
	vfs        *vfs.VFS
	cacheDB    *cache.DB
	store      *cache.Store
	evictor    *cache.Evictor
	poller     *syncer.Poller
	writeback  *syncer.WritebackProcessor
	watcher    *syncer.Watcher
	downloader *syncer.Downloader
	reconciler *syncer.Reconciler
	mapper     drivemap.Mapper

	syncDir    string
	mountPoint string
	remotePath string
	cacheMax   int64
	startedAt  time.Time
	token      string
	activity   *activityRing
	mountInfo  *mount.MountInfo

	shutdownOnce sync.Once
	shutdownCh   chan struct{}

	initialSyncCancel context.CancelFunc
	initialSyncDone   chan struct{}

	svcMu   sync.Mutex
	stopped bool
	ipcWG   sync.WaitGroup
}

func (sd *SyncDaemon) beginMutation() bool {
	sd.svcMu.Lock()
	defer sd.svcMu.Unlock()
	if sd.stopped {
		return false
	}
	sd.ipcWG.Add(1)
	return true
}

const initialSyncStopWait = 15 * time.Second

func ServeSync(cfg *Config) error {
	logFile = mount.MountLogPath(cfg.ownerID(), cfg.RemotePath)
	if lf, err := mlog.NewRotatingLog(logFile); err == nil {
		mlog.SetOutput(lf)
		defer lf.Close()
	}
	mlog.Infof("sync daemon starting: remote=%s mountpoint=%s level=%s", cfg.RemotePath, cfg.MountPoint, mlog.CurrentLevel())

	defer mlog.RecoverPanic("sync daemon")

	remotePath := mount.NormalizeRemotePath(cfg.RemotePath)
	ownerID := cfg.ownerID()
	syncPaths := mount.LoadSyncPaths()
	syncDir := syncPaths.GetSyncDir(ownerID, remotePath)
	metaDir := filepath.Join(syncDir, ".pigcloud")

	if err := os.MkdirAll(metaDir, 0700); err != nil {
		return fmt.Errorf("create sync dir: %w", err)
	}

	if err := mount.ClaimSyncDir(syncDir, ownerID); err != nil {
		return err
	}

	mount.HideDir(metaDir)

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
		mlog.Infof("sync daemon: requeued %d stranded writeback entries", n)
	}
	if swept, err := cache.GCOrphans(cacheDB, store); err == nil {
		if swept.Blobs > 0 {
			mlog.Infof("sync daemon: removed %d orphan cache blobs", swept.Blobs)
		}
		if swept.Temps > 0 {
			mlog.Infof("sync daemon: removed %d stranded cache temp files", swept.Temps)
		}
	}

	evictor := cache.NewEvictor(cacheDB, store, cfg.CacheSize)
	client := api.NewClient()

	v := vfs.New(cfg.RemotePath, cacheDB, store, evictor, client,
		cfg.PublicKey, cfg.PrivateKey, cfg.NameKey, cfg.SigningPublicKey, cfg.SigningPrivateKey)
	v.SetReadOnly(cfg.ReadOnly)

	poller := syncer.NewPoller(v, client, cacheDB, cfg.PollInterval)
	writeback := syncer.NewWritebackProcessor(v, client, cacheDB, store, syncDir)

	suppress := &sync.Map{}

	watcher, err := syncer.NewWatcher(syncDir, remotePath, cacheDB, store, evictor, v, suppress)
	if err != nil {
		store.Close()
		cacheDB.Close()
		return fmt.Errorf("create watcher: %w", err)
	}

	downloader := syncer.NewDownloader(syncDir, remotePath, v, client, cacheDB, suppress)

	downloader.SetStore(store)

	poller.SetLocalDelete(downloader.RemoveLocal)

	reconciler := syncer.NewReconciler(syncDir, remotePath, cacheDB, 0)
	reconciler.SetStore(store)

	mapper := drivemap.New()

	sd := &SyncDaemon{
		vfs:             v,
		cacheDB:         cacheDB,
		store:           store,
		evictor:         evictor,
		poller:          poller,
		writeback:       writeback,
		watcher:         watcher,
		downloader:      downloader,
		reconciler:      reconciler,
		mapper:          mapper,
		syncDir:         syncDir,
		mountPoint:      cfg.MountPoint,
		remotePath:      remotePath,
		cacheMax:        cfg.CacheSize,
		startedAt:       time.Now(),
		activity:        &activityRing{},
		shutdownCh:      make(chan struct{}),
		initialSyncDone: make(chan struct{}),
	}
	sd.wireActivity()

	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	sd.token = hex.EncodeToString(tokenBytes)

	listener, err := net.Listen("tcp", netutil.LoopbackAny)
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

	info := &mount.MountInfo{
		Port:       port,
		Token:      sd.token,
		PID:        os.Getpid(),
		MountPoint: cfg.MountPoint,
		RemotePath: cfg.RemotePath,
		CacheDir:   metaDir,
		SyncDir:    syncDir,
		Mode:       mount.ModeSync,
		Owner:      ownerID,
		StartedAt:  sd.startedAt,
	}
	if err := mount.WriteMountEntry(info); err != nil {
		mapper.Unmap(cfg.MountPoint)
		listener.Close()
		sd.cleanup()
		return fmt.Errorf("write mount entry: %w", err)
	}
	sd.mountInfo = info

	sigCtx, sigStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigStop()
	syncCtx, syncCancel := context.WithTimeout(sigCtx, 30*time.Minute)
	defer syncCancel()
	sd.initialSyncCancel = syncCancel

	go sd.acceptIPC(listener)

	go func() {
		<-sigCtx.Done()
		mlog.Infof("sync daemon: shutdown signal received")
		sd.shutdown()
	}()

	if err := downloader.InitialSync(syncCtx); err != nil {
		mlog.Warnf("sync daemon: initial sync error (continuing): %v", err)
	}
	syncCancel()
	close(sd.initialSyncDone)

	sd.svcMu.Lock()
	if sd.stopped {
		sd.svcMu.Unlock()
		<-sd.shutdownCh
		listener.Close()
		mlog.Infof("sync daemon exiting (shutdown during initial sync)")
		return nil
	}

	poller.Start()
	downloader.Start()
	if cfg.ReadOnly {
		mlog.Infof("sync daemon: read-only mount, write-back disabled")
	} else {
		writeback.Start()
		if err := watcher.Start(); err != nil {
			mlog.Errorf("sync daemon: watcher start error: %v", err)
		}
		reconciler.Start()
	}
	sd.svcMu.Unlock()

	mlog.Infof("sync daemon ready: pid=%d port=%d mountpoint=%s syncdir=%s",
		os.Getpid(), port, cfg.MountPoint, syncDir)

	<-sd.shutdownCh

	listener.Close()
	mlog.Infof("sync daemon exiting")
	return nil
}

func (sd *SyncDaemon) acceptIPC(listener net.Listener) { acceptIPC(sd, listener) }

func (sd *SyncDaemon) handleConn(conn net.Conn) { handleIPCConn(sd, conn) }

func (sd *SyncDaemon) ipcName() string                { return "sync daemon" }
func (sd *SyncDaemon) ipcToken() string               { return sd.token }
func (sd *SyncDaemon) ipcShutdownCh() <-chan struct{} { return sd.shutdownCh }
func (sd *SyncDaemon) endMutation()                   { sd.ipcWG.Done() }
func (sd *SyncDaemon) setPinned(remotePath string, pinned bool) {
	sd.cacheDB.SetPinned(remotePath, pinned)
}
func (sd *SyncDaemon) flushWriteback(budget time.Duration) (int, error) {
	return sd.writeback.FlushAll(budget)
}
func (sd *SyncDaemon) cleanRejected() (int, error) { return sd.vfs.CleanRejected() }

func (sd *SyncDaemon) handleExtra(req mount.DaemonRequest, enc *json.Encoder) bool {
	switch req.Action {
	case "resolve":
		if err := sd.resolveConflict(req.Path, req.Choice); err != nil {
			enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(mount.DaemonResponse{OK: true})
		}
	case "files":
		sd.handleFiles(enc)
	case "conflicts":
		sd.handleConflicts(enc)
	case "activity":
		enc.Encode(mount.DaemonResponse{OK: true, Activity: sd.activity.snapshot()})
	default:
		return false
	}
	return true
}

func (sd *SyncDaemon) retryFailed(remotePath string) (int, error) {
	cleared, err := cache.ClearFailedTransfers(sd.cacheDB, strings.TrimPrefix(remotePath, "/"))
	if err != nil {
		return 0, err
	}
	for _, in := range cleared {
		node := sd.vfs.NodeByID(in.ID)
		if in.Dirty {
			sd.cacheDB.SetSyncStatus(in.ID, cache.StatusPending, "")
			if err := sd.cacheDB.EnqueueWriteback(in.ID, "upload", in.RemotePath, ""); err != nil &&
				!errors.Is(err, cache.ErrReadOnlyMount) {
				mlog.Warnf("retry: re-enqueue %s: %v", in.RemotePath, err)
			}
			if node != nil {
				node.Mu.Lock()
				node.SyncStatus, node.StatusReason, node.Dirty = cache.StatusPending, "", true
				node.Mu.Unlock()
			}
			continue
		}
		sd.cacheDB.SetSyncStatus(in.ID, cache.StatusSynced, "")
		if node != nil {
			node.Mu.Lock()
			node.SyncStatus, node.StatusReason, node.Cached = cache.StatusSynced, "", false
			node.Mu.Unlock()
		}
	}
	return len(cleared), nil
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
		cache.ReleaseBlob(sd.cacheDB, sd.store, inode.ContentHash, inode.ID)
		setNode(cache.StatusSynced, false)
		return nil
	}

	switch choice {
	case "local":
		if sd.cacheDB.WritebackDisabled() {
			return cache.ErrReadOnlyMount
		}
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
		cache.ReleaseBlob(sd.cacheDB, sd.store, inode.ContentHash, inode.ID)
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
	cacheUsed := cache.CacheBytes(sd.cacheDB, sd.store)
	pending, _ := sd.cacheDB.PendingWritebackCount()
	failed, _ := sd.cacheDB.FailedWritebackCount()
	failedDownloads, _ := sd.cacheDB.FailedDownloadCount()

	lastPoll := sd.poller.LastPoll()
	lastPollStr := ""
	if !lastPoll.IsZero() {
		lastPollStr = fmt.Sprintf("%ds ago", int(time.Since(lastPoll).Seconds()))
	}

	uptime := time.Since(sd.startedAt).Round(time.Second).String()

	enc.Encode(mount.DaemonResponse{
		OK:                  true,
		Online:              sd.poller.IsOnline(),
		MountPoint:          sd.mountPoint,
		RemotePath:          "/" + sd.remotePath,
		Mode:                mount.ModeSync,
		SyncDir:             sd.syncDir,
		CacheUsed:           cacheUsed,
		CacheMax:            sd.cacheMax,
		PendingCount:        pending,
		FailedCount:         failed,
		FailedDownloadCount: failedDownloads,
		LastPoll:            lastPollStr,
		Uptime:              uptime,
	})
}

func (sd *SyncDaemon) wireActivity() {
	record := func(path, direction string, bytes int64, err error) {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		sd.activity.add(mount.ActivityEvent{
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
		enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		return
	}
	enc.Encode(mount.DaemonResponse{OK: true, Files: entries})
}

func (sd *SyncDaemon) handleConflicts(enc *json.Encoder) {
	entries, err := buildConflictEntries(sd.cacheDB)
	if err != nil {
		enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		return
	}
	enc.Encode(mount.DaemonResponse{OK: true, Files: entries})
}

func fileEntryFromInode(in *cache.Inode) mount.FileEntry {
	return mount.FileEntry{
		Path:   in.RemotePath,
		Status: string(in.SyncStatus),
		Dirty:  in.Dirty,
		Size:   in.Size,
		Pinned: in.Pinned,
		Reason: in.StatusReason,
	}
}

func buildFileEntries(db *cache.DB) ([]mount.FileEntry, error) {
	inodes, err := db.AllInodes()
	if err != nil {
		return nil, err
	}
	entries := make([]mount.FileEntry, 0, len(inodes))
	for _, in := range inodes {
		entries = append(entries, fileEntryFromInode(in))
	}
	return entries, nil
}

func buildConflictEntries(db *cache.DB) ([]mount.FileEntry, error) {
	issues, err := db.ListIssues()
	if err != nil {
		return nil, err
	}
	entries := make([]mount.FileEntry, 0)
	for _, in := range issues {
		if in.SyncStatus == cache.StatusConflict {
			entries = append(entries, fileEntryFromInode(in))
		}
	}
	return entries, nil
}

func (sd *SyncDaemon) shutdown() {
	sd.shutdownOnce.Do(func() {
		mlog.Infof("sync daemon shutting down")

		sd.svcMu.Lock()
		sd.stopped = true
		sd.svcMu.Unlock()
		sd.vfs.Shutdown()
		drainIPC(&sd.ipcWG, "sync daemon")

		if sd.initialSyncCancel != nil {
			sd.initialSyncCancel()
		}
		select {
		case <-sd.initialSyncDone:
		case <-time.After(initialSyncStopWait):
			mlog.Warnf("sync daemon: initial sync still running %v after cancel; proceeding with shutdown", initialSyncStopWait)
		}

		sd.writeback.FlushAll(mount.FlushBudget)
		sd.writeback.Stop()
		sd.watcher.Stop()
		sd.reconciler.Stop()
		sd.downloader.Stop()
		sd.poller.Stop()

		if err := sd.mapper.Unmap(sd.mountPoint); err != nil {
			mlog.Warnf("sync daemon: unmap warning: %v", err)
		}

		sd.cleanup()
		close(sd.shutdownCh)
	})
}

func (sd *SyncDaemon) cleanup() {
	sd.store.Close()
	sd.cacheDB.Close()
	mount.EvictMountEntry(sd.mountInfo)
}
