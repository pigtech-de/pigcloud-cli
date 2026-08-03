package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount/cache"
	"pigcloud/internal/mount/vfs"
)

type Poller struct {
	vfs      *vfs.VFS
	client   *api.Client
	cacheDB  *cache.DB
	interval time.Duration

	mu       sync.Mutex
	online   bool
	lastPoll time.Time
	cancel   context.CancelFunc

	localDelete func(remotePath string)
}

func (p *Poller) SetLocalDelete(fn func(remotePath string)) {
	p.localDelete = fn
}

func NewPoller(v *vfs.VFS, client *api.Client, cacheDB *cache.DB, interval time.Duration) *Poller {
	return &Poller{
		vfs:      v,
		client:   client,
		cacheDB:  cacheDB,
		interval: interval,
		online:   true,
	}
}

func (p *Poller) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go p.loop(ctx)
}

func (p *Poller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *Poller) IsOnline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.online
}

func (p *Poller) LastPoll() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastPoll
}

func (p *Poller) loop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("poller PANIC: %v\n%s", r, buf[:n])
		}
	}()

	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	err := p.pollRecursive(ctx, p.vfs.Root)

	p.mu.Lock()
	if err != nil {
		p.online = false
	} else {
		p.online = true
		p.lastPoll = time.Now()
	}
	p.mu.Unlock()

	p.vfs.Online = p.online
}

func (p *Poller) pollRecursive(ctx context.Context, parent *vfs.Node) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !parent.Loaded {
		return nil
	}

	source := "/"
	if parent.RemotePath != "" {
		source = "/" + parent.RemotePath
	}

	options := map[string]string{"source": source}
	addPathTokens(options, parent.RemotePath, p.vfs.NameKey)

	resp, err := p.client.Execute(ctx, "ls", options)
	if err != nil {
		return fmt.Errorf("poll %s: %w", source, err)
	}
	if !resp.Success {
		return fmt.Errorf("poll %s: %s", source, resp.Message)
	}

	var payload api.ListPayload
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return fmt.Errorf("parse poll response: %w", err)
	}

	remoteEntries := make(map[string]*api.ListEntry)
	decryptFailed := false
	for i := range payload.Entries {
		entry := &payload.Entries[i]
		name := decryptName(entry.E2EEDisplayName, p.vfs.PrivateKey)
		if name == "(encrypted)" || (name == "" && entry.E2EEDisplayName != "") {
			decryptFailed = true
			continue
		}
		if name == "" || vfs.IsExcludedDir(name) {
			continue
		}
		remoteEntries[name] = entry
	}

	parent.Mu.Lock()
	children := make(map[string]*vfs.Node)
	for k, v := range parent.Children {
		children[k] = v
	}
	parent.Mu.Unlock()

	for name, entry := range remoteEntries {
		existing := children[name]
		if existing != nil {
			existing.Mu.Lock()
			dbInode, _ := p.cacheDB.GetInode(existing.ID)
			locallyDirty := existing.Dirty || (dbInode != nil && dbInode.Dirty)

			newSize, haveSize := plaintextSizeOf(entry)
			var remoteMtime time.Time
			haveMtime := false
			if entry.Modified != nil {
				if t, err := time.Parse(time.RFC3339, *entry.Modified); err == nil {
					remoteMtime, haveMtime = t, true
				}
			}
			sizeChanged := haveSize && newSize != existing.Size
			mtimeAdvanced := haveMtime && remoteMtime.After(existing.Mtime)
			conflict := locallyDirty && (sizeChanged || mtimeAdvanced)
			if conflict {
				if existing.SyncStatus != cache.StatusConflict {
					existing.SyncStatus = cache.StatusConflict
					existing.StatusReason = "remote changed while local edits pending"
					p.cacheDB.SetSyncStatus(existing.ID, cache.StatusConflict, existing.StatusReason)
					log.Printf("poller: conflict on %s (remote changed, local edits pending)", existing.RemotePath)
				}
			} else if !locallyDirty && (sizeChanged || mtimeAdvanced) {
				if haveSize {
					existing.Size = newSize
				}
				existing.Cached = false
				existing.ContentHash = ""
				p.cacheDB.InvalidateCache(existing.ID)
			}
			if haveMtime && !conflict {
				existing.Mtime = remoteMtime
			}
			existing.Mu.Unlock()
		} else {
			isDir := entry.Type == "directory"
			childPath := joinPath(parent.RemotePath, name)
			var node *vfs.Node
			if isDir {
				node = vfs.NewDirNode(name, childPath, parent)
			} else {
				size, _ := plaintextSizeOf(entry)
				mtime := time.Now()
				if entry.Modified != nil {
					if t, err := time.Parse(time.RFC3339, *entry.Modified); err == nil {
						mtime = t
					}
				}
				node = vfs.NewFileNode(name, childPath, size, mtime, parent)
			}

			inode := &cache.Inode{
				RemotePath:  node.RemotePath,
				DisplayName: name,
				IsDir:       isDir,
				Size:        node.Size,
				Mtime:       node.Mtime.Unix(),
				SyncStatus:  cache.StatusSynced,
				ParentID:    parent.ID,
			}
			id, _ := p.cacheDB.UpsertInode(inode)
			node.ID = id

			parent.AddChild(node)

			if isDir {
				if _, err := p.vfs.Readdir(node); err != nil {
					log.Printf("poller: load new dir %s: %v", node.RemotePath, err)
				}
			}
		}
	}

	if decryptFailed {
		log.Printf("poller: %s has undecryptable entries; skipping prune this cycle", source)
	} else {
		for name, child := range children {
			if _, exists := remoteEntries[name]; !exists && !child.Dirty {
				parent.RemoveChild(name)
				if p.localDelete != nil {
					p.localDelete(child.RemotePath)
				}
				cache.ReleaseBlob(p.cacheDB, p.vfs.Store, child.ContentHash, child.ID)
				p.cacheDB.DeleteInode(child.RemotePath)
			}
		}
	}

	for name := range remoteEntries {
		child := parent.GetChild(name)
		if child != nil && child.IsDir && child.Loaded {
			if err := p.pollRecursive(ctx, child); err != nil {
				return err
			}
		}
	}

	return nil
}

func decryptName(e2eeB64 string, priv *crypto.PrivateKeySet) string {
	if e2eeB64 == "" {
		return ""
	}
	sealed, err := base64.StdEncoding.DecodeString(e2eeB64)
	if err != nil {
		return "(encrypted)"
	}
	name, err := crypto.UnsealDisplayName(sealed, priv)
	if err != nil {
		return "(encrypted)"
	}
	if !vfs.IsSafeName(name) {
		return "(encrypted)"
	}
	return name
}

func addPathTokens(options map[string]string, remotePath string, nameKey []byte) {
	if remotePath == "" || nameKey == nil {
		return
	}
	tokens := make(map[string]string)
	parts := strings.Split(remotePath, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = current + "/" + part
		}
		token, err := crypto.ComputePathToken(nameKey, current)
		if err != nil {
			continue
		}
		tokens[current] = fmt.Sprintf("%x", token)
	}
	if len(tokens) > 0 {
		data, _ := json.Marshal(tokens)
		options["path_tokens"] = string(data)
	}
}

func plaintextSizeOf(entry *api.ListEntry) (int64, bool) {
	if entry.PlaintextSize != nil {
		return *entry.PlaintextSize, true
	}
	return 0, false
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "/" + child
}
