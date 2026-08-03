package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pigcloud/internal/mount/cache"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestReconcilerHealsDrift(t *testing.T) {
	syncDir := t.TempDir()
	db, err := cache.Open(filepath.Join(syncDir, ".pigcloud"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	r := NewReconciler(syncDir, "", db, time.Minute)

	writeFile(t, filepath.Join(syncDir, "untracked.txt"), "hello")

	writeFile(t, filepath.Join(syncDir, "failed.txt"), "retry me")
	db.UpsertInode(&cache.Inode{RemotePath: "failed.txt", DisplayName: "failed.txt", Size: 8, SyncStatus: cache.StatusFailed})

	writeFile(t, filepath.Join(syncDir, "edited.txt"), "now longer than before")
	db.UpsertInode(&cache.Inode{RemotePath: "edited.txt", DisplayName: "edited.txt", Size: 3, Cached: true, ContentHash: "oldhash", SyncStatus: cache.StatusSynced})

	writeFile(t, filepath.Join(syncDir, "clean.txt"), "stable")
	db.UpsertInode(&cache.Inode{RemotePath: "clean.txt", DisplayName: "clean.txt", Size: 6, Cached: true, ContentHash: "h", SyncStatus: cache.StatusSynced})

	healed := r.Reconcile(context.Background())
	if healed != 3 {
		t.Fatalf("healed=%d, want 3 (untracked, failed, edited)", healed)
	}

	entries, err := db.DequeueWriteback(100, 0)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	queued := map[string]bool{}
	for _, e := range entries {
		if e.Action == "upload" {
			queued[e.RemotePath] = true
		}
	}
	for _, p := range []string{"untracked.txt", "failed.txt", "edited.txt"} {
		if !queued[p] {
			t.Errorf("%s was not queued for upload", p)
		}
	}
	if queued["clean.txt"] {
		t.Error("clean.txt (size matches) must not be re-queued")
	}

	if in, _ := db.GetInodeByPath("failed.txt"); in == nil || in.SyncStatus != cache.StatusPending {
		t.Errorf("failed.txt not reset to pending")
	}
	if in, _ := db.GetInodeByPath("edited.txt"); in == nil || in.ContentHash != "" || in.Size != 22 {
		t.Errorf("edited.txt re-queue should clear hash and refresh size, got %+v", in)
	}
}

func TestReconcilerIgnoresMetaDir(t *testing.T) {
	syncDir := t.TempDir()
	db, err := cache.Open(filepath.Join(syncDir, ".pigcloud"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	r := NewReconciler(syncDir, "", db, time.Minute)
	if healed := r.Reconcile(context.Background()); healed != 0 {
		t.Fatalf("healed=%d, want 0 (only .pigcloud present)", healed)
	}
	if entries, _ := db.DequeueWriteback(100, 0); len(entries) != 0 {
		t.Fatalf("expected empty queue, got %d entries", len(entries))
	}
}
