package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func SpawnBackground(
	pubKeyHex, privKeyHex,
	kyberPubHex, kyberSeedHex,
	nameKeyHex string,
	signingPubEdHex, signingPrivEdHex,
	signingPubMlHex, signingPrivMlHex string,
	ttlSeconds int,
) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(exePath, "__agent-serve",
		"--pub", pubKeyHex,
		"--priv", privKeyHex,
		"--kyber-pub", kyberPubHex,
		"--kyber-seed", kyberSeedHex,
		"--name-key", nameKeyHex,
		"--sign-pub-ed", signingPubEdHex,
		"--sign-priv-ed", signingPrivEdHex,
		"--sign-pub-ml", signingPubMlHex,
		"--sign-priv-ml", signingPrivMlHex,
		"--ttl", fmt.Sprintf("%d", ttlSeconds),
	)

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = windowsDetachAttr()
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	cmd.Process.Release()
	return nil
}
