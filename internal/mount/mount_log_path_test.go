package mount

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMountLogPathKeyedLikeRegistryEntry(t *testing.T) {
	owner, remote := "ownerfp", "/Photos"

	p := MountLogPath(owner, remote)
	if !strings.HasSuffix(p, ".log") {
		t.Fatalf("log path %q lacks .log suffix", p)
	}
	if !strings.Contains(filepath.Base(p), mountKey(owner, remote)) {
		t.Fatalf("log path %q not keyed by mountKey", p)
	}
	if strings.TrimSuffix(entryFileName(owner, remote), ".json") != mountKey(owner, remote) {
		t.Fatalf("registry entry and log key diverged")
	}
	if MountLogPath("other", remote) == p || MountLogPath(owner, "/Docs") == p {
		t.Fatalf("log path collided across mounts")
	}
}
