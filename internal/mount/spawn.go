package mount

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type SpawnKeys struct {
	PubHex        string `json:"pub"`
	PrivHex       string `json:"priv"`
	KyberPubHex   string `json:"kyber_pub"`
	KyberSeedHex  string `json:"kyber_seed"`
	NameKeyHex    string `json:"name_key"`
	SignPubEdHex  string `json:"sign_pub_ed"`
	SignPrivEdHex string `json:"sign_priv_ed"`
	SignPubMlHex  string `json:"sign_pub_ml"`
	SignPrivMlHex string `json:"sign_priv_ml"`
}

func SpawnBackground(remotePath, mountPoint string, keys SpawnKeys,
	cacheSizeBytes int64, pollSeconds int, mode string, readOnly bool) error {

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	payload, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("marshal spawn keys: %w", err)
	}

	args := []string{"__mount-serve",
		"--remote", remotePath,
		"--mountpoint", mountPoint,
		"--cache-size", fmt.Sprintf("%d", cacheSizeBytes),
		"--poll", fmt.Sprintf("%d", pollSeconds),
		"--mode", mode,
	}
	if readOnly {
		args = append(args, "--read-only")
	}
	cmd := exec.Command(exePath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil

	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = mountDetachAttr()
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("start mount daemon: %w", err)
	}

	if _, err := stdin.Write(payload); err != nil {
		stdin.Close()
		return fmt.Errorf("write spawn keys: %w", err)
	}
	stdin.Close()

	cmd.Process.Release()
	return nil
}
