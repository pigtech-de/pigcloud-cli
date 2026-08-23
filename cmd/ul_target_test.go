package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"pigcloud/internal/api"
	"pigcloud/internal/config"
)

type recordingUploadServer struct {
	mu        sync.Mutex
	ulPaths   []string
	mkPaths   []string
	mkParents []string
	inPaths   []string
	chunkPuts int
	inExists bool
}

func (s *recordingUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		_ = r.ParseMultipartForm(1 << 20)
		s.mu.Lock()
		s.chunkPuts++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success":true,"chunkReceived":true}`)
		return
	}
	if r.URL.Query().Get("action") == "auth-csrf" {
		http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "s", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success":true,"csrfToken":"tok"}`)
		return
	}

	var req api.CLIRequest
	if meta := r.Header.Get(api.HeaderCliMetadata); meta != "" {
		raw, err := base64.StdEncoding.DecodeString(meta)
		if err == nil {
			_ = json.Unmarshal(raw, &req)
		}
		_, _ = io.Copy(io.Discard, r.Body)
	} else {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
	}

	s.mu.Lock()
	switch req.Command {
	case "ul":
		s.ulPaths = append(s.ulPaths, req.Options["target"])
	case "mk":
		s.mkPaths = append(s.mkPaths, req.Options["source"])
		s.mkParents = append(s.mkParents, req.Options["parents"])
	case "in":
		s.inPaths = append(s.inPaths, req.Options["source"])
	}
	exists := s.inExists
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if req.Command == "in" && !exists {
		io.WriteString(w, `{"success":false,"message":"not found"}`)
		return
	}
	io.WriteString(w, `{"success":true,"name":"f.txt","storedPath":"/x/f.txt","storage":{"usedBytes":1,"limitBytes":2}}`)
}

func withTestEndpoint(t *testing.T, url string) {
	t.Helper()
	orig := config.GetConfigPath()
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"endpoint":` + strconv.Quote(url) + `,"api_key":"test-key","cwd":"/"}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	config.SetConfigFile(path)
	config.Load()
	t.Cleanup(func() {
		config.SetConfigFile(orig)
		config.Load()
	})
}

func TestRecursiveUploadTargetsParentDirectory(t *testing.T) {
	srv := &recordingUploadServer{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	withTestEndpoint(t, ts.URL)

	localDir := filepath.Join(t.TempDir(), "payload")
	if err := os.MkdirAll(filepath.Join(localDir, "sub"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, rel := range []string{"root.txt", filepath.Join("sub", "nested.txt")} {
		if err := os.WriteFile(filepath.Join(localDir, rel), []byte("data"), 0600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	runRecursiveUpload(context.Background(), localDir, "/Backups")

	srv.mu.Lock()
	got := append([]string(nil), srv.ulPaths...)
	srv.mu.Unlock()
	sort.Strings(got)

	want := []string{"/Backups/payload", "/Backups/payload/sub"}
	if len(got) != len(want) {
		t.Fatalf("uploaded %d files, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("upload target %d = %q, want the parent directory %q", i, got[i], want[i])
		}
	}
}

func TestForceCollisionRefusedBeforeTransfer(t *testing.T) {
	srv := &recordingUploadServer{inExists: true}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	withTestEndpoint(t, ts.URL)

	savedForce := ulForce
	ulForce = true
	defer func() { ulForce = savedForce }()

	localDir := filepath.Join(t.TempDir(), "payload")
	if err := os.MkdirAll(localDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(localDir, "big.bin"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(120 << 20); err != nil {
		f.Close()
		t.Fatalf("truncate: %v", err)
	}
	f.Close()
	if !api.UploadIsChunked(120 << 20) {
		t.Fatal("fixture is not above the chunk threshold")
	}

	runRecursiveUpload(context.Background(), localDir, "/Backups")

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.ulPaths) != 0 {
		t.Errorf("transferred %d uploads before refusing the collision: %v", len(srv.ulPaths), srv.ulPaths)
	}
	if srv.chunkPuts != 0 {
		t.Errorf("sent %d chunk parts before refusing the collision", srv.chunkPuts)
	}
	if len(srv.inPaths) == 0 {
		t.Error("never probed for the collision")
	}
}

func TestRecursiveUploadCreatesEveryRemoteDirectoryWithParents(t *testing.T) {
	srv := &recordingUploadServer{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	withTestEndpoint(t, ts.URL)

	localDir := filepath.Join(t.TempDir(), "payload")
	if err := os.MkdirAll(filepath.Join(localDir, "sub", "deep"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "sub", "deep", "f.txt"), []byte("data"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	runRecursiveUpload(context.Background(), localDir, "/Backups")

	srv.mu.Lock()
	paths := append([]string(nil), srv.mkPaths...)
	parents := append([]string(nil), srv.mkParents...)
	srv.mu.Unlock()

	want := []string{"/Backups/payload", "/Backups/payload/sub", "/Backups/payload/sub/deep"}
	if len(paths) != len(want) {
		t.Fatalf("mkdir calls = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("mkdir %d = %q, want %q (a parent created after its child cannot resolve)", i, paths[i], want[i])
		}
		if parents[i] != "true" {
			t.Errorf("mkdir %q sent parents=%q, want true", paths[i], parents[i])
		}
	}
}
