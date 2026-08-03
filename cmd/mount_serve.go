package cmd

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"pigcloud/internal/crypto"
	"pigcloud/internal/mount"
	"pigcloud/internal/mount/cache"
)

var mountServeCmd = &cobra.Command{
	Use:    "__mount-serve",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		remotePath, _ := cmd.Flags().GetString("remote")
		mountPoint, _ := cmd.Flags().GetString("mountpoint")
		cacheSizeBytes, _ := cmd.Flags().GetInt64("cache-size")
		pollSec, _ := cmd.Flags().GetInt("poll")
		mode, _ := cmd.Flags().GetString("mode")
		readOnly, _ := cmd.Flags().GetBool("read-only")

		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read keys: %v\n", err)
			os.Exit(1)
		}
		var keys mount.SpawnKeys
		if err := json.Unmarshal(raw, &keys); err != nil {
			fmt.Fprintf(os.Stderr, "parse keys: %v\n", err)
			os.Exit(1)
		}

		pubBytes, err := hex.DecodeString(keys.PubHex)
		if err != nil || len(pubBytes) != 32 {
			os.Exit(1)
		}
		privBytes, err := hex.DecodeString(keys.PrivHex)
		if err != nil || len(privBytes) != 32 {
			os.Exit(1)
		}
		kyberPubBytes, err := hex.DecodeString(keys.KyberPubHex)
		if err != nil || len(kyberPubBytes) != crypto.KyberPublicKeySize {
			os.Exit(1)
		}
		kyberSeedBytes, err := hex.DecodeString(keys.KyberSeedHex)
		if err != nil || len(kyberSeedBytes) != crypto.KyberSeedSize {
			os.Exit(1)
		}
		nameBytes, err := hex.DecodeString(keys.NameKeyHex)
		if err != nil {
			os.Exit(1)
		}

		var x25519Pub, x25519Priv [32]byte
		copy(x25519Pub[:], pubBytes)
		copy(x25519Priv[:], privBytes)

		pub := &crypto.PublicKeySet{X25519: x25519Pub, Kyber: kyberPubBytes}
		priv := &crypto.PrivateKeySet{X25519: x25519Priv, Kyber: kyberSeedBytes}
		signPub, signPriv := decodeSigningKeys(keys)

		if cacheSizeBytes <= 0 {
			cacheSizeBytes = cache.DefaultMaxSize
		}
		if pollSec <= 0 {
			pollSec = 30
		}
		if mode == "" {
			mode = mount.ModeSync
		}

		cfg := &mount.DaemonConfig{
			MountPoint:        mountPoint,
			RemotePath:        remotePath,
			CacheSize:         cacheSizeBytes,
			PollInterval:      time.Duration(pollSec) * time.Second,
			PublicKey:         pub,
			PrivateKey:        priv,
			NameKey:           nameBytes,
			SigningPublicKey:  signPub,
			SigningPrivateKey: signPriv,
			Mode:              mode,
			ReadOnly:          readOnly,
		}

		var serveErr error
		if mode == mount.ModeVirtual {
			serveErr = mount.ServeDaemon(cfg)
		} else {
			serveErr = mount.ServeSyncDaemon(cfg)
		}

		if serveErr != nil {
			fmt.Fprintf(os.Stderr, "mount daemon error: %v\n", serveErr)
			os.Exit(1)
		}
	},
}

func decodeSigningKeys(keys mount.SpawnKeys) (*crypto.SigningPublicKeySet, *crypto.SigningPrivateKeySet) {
	pubEd, err := hex.DecodeString(keys.SignPubEdHex)
	if err != nil || len(pubEd) != crypto.Ed25519PKSize {
		return nil, nil
	}
	privEd, err := hex.DecodeString(keys.SignPrivEdHex)
	if err != nil || len(privEd) != crypto.Ed25519SKSize {
		return nil, nil
	}
	pubMl, err := hex.DecodeString(keys.SignPubMlHex)
	if err != nil || len(pubMl) != crypto.Mldsa44PKSize {
		return nil, nil
	}
	privMl, err := hex.DecodeString(keys.SignPrivMlHex)
	if err != nil || len(privMl) != crypto.Mldsa44SKSize {
		return nil, nil
	}
	var edPub [crypto.Ed25519PKSize]byte
	copy(edPub[:], pubEd)
	return &crypto.SigningPublicKeySet{Ed25519: edPub, Mldsa: pubMl},
		&crypto.SigningPrivateKeySet{Ed25519: ed25519.PrivateKey(privEd), Mldsa: privMl}
}

func init() {
	mountServeCmd.Flags().String("remote", "/", "")
	mountServeCmd.Flags().String("mountpoint", "", "")
	mountServeCmd.Flags().Int64("cache-size", cache.DefaultMaxSize, "")
	mountServeCmd.Flags().Int("poll", 30, "")
	mountServeCmd.Flags().String("mode", mount.ModeSync, "")
	mountServeCmd.Flags().Bool("read-only", false, "")
	rootCmd.AddCommand(mountServeCmd)
}
