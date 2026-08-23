package agent

import (
	"os"
	"path/filepath"
)

const maxAgentLogSize = 1 << 20

func agentLogPath() string {
	return filepath.Join(filepath.Dir(agentFilePath()), "agent.log")
}

func openAgentLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxAgentLogSize {
		os.Rename(path, path+".1")
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}
