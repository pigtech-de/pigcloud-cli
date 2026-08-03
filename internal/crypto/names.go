package crypto

import (
	"strings"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/text/unicode/norm"
)

const nameKeyContext = "pigcloud-e2ee-name-key-v2"

func DeriveNameKey(priv *PrivateKeySet) ([]byte, error) {
	h, err := blake2b.New256([]byte(nameKeyContext))
	if err != nil {
		return nil, err
	}
	h.Write(priv.X25519[:])
	h.Write(priv.Kyber)
	return h.Sum(nil), nil
}

func ComputePathToken(nameKey []byte, canonicalPath string) ([]byte, error) {
	normalized := normalizeLower(canonicalPath)
	h, err := blake2b.New256(nameKey)
	if err != nil {
		return nil, err
	}
	h.Write([]byte(normalized))
	return h.Sum(nil), nil
}

func SealDisplayName(name string, recipient *PublicKeySet) ([]byte, error) {
	return HybridSeal([]byte(name), recipient)
}

func UnsealDisplayName(sealed []byte, priv *PrivateKeySet) (string, error) {
	plaintext, err := HybridUnseal(sealed, priv)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func normalizeLower(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = norm.NFC.String(path)
	path = strings.ToLower(path)
	return path
}
