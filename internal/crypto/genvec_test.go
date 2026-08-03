package crypto

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestGenerateHybridSealVector(t *testing.T) {
	if os.Getenv("PIGCLOUD_GEN_VECTORS") != "1" {
		t.Skip("vector generation; set PIGCLOUD_GEN_VECTORS=1 to regenerate the fixture")
	}

	pub, priv, err := GenerateHybridKeyPair()
	if err != nil {
		t.Fatalf("GenerateHybridKeyPair: %v", err)
	}
	defer priv.Zero()

	plaintext := []byte("pigcloud-hybrid-seal-v2 | conformance vector #1 | UTF-8: pörk pîë \xf0\x9f\x90\xb7")

	sealed, err := HybridSeal(plaintext, pub)
	if err != nil {
		t.Fatalf("HybridSeal: %v", err)
	}
	if len(sealed) < 1144+16 {
		t.Fatalf("sealed blob too short: %d", len(sealed))
	}

	v := vectorFile{
		VectorKind:  "hybrid_seal_v1",
		SpecVersion: 2,
		KDFInfo:     HybridKDFInfo,
		Description: "Hybrid X25519+ML-KEM-768 sealed blob. Each conforming impl must HybridUnseal(sealed_blob_b64, recipient.sk_b64+seed_b64) and recover plaintext_b64 byte-for-byte.",
		Generator: generatorMeta{
			Impl:        "go",
			Package:     "pigcloud/internal/crypto",
			Function:    "HybridSeal",
			Tool:        "cli/internal/crypto/genvec_test.go (PIGCLOUD_GEN_VECTORS=1 go test -run TestGenerateHybridSealVector)",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Recipient: recipientKeys{
			X25519PkB64:  base64.StdEncoding.EncodeToString(pub.X25519[:]),
			X25519SkB64:  base64.StdEncoding.EncodeToString(priv.X25519[:]),
			MLKemPkB64:   base64.StdEncoding.EncodeToString(pub.Kyber),
			MLKemSeedB64: base64.StdEncoding.EncodeToString(priv.Kyber),
		},
		PlaintextB64:  base64.StdEncoding.EncodeToString(plaintext),
		SealedBlobB64: base64.StdEncoding.EncodeToString(sealed),
		Layout: hybridLayout{
			HeaderSize:        1144,
			EphX25519PKOffset: 0,
			EphX25519PKSize:   32,
			MLKemCTOffset:     32,
			MLKemCTSize:       1088,
			NonceOffset:       1120,
			NonceSize:         24,
			AEADCTOffset:      1144,
		},
	}

	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body = append(body, '\n')

	dst := vectorPath(t, "hybrid_seal_v1.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
	t.Logf("wrote %s (%d bytes)", dst, len(body))
}

func TestGenerateChatMessageVector(t *testing.T) {
	if os.Getenv("PIGCLOUD_GEN_VECTORS") != "1" {
		t.Skip("vector generation; set PIGCLOUD_GEN_VECTORS=1 to regenerate the fixture")
	}

	dataKey := make([]byte, KeySize)
	for i := range dataKey {
		dataKey[i] = byte(i)
	}
	nonce := make([]byte, NonceSize)
	for i := range nonce {
		nonce[i] = byte(0x40 + i)
	}
	plaintext := []byte("pigcloud chat body | web<->CLI XChaCha20-Poly1305 IETF | UTF-8: pörk pîë \xf0\x9f\x90\xb7")

	aead, err := chacha20poly1305.NewX(dataKey)
	if err != nil {
		t.Fatalf("NewX: %v", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	v := chatMessageVector{
		VectorKind:  "chat_message_v1",
		SpecVersion: 1,
		Description: "Chat message body cipher (XChaCha20-Poly1305 IETF). Each impl must DecryptMessage(ciphertext_b64, nonce_b64, data_key_b64) and recover plaintext_b64. Guards the CLI<->web chat split (was XSalsa20 secretbox on the CLI).",
		Generator: generatorMeta{
			Impl:        "go",
			Package:     "pigcloud/internal/crypto",
			Function:    "EncryptMessage",
			Tool:        "cli/internal/crypto/genvec_test.go (PIGCLOUD_GEN_VECTORS=1 go test -run TestGenerateChatMessageVector)",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		DataKeyB64:    base64.StdEncoding.EncodeToString(dataKey),
		NonceB64:      base64.StdEncoding.EncodeToString(nonce),
		PlaintextB64:  base64.StdEncoding.EncodeToString(plaintext),
		CiphertextB64: base64.StdEncoding.EncodeToString(ciphertext),
	}

	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body = append(body, '\n')

	dst := vectorPath(t, "chat_message_v1.json")
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
	t.Logf("wrote %s (%d bytes)", dst, len(body))
}
