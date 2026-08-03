package crypto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type vectorFile struct {
	VectorKind    string        `json:"vector_kind"`
	SpecVersion   int           `json:"spec_version"`
	KDFInfo       string        `json:"kdf_info"`
	Description   string        `json:"description"`
	Generator     generatorMeta `json:"generator"`
	Recipient     recipientKeys `json:"recipient"`
	PlaintextB64  string        `json:"plaintext_b64"`
	SealedBlobB64 string        `json:"sealed_blob_b64"`
	Layout        hybridLayout  `json:"layout"`
}

type generatorMeta struct {
	Impl        string `json:"impl"`
	Package     string `json:"package"`
	Function    string `json:"function"`
	Tool        string `json:"tool"`
	GeneratedAt string `json:"generated_at"`
}

type recipientKeys struct {
	X25519PkB64  string `json:"x25519_pk_b64"`
	X25519SkB64  string `json:"x25519_sk_b64"`
	MLKemPkB64   string `json:"mlkem_pk_b64"`
	MLKemSeedB64 string `json:"mlkem_seed_b64"`
}

type hybridLayout struct {
	HeaderSize        int `json:"header_size"`
	EphX25519PKOffset int `json:"ephemeral_x25519_pk_offset"`
	EphX25519PKSize   int `json:"ephemeral_x25519_pk_size"`
	MLKemCTOffset     int `json:"mlkem_ciphertext_offset"`
	MLKemCTSize       int `json:"mlkem_ciphertext_size"`
	NonceOffset       int `json:"xchacha20_nonce_offset"`
	NonceSize         int `json:"xchacha20_nonce_size"`
	AEADCTOffset      int `json:"aead_ciphertext_offset"`
}

func TestConformance_HybridSealV1(t *testing.T) {
	path := vectorPath(t, "hybrid_seal_v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v\nrun PIGCLOUD_GEN_VECTORS=1 go test -run TestGenerateHybridSealVector to create it", path, err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if v.VectorKind != "hybrid_seal_v1" {
		t.Fatalf("unexpected vector_kind=%q", v.VectorKind)
	}
	if v.KDFInfo != HybridKDFInfo {
		t.Fatalf("kdf_info mismatch: fixture=%q this impl=%q", v.KDFInfo, HybridKDFInfo)
	}

	priv := &PrivateKeySet{
		X25519: decode32(t, "x25519_sk", v.Recipient.X25519SkB64),
		Kyber:  decodeB64(t, "mlkem_seed", v.Recipient.MLKemSeedB64),
	}
	defer priv.Zero()

	sealed := decodeB64(t, "sealed_blob", v.SealedBlobB64)
	wantPlaintext := decodeB64(t, "plaintext", v.PlaintextB64)

	gotPlaintext, err := HybridUnseal(sealed, priv)
	if err != nil {
		t.Fatalf("HybridUnseal failed — impl diverges from fixture: %v", err)
	}
	if !bytes.Equal(gotPlaintext, wantPlaintext) {
		t.Fatalf("plaintext mismatch — impl unseals but produces different bytes\n  got:  %x\n  want: %x", gotPlaintext, wantPlaintext)
	}
}

type chatMessageVector struct {
	VectorKind    string        `json:"vector_kind"`
	SpecVersion   int           `json:"spec_version"`
	Description   string        `json:"description"`
	Generator     generatorMeta `json:"generator"`
	DataKeyB64    string        `json:"data_key_b64"`
	NonceB64      string        `json:"nonce_b64"`
	PlaintextB64  string        `json:"plaintext_b64"`
	CiphertextB64 string        `json:"ciphertext_b64"`
}

func TestConformance_ChatMessageV1(t *testing.T) {
	path := vectorPath(t, "chat_message_v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v\nrun PIGCLOUD_GEN_VECTORS=1 go test -run TestGenerateChatMessageVector to create it", path, err)
	}
	var v chatMessageVector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if v.VectorKind != "chat_message_v1" {
		t.Fatalf("unexpected vector_kind=%q", v.VectorKind)
	}

	dataKey := decodeB64(t, "data_key", v.DataKeyB64)
	nonce := decodeB64(t, "nonce", v.NonceB64)
	ciphertext := decodeB64(t, "ciphertext", v.CiphertextB64)
	wantPlaintext := decodeB64(t, "plaintext", v.PlaintextB64)

	gotPlaintext, err := DecryptMessage(ciphertext, nonce, dataKey)
	if err != nil {
		t.Fatalf("DecryptMessage failed — chat body cipher diverges from fixture: %v", err)
	}
	if !bytes.Equal(gotPlaintext, wantPlaintext) {
		t.Fatalf("plaintext mismatch — decrypts but produces different bytes\n  got:  %x\n  want: %x", gotPlaintext, wantPlaintext)
	}
}

func vectorPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "vectors", name)
}

func decodeB64(t *testing.T, label, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode %s: %v", label, err)
	}
	return b
}

func decode32(t *testing.T, label, s string) [32]byte {
	t.Helper()
	b := decodeB64(t, label, s)
	if len(b) != 32 {
		t.Fatalf("%s: want 32 bytes, got %d", label, len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out
}
