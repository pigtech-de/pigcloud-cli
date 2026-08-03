package cmd

import (
	"encoding/hex"
	"os"
	"time"

	"github.com/spf13/cobra"
	"pigcloud/internal/agent"
	"pigcloud/internal/crypto"
)

var agentServeCmd = &cobra.Command{
	Use:    "__agent-serve",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		pubHex, _ := cmd.Flags().GetString("pub")
		privHex, _ := cmd.Flags().GetString("priv")
		kyberPubHex, _ := cmd.Flags().GetString("kyber-pub")
		kyberSeedHex, _ := cmd.Flags().GetString("kyber-seed")
		nameHex, _ := cmd.Flags().GetString("name-key")
		signPubEdHex, _ := cmd.Flags().GetString("sign-pub-ed")
		signPrivEdHex, _ := cmd.Flags().GetString("sign-priv-ed")
		signPubMlHex, _ := cmd.Flags().GetString("sign-pub-ml")
		signPrivMlHex, _ := cmd.Flags().GetString("sign-priv-ml")
		ttlSec, _ := cmd.Flags().GetInt("ttl")

		pubBytes, err := hex.DecodeString(pubHex)
		if err != nil || len(pubBytes) != 32 {
			os.Exit(1)
		}
		privBytes, err := hex.DecodeString(privHex)
		if err != nil || len(privBytes) != 32 {
			os.Exit(1)
		}
		kyberPubBytes, err := hex.DecodeString(kyberPubHex)
		if err != nil || len(kyberPubBytes) != crypto.KyberPublicKeySize {
			os.Exit(1)
		}
		kyberSeedBytes, err := hex.DecodeString(kyberSeedHex)
		if err != nil || len(kyberSeedBytes) != crypto.KyberSeedSize {
			os.Exit(1)
		}
		nameBytes, err := hex.DecodeString(nameHex)
		if err != nil {
			os.Exit(1)
		}

		signPubEd := decodeOptional(signPubEdHex, crypto.Ed25519PKSize)
		signPrivEd := decodeOptional(signPrivEdHex, crypto.Ed25519SKSize)
		signPubMl := decodeOptional(signPubMlHex, crypto.Mldsa44PKSize)
		signPrivMl := decodeOptional(signPrivMlHex, crypto.Mldsa44SKSize)

		var keys agent.KeyMaterial
		copy(keys.PublicKey[:], pubBytes)
		copy(keys.PrivateKey[:], privBytes)
		keys.KyberPublicKey = kyberPubBytes
		keys.KyberSeed = kyberSeedBytes
		keys.NameKey = nameBytes
		keys.SigningPublicKeyEd25519 = signPubEd
		keys.SigningPrivateKeyEd25519 = signPrivEd
		keys.SigningPublicKeyMldsa = signPubMl
		keys.SigningPrivateKeyMldsa = signPrivMl

		for i := range pubHex {
			pubHex = pubHex[:i] + "0" + pubHex[i+1:]
		}
		for i := range privHex {
			privHex = privHex[:i] + "0" + privHex[i+1:]
		}
		for i := range kyberSeedHex {
			kyberSeedHex = kyberSeedHex[:i] + "0" + kyberSeedHex[i+1:]
		}
		for i := range signPrivEdHex {
			signPrivEdHex = signPrivEdHex[:i] + "0" + signPrivEdHex[i+1:]
		}
		for i := range signPrivMlHex {
			signPrivMlHex = signPrivMlHex[:i] + "0" + signPrivMlHex[i+1:]
		}

		ttl := time.Duration(ttlSec) * time.Second
		if ttl <= 0 {
			ttl = time.Hour
		}

		agent.Serve(&keys, ttl)
	},
}

func decodeOptional(hexStr string, expectedLen int) []byte {
	if hexStr == "" {
		return nil
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != expectedLen {
		return nil
	}
	return b
}

func init() {
	agentServeCmd.Flags().String("pub", "", "")
	agentServeCmd.Flags().String("priv", "", "")
	agentServeCmd.Flags().String("kyber-pub", "", "")
	agentServeCmd.Flags().String("kyber-seed", "", "")
	agentServeCmd.Flags().String("name-key", "", "")
	agentServeCmd.Flags().String("sign-pub-ed", "", "")
	agentServeCmd.Flags().String("sign-priv-ed", "", "")
	agentServeCmd.Flags().String("sign-pub-ml", "", "")
	agentServeCmd.Flags().String("sign-priv-ml", "", "")
	agentServeCmd.Flags().Int("ttl", 3600, "")
	rootCmd.AddCommand(agentServeCmd)
}
