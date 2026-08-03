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
	"strconv"
	"sync"
	"syscall"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount/cache"
	msync "pigcloud/internal/mount/sync"
	"pigcloud/internal/mount/vfs"
)

const (
	ModeSync    = "sync"
	ModeVirtual = "virtual"
)

const flushDeadline = 35 * time.Second

type MountInfo struct {
	Port       int       `json:"port"`
	Token      string    `json:"token"`
	PID        int       `json:"pid"`
	MountPoint string    `json:"mount_point"`
	RemotePath string    `json:"remote_path"`
	CacheDir   string    `json:"cache_dir"`
	SyncDir    string    `json:"sync_dir,omitempty"`
	Mode       string    `json:"mode"`
	Owner      string    `json:"owner,omitempty"`
	StartedAt  time.Time `json:"started_at"`

	Source string `json:"-"`
}

type DaemonRequest struct {
	Token  string `json:"token"`
	Action string `json:"action"`
	Path   string `json:"path,omitempty"`
	Choice string `json:"choice,omitempty"`
}

type FileEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Dirty  bool   `json:"dirty,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type DaemonResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	Online       bool   `json:"online,omitempty"`
	MountPoint   string `json:"mount_point,omitempty"`
	RemotePath   string `json:"remote_path,omitempty"`
	Mode         string `json:"mode,omitempty"`
	SyncDir      string `json:"sync_dir,omitempty"`
	CacheUsed    int64  `json:"cache_used,omitempty"`
	CacheMax     int64  `json:"cache_max,omitempty"`
	PendingCount int    `json:"pending_count,omitempty"`
	FailedCount  int    `json:"failed_count,omitempty"`
	LastPoll     string `json:"last_poll,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
	Cleaned      int    `json:"cleaned,omitempty"`

	Files    []FileEntry     `json:"files,omitempty"`
	Activity []ActivityEvent `json:"activity,omitempty"`
}

type Daemon struct {
	vfs       *vfs.VFS
	cacheDB   *cache.DB
	store     *cache.Store
	evictor   *cache.Evictor
	backend   MountBackend
	poller    *msync.Poller
	writeback *msync.WritebackProcessor

	mountPoint string
	remotePath string
	cacheDir   string
	cacheMax   int64
	startedAt  time.Time
	token      string
	mountInfo  *MountInfo

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

type DaemonConfig struct {
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

func (cfg *DaemonConfig) ownerID() string {
	if cfg.PublicKey == nil {
		return ""
	}
	return crypto.AccountFingerprint(cfg.PublicKey.X25519[:])
}

var logFile string

func ServeDaemon(cfg *DaemonConfig) error {
	logFile = filepath.Join(configDir(), "mount.log")
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err == nil {
		log.SetOutput(lf)
		os.Stderr = lf
		defer lf.Close()
	}
	log.Printf("mount daemon starting: remote=%s mountpoint=%s", cfg.RemotePath, cfg.MountPoint)

	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("PANIC: %v\n%s", r, buf[:n])
		}
	}()

	mountID := make([]byte, 8)
	rand.Read(mountID)
	cacheDir := filepath.Join(dataDir(), "mount-cache", hex.EncodeToString(mountID))
	cleanStaleMountCaches(cacheDir)

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
		log.Printf("mount daemon: requeued %d stranded writeback entries", n)
	}
	if n, err := cache.GCOrphans(cacheDB, store); err == nil && n > 0 {
		log.Printf("mount daemon: removed %d orphan cache blobs", n)
	}

	evictor := cache.NewEvictor(cacheDB, store, cfg.CacheSize)
	client := api.NewClient()

	v := vfs.New(cfg.RemotePath, cacheDB, store, evictor, client,
		cfg.PublicKey, cfg.PrivateKey, cfg.NameKey, cfg.SigningPublicKey, cfg.SigningPrivateKey)
	v.ReadOnly = cfg.ReadOnly

	poller := msync.NewPoller(v, client, cacheDB, cfg.PollInterval)
	writeback := msync.NewWritebackProcessor(v, client, cacheDB, store, "")

	backend := NewBackend()

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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		d.cleanup()
		return fmt.Errorf("start IPC server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	info := &MountInfo{
		Port:       port,
		Token:      d.token,
		PID:        os.Getpid(),
		MountPoint: cfg.MountPoint,
		RemotePath: cfg.RemotePath,
		CacheDir:   cacheDir,
		Mode:       ModeVirtual,
		Owner:      cfg.ownerID(),
		StartedAt:  d.startedAt,
	}
	if err := WriteMountEntry(info); err != nil {
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
		log.Printf("mount daemon: shutdown signal received")
		d.shutdown()
	}()

	log.Printf("mount daemon ready: pid=%d port=%d mountpoint=%s", os.Getpid(), port, cfg.MountPoint)

	mountErr := backend.Mount(cfg.MountPoint, v)

	if mountErr != nil {
		log.Printf("mount returned error: %v", mountErr)
	} else {
		log.Printf("mount returned (unmounted)")
	}

	d.shutdown()
	listener.Close()

	log.Printf("mount daemon exiting")
	return mountErr
}

func (d *Daemon) acceptIPC(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-d.shutdownCh:
				return
			default:
				continue
			}
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req DaemonRequest
	if err := dec.Decode(&req); err != nil {
		enc.Encode(DaemonResponse{Error: "invalid request"})
		return
	}

	if req.Token != d.token {
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
		d.handleStatus(enc)

	case "shutdown":
		enc.Encode(DaemonResponse{OK: true})
		go func() {
			time.Sleep(100 * time.Millisecond)
			d.shutdown()
		}()

	case "pin":
		if req.Path != "" {
			d.cacheDB.SetPinned(req.Path, true)
		}
		enc.Encode(DaemonResponse{OK: true})

	case "unpin":
		if req.Path != "" {
			d.cacheDB.SetPinned(req.Path, false)
		}
		enc.Encode(DaemonResponse{OK: true})

	case "flush":
		flushed, err := d.writeback.FlushAll(30 * time.Second)
		if err != nil {
			enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(DaemonResponse{OK: true, PendingCount: flushed})
		}

	case "clean":
		count, err := d.vfs.CleanRejected()
		if err != nil {
			enc.Encode(DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(DaemonResponse{OK: true, Cleaned: count})
		}

	default:
		enc.Encode(DaemonResponse{Error: "unknown action"})
	}
}

func (d *Daemon) handleStatus(enc *json.Encoder) {
	cacheUsed, _ := d.cacheDB.TotalCacheSize()
	pending, _ := d.cacheDB.PendingWritebackCount()
	failed, _ := d.cacheDB.FailedWritebackCount()

	lastPoll := d.poller.LastPoll()
	lastPollStr := ""
	if !lastPoll.IsZero() {
		lastPollStr = fmt.Sprintf("%ds ago", int(time.Since(lastPoll).Seconds()))
	}

	uptime := time.Since(d.startedAt).Round(time.Second).String()

	enc.Encode(DaemonResponse{
		OK:           true,
		Online:       d.poller.IsOnline(),
		MountPoint:   d.mountPoint,
		RemotePath:   "/" + NormalizeRemotePath(d.remotePath),
		Mode:         ModeVirtual,
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

		d.writeback.FlushAll(30 * time.Second)
		d.writeback.Stop()
		d.poller.Stop()

		d.backend.Unmount()

		d.cleanup()
	})
}

func (d *Daemon) cleanup() {
	d.store.Close()
	d.cacheDB.Close()
	EvictMountEntry(d.mountInfo)
	if d.cacheDir != "" {
		os.RemoveAll(d.cacheDir)
	}
}

func mountFilePath() string {
	return filepath.Join(configDir(), "mount.json")
}

func configDir() string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(dir, "pigcloud")
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pigcloud")
}

func dataDir() string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(dir, "pigcloud")
	}
	return configDir()
}

func cleanStaleMountCaches(current string) {
	root := filepath.Join(dataDir(), "mount-cache")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if p := filepath.Join(root, e.Name()); p != current {
			os.RemoveAll(p)
		}
	}
}

func ReadMountFile() *MountInfo {
	data, err := os.ReadFile(mountFilePath())
	if err != nil {
		return nil
	}
	var info MountInfo
	if json.Unmarshal(data, &info) != nil {
		return nil
	}
	return &info
}

func RemoveMountFile() {
	os.Remove(mountFilePath())
}

func IsMounted() bool {
	for _, info := range ListMounts() {
		if IsMountReachable(info) {
			return true
		}
	}
	return false
}

func SendRequest(info *MountInfo, action string) (*DaemonResponse, error) {
	return SendRequestWithPath(info, action, "")
}

func SendRequestNoEvict(info *MountInfo, action string) (*DaemonResponse, error) {
	return sendRequestOpts(DaemonRequest{Token: info.Token, Action: action}, info, false)
}

func SendRequestWithPath(info *MountInfo, action, path string) (*DaemonResponse, error) {
	return sendRequest(DaemonRequest{Token: info.Token, Action: action, Path: path}, info)
}

func SendResolve(info *MountInfo, path, choice string) (*DaemonResponse, error) {
	return sendRequest(DaemonRequest{Token: info.Token, Action: "resolve", Path: path, Choice: choice}, info)
}

func sendRequest(req DaemonRequest, info *MountInfo) (*DaemonResponse, error) {
	return sendRequestOpts(req, info, true)
}

func sendRequestOpts(req DaemonRequest, info *MountInfo, evictOnDialFail bool) (*DaemonResponse, error) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(info.Port), 5*time.Second)
	if err != nil {
		if evictOnDialFail {
			EvictMountEntry(info)
		}
		return nil, err
	}
	defer conn.Close()
	deadline := 10 * time.Second
	if req.Action == "flush" {
		deadline = flushDeadline
	}
	conn.SetDeadline(time.Now().Add(deadline))

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		return nil, err
	}

	var resp DaemonResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
