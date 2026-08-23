package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"pigcloud/internal/api"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/mlog"
	"pigcloud/internal/mount/syncer"
	"pigcloud/internal/mount/vfs"
	"pigcloud/internal/netutil"
	"strings"
	"sync"
	"syscall"
	"time"
)

func isMutatingIPC(action string) bool {
	switch action {
	case "pin", "unpin", "flush", "clean", "resolve", "retry":
		return true
	}
	return false
}

func drainIPC(wg *sync.WaitGroup, name string) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(mount.FlushDeadline):
		mlog.Warnf("%s: IPC handlers still busy after %v; proceeding with shutdown", name, mount.FlushDeadline)
	}
}

type Daemon struct {
	vfs       *vfs.VFS
	cacheDB   *cache.DB
	store     *cache.Store
	evictor   *cache.Evictor
	backend   mount.MountBackend
	poller    *syncer.Poller
	writeback *syncer.WritebackProcessor

	mountPoint string
	remotePath string
	cacheDir   string
	cacheMax   int64
	startedAt  time.Time
	token      string
	mountInfo  *mount.MountInfo

	shutdownOnce sync.Once
	shutdownCh   chan struct{}

	stopMu   sync.Mutex
	stopping bool
	ipcWG    sync.WaitGroup
}

func (d *Daemon) beginMutation() bool {
	d.stopMu.Lock()
	defer d.stopMu.Unlock()
	if d.stopping {
		return false
	}
	d.ipcWG.Add(1)
	return true
}

type Config struct {
	MountPoint        string
	RemotePath        string
	CacheSize         int64
	PollInterval      time.Duration
	PublicKey         *crypto.PublicKeySet
	PrivateKey        *crypto.PrivateKeySet
	NameKey           []byte
	SigningPublicKey  *crypto.SigningPublicKeySet
	SigningPrivateKey *crypto.SigningPrivateKeySet
	Mode              string
	ReadOnly          bool
}

func (cfg *Config) ownerID() string {
	if cfg.PublicKey == nil {
		return ""
	}
	return crypto.AccountFingerprint(cfg.PublicKey.X25519[:])
}

var logFile string

func ServeVirtual(cfg *Config) error {
	logFile = mount.MountLogPath(cfg.ownerID(), cfg.RemotePath)
	if lf, err := mlog.NewRotatingLog(logFile); err == nil {
		mlog.SetOutput(lf)
		defer lf.Close()
	}
	mlog.Infof("mount daemon starting: remote=%s mountpoint=%s level=%s", cfg.RemotePath, cfg.MountPoint, mlog.CurrentLevel())

	defer mlog.RecoverPanic("mount daemon")

	mountID := make([]byte, 8)
	rand.Read(mountID)
	cacheDir := filepath.Join(mount.DataDir(), "mount-cache", hex.EncodeToString(mountID))
	mount.CleanStaleMountCaches(cacheDir)

	cacheDB, err := cache.Open(cacheDir)
	if err != nil {
		return fmt.Errorf("open cache DB: %w", err)
	}

	store, err := cache.NewStore(cacheDir)
	if err != nil {
		cacheDB.Close()
		return fmt.Errorf("create cache store: %w", err)
	}

	if n, err := cacheDB.RequeueInProgress(); err == nil && n > 0 {
		mlog.Infof("mount daemon: requeued %d stranded writeback entries", n)
	}
	if swept, err := cache.GCOrphans(cacheDB, store); err == nil {
		if swept.Blobs > 0 {
			mlog.Infof("mount daemon: removed %d orphan cache blobs", swept.Blobs)
		}
		if swept.Temps > 0 {
			mlog.Infof("mount daemon: removed %d stranded cache temp files", swept.Temps)
		}
	}

	evictor := cache.NewEvictor(cacheDB, store, cfg.CacheSize)
	client := api.NewClient()

	v := vfs.New(cfg.RemotePath, cacheDB, store, evictor, client,
		cfg.PublicKey, cfg.PrivateKey, cfg.NameKey, cfg.SigningPublicKey, cfg.SigningPrivateKey)
	v.SetReadOnly(cfg.ReadOnly)

	poller := syncer.NewPoller(v, client, cacheDB, cfg.PollInterval)
	writeback := syncer.NewWritebackProcessor(v, client, cacheDB, store, "")

	backend := mount.NewBackend()

	d := &Daemon{
		vfs:        v,
		cacheDB:    cacheDB,
		store:      store,
		evictor:    evictor,
		backend:    backend,
		poller:     poller,
		writeback:  writeback,
		mountPoint: cfg.MountPoint,
		remotePath: cfg.RemotePath,
		cacheDir:   cacheDir,
		cacheMax:   cfg.CacheSize,
		startedAt:  time.Now(),
		shutdownCh: make(chan struct{}),
	}

	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	d.token = hex.EncodeToString(tokenBytes)

	listener, err := net.Listen("tcp", netutil.LoopbackAny)
	if err != nil {
		d.cleanup()
		return fmt.Errorf("start IPC server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	info := &mount.MountInfo{
		Port:       port,
		Token:      d.token,
		PID:        os.Getpid(),
		MountPoint: cfg.MountPoint,
		RemotePath: cfg.RemotePath,
		CacheDir:   cacheDir,
		Mode:       mount.ModeVirtual,
		Owner:      cfg.ownerID(),
		StartedAt:  d.startedAt,
	}
	if err := mount.WriteMountEntry(info); err != nil {
		listener.Close()
		d.cleanup()
		return fmt.Errorf("write mount entry: %w", err)
	}
	d.mountInfo = info

	poller.Start()
	writeback.Start()

	go d.acceptIPC(listener)

	sigCtx, sigStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigStop()
	go func() {
		<-sigCtx.Done()
		mlog.Infof("mount daemon: shutdown signal received")
		d.shutdown()
	}()

	mlog.Infof("mount daemon ready: pid=%d port=%d mountpoint=%s", os.Getpid(), port, cfg.MountPoint)

	mountErr := backend.Mount(cfg.MountPoint, v)

	if mountErr != nil {
		mlog.Errorf("mount returned error: %v", mountErr)
	} else {
		mlog.Infof("mount returned (unmounted)")
	}

	d.shutdown()
	listener.Close()

	mlog.Infof("mount daemon exiting")
	return mountErr
}

func (d *Daemon) acceptIPC(listener net.Listener) { acceptIPC(d, listener) }

func (d *Daemon) handleConn(conn net.Conn) { handleIPCConn(d, conn) }

func (d *Daemon) ipcName() string                { return "mount daemon" }
func (d *Daemon) ipcToken() string               { return d.token }
func (d *Daemon) ipcShutdownCh() <-chan struct{} { return d.shutdownCh }
func (d *Daemon) endMutation()                   { d.ipcWG.Done() }
func (d *Daemon) setPinned(remotePath string, pinned bool) {
	d.cacheDB.SetPinned(remotePath, pinned)
}
func (d *Daemon) flushWriteback(budget time.Duration) (int, error) {
	return d.writeback.FlushAll(budget)
}
func (d *Daemon) cleanRejected() (int, error) { return d.vfs.CleanRejected() }

func (d *Daemon) handleExtra(mount.DaemonRequest, *json.Encoder) bool { return false }

func (d *Daemon) retryFailed(remotePath string) (int, error) {
	cleared, err := cache.ClearFailedTransfers(d.cacheDB, strings.TrimPrefix(remotePath, "/"))
	if err != nil {
		return 0, err
	}
	for _, in := range cleared {
		d.cacheDB.SetSyncStatus(in.ID, cache.StatusPending, "")
		if node := d.vfs.NodeByID(in.ID); node != nil {
			node.Mu.Lock()
			node.SyncStatus = cache.StatusPending
			node.StatusReason = ""
			node.Mu.Unlock()
		}
	}
	return len(cleared), nil
}

func (d *Daemon) handleStatus(enc *json.Encoder) {
	cacheUsed := cache.CacheBytes(d.cacheDB, d.store)
	pending, _ := d.cacheDB.PendingWritebackCount()
	failed, _ := d.cacheDB.FailedWritebackCount()

	lastPoll := d.poller.LastPoll()
	lastPollStr := ""
	if !lastPoll.IsZero() {
		lastPollStr = fmt.Sprintf("%ds ago", int(time.Since(lastPoll).Seconds()))
	}

	uptime := time.Since(d.startedAt).Round(time.Second).String()

	enc.Encode(mount.DaemonResponse{
		OK:           true,
		Online:       d.poller.IsOnline(),
		MountPoint:   d.mountPoint,
		RemotePath:   "/" + mount.NormalizeRemotePath(d.remotePath),
		Mode:         mount.ModeVirtual,
		CacheUsed:    cacheUsed,
		CacheMax:     d.cacheMax,
		PendingCount: pending,
		FailedCount:  failed,
		LastPoll:     lastPollStr,
		Uptime:       uptime,
	})
}

func (d *Daemon) shutdown() {
	d.shutdownOnce.Do(func() {
		close(d.shutdownCh)
		d.vfs.Shutdown()

		d.stopMu.Lock()
		d.stopping = true
		d.stopMu.Unlock()
		drainIPC(&d.ipcWG, "mount daemon")

		d.writeback.FlushAll(mount.FlushBudget)
		d.writeback.Stop()
		d.poller.Stop()

		d.backend.Unmount()

		d.cleanup()
	})
}

func (d *Daemon) cleanup() {
	d.store.Close()
	d.cacheDB.Close()
	mount.EvictMountEntry(d.mountInfo)
	if d.cacheDir != "" {
		os.RemoveAll(d.cacheDir)
	}
}
