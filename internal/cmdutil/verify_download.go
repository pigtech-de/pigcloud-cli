package cmdutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	"pigcloud/internal/api"
	"pigcloud/internal/crypto"
)

func VerifyDownloadIntegrity(r io.Reader, dlResult *api.DownloadResult) error {
	return verifyDownloadIntegrity(r, dlResult, resolveOwnSigningEdPubInteractive())
}

func VerifyDownloadIntegrityWithSigningKey(r io.Reader, dlResult *api.DownloadResult, signingPriv *crypto.SigningPrivateKeySet) error {
	return verifyDownloadIntegrity(r, dlResult, deriveSigningEdPub(signingPriv))
}

func verifyDownloadIntegrity(r io.Reader, dlResult *api.DownloadResult, ownEdPub []byte) error {
	if dlResult == nil {
		return fmt.Errorf("download_result_missing")
	}
	if dlResult.TEESignatureEd25519 != "" || dlResult.TEESignatureMldsa != "" {
		return verifyTEESigsReader(r, dlResult)
	}
	if dlResult.SignatureEd25519 != "" || dlResult.SignatureMldsa != "" {
		return verifyOwnerSigsReader(r, dlResult, ownEdPub)
	}
	return fmt.Errorf("file_signature_missing")
}

func verifyOwnerSigsReader(r io.Reader, dlResult *api.DownloadResult, ownEdPub []byte) error {
	sigEd, err := decodeB64Required(dlResult.SignatureEd25519, "signature_ed25519")
	if err != nil {
		return err
	}
	sigMl, err := decodeB64Required(dlResult.SignatureMldsa, "signature_mldsa")
	if err != nil {
		return err
	}
	pkEd, err := decodeB64Required(dlResult.SigningPkEd25519, "signing_pk_ed25519")
	if err != nil {
		return err
	}
	pkMl, err := decodeB64Required(dlResult.SigningPkMldsa, "signing_pk_mldsa")
	if err != nil {
		return err
	}
	if len(pkEd) != crypto.Ed25519PKSize || len(pkMl) != crypto.Mldsa44PKSize {
		return fmt.Errorf("signing_public_key_wrong_size")
	}
	if len(ownEdPub) == crypto.Ed25519PKSize {
		rememberSigningEdPub(ownEdPub)
		if !bytes.Equal(ownEdPub, pkEd) && !signingEdPubTrusted(pkEd) {
			seedOwnSigningPkHistory(ownEdPub)
			if !signingEdPubTrusted(pkEd) {
				return fmt.Errorf("owner_signing_pk_untrusted")
			}
		}
	}
	var edPub [crypto.Ed25519PKSize]byte
	copy(edPub[:], pkEd)
	pub := &crypto.SigningPublicKeySet{Ed25519: edPub, Mldsa: pkMl}
	return crypto.VerifyFileSignatures(r, sigEd, sigMl, pub)
}

func verifyTEESigsReader(r io.Reader, dlResult *api.DownloadResult) error {
	sigEd, err := decodeB64Required(dlResult.TEESignatureEd25519, "tee_signature_ed25519")
	if err != nil {
		return err
	}
	sigMl, err := decodeB64Required(dlResult.TEESignatureMldsa, "tee_signature_mldsa")
	if err != nil {
		return err
	}
	pkEd, err := decodeB64Required(dlResult.TEESigningPkEd25519, "tee_signing_pk_ed25519")
	if err != nil {
		return err
	}
	pkMl, err := decodeB64Required(dlResult.TEESigningPkMldsa, "tee_signing_pk_mldsa")
	if err != nil {
		return err
	}
	if len(pkEd) != crypto.Ed25519PKSize || len(pkMl) != crypto.Mldsa44PKSize {
		return fmt.Errorf("tee_signing_public_key_wrong_size")
	}
	var edPub [crypto.Ed25519PKSize]byte
	copy(edPub[:], pkEd)
	pub := &crypto.SigningPublicKeySet{Ed25519: edPub, Mldsa: pkMl}
	return crypto.VerifyTEEFileSignatures(r, sigEd, sigMl, pub)
}

func decodeB64Required(s, label string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("%s_missing", label)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s_invalid: %w", label, err)
	}
	return b, nil
}
