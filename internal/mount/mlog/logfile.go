package mlog

import (
	"os"
	"strings"
	"sync"
)

const MaxLogSize = 5 << 20

func FatalLogPath(logPath string) string {
	return strings.TrimSuffix(logPath, ".log") + ".fatal.log"
}

func OpenLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > MaxLogSize {
		rotate(path)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}

func rotate(path string) error {
	return os.Rename(path, path+".1")
}

type RotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func NewRotatingLog(path string) (*RotatingLog, error) {
	f, err := OpenLog(path)
	if err != nil {
		return nil, err
	}
	var size int64
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	return &RotatingLog{path: path, f: f, size: size}, nil
}

func (r *RotatingLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > MaxLogSize {
		r.rotateLocked()
	}
	if r.f == nil {
		return len(p), nil
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingLog) rotateLocked() {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	rotate(r.path)
	if f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
		r.f = f
		r.size = 0
	}
}

func (r *RotatingLog) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
