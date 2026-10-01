package e2ee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pigcloud/internal/api"
	"pigcloud/internal/config"
	"pigcloud/internal/fsutil"
)

const teeSigningPinVersion = 1

var ErrTeeAttestationUnavailable = errors.New("tee_signing_pk_unattested")

func IsTeeAttestationUnavailable(err error) bool {
	return errors.Is(err, ErrTeeAttestationUnavailable)
}

func teeSigningPksPath() string {
	dir := config.Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "tee_signing_pks.json")
}

type teeSigningPk struct {
	Ed string `json:"ed25519"`
	Ml string `json:"mldsa"`
}

type teeSigningPksFile struct {
	V int `json:"v"`
	Owners map[string]teeSigningPk `json:"owners"`
}

func loadTeeSigningPkFile() *teeSigningPksFile {
	empty := &teeSigningPksFile{V: teeSigningPinVersion, Owners: map[string]teeSigningPk{}}
	path := teeSigningPksPath()
	if path == "" {
		return empty
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var f teeSigningPksFile
	if json.Unmarshal(raw, &f) != nil || f.V != teeSigningPinVersion || f.Owners == nil {
		return empty
	}
	return &f
}

func pinnedTeeSigningPk() (teeSigningPk, bool) {
	owner := signingPinOwner()
	if owner == "" {
		return teeSigningPk{}, false
	}
	pin, ok := loadTeeSigningPkFile().Owners[owner]
	return pin, ok && pin.Ed != "" && pin.Ml != ""
}

func teeSigningPkPinned() bool {
	_, ok := pinnedTeeSigningPk()
	return ok
}

func TeeSigningPinned() bool { return teeSigningPkPinned() }

func recordTeeSigningPk(edB64, mlB64 string) {
	path := teeSigningPksPath()
	owner := signingPinOwner()
	if path == "" || owner == "" || edB64 == "" || mlB64 == "" {
		return
	}
	f := loadTeeSigningPkFile()
	f.Owners[owner] = teeSigningPk{Ed: edB64, Ml: mlB64}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = fsutil.WriteFileAtomic(path, data, 0600)
}

var (
	teeSigningAttMu       sync.Mutex
	teeSigningAtt         *teeSigningPk
	teeSigningAttNextTry  time.Time
	teeSigningAttThrottle = time.Minute
)

func attestedTeeSigningPk() *teeSigningPk {
	teeSigningAttMu.Lock()
	defer teeSigningAttMu.Unlock()
	if teeSigningAtt != nil {
		return teeSigningAtt
	}
	if time.Now().Before(teeSigningAttNextTry) {
		return nil
	}
	teeSigningAttNextTry = time.Now().Add(teeSigningAttThrottle)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := api.NewClient().FetchTeeAttestation(ctx)
	if err != nil || resp == nil || !resp.Success || !resp.Enabled || !resp.Available {
		return nil
	}
	att := resp.Attestation
	if att.VerificationStatus == "untrusted" {
		return nil
	}
	if att.AttestationMode == "epid" && att.Mrenclave != "" && att.SgxQuote != "" && att.VerificationStatus != "trusted" {
		return nil
	}
	if att.EnclaveSigningPkEd25519 == "" || att.EnclaveSigningPkMldsa == "" {
		return nil
	}
	teeSigningAtt = &teeSigningPk{Ed: att.EnclaveSigningPkEd25519, Ml: att.EnclaveSigningPkMldsa}
	return teeSigningAtt
}

func checkTeeSigningPks(edB64, mlB64 string) (func(), error) {
	if pin, ok := pinnedTeeSigningPk(); ok {
		if pin.Ed != edB64 || pin.Ml != mlB64 {
			return nil, fmt.Errorf("tee_signing_pk_changed: the enclave signing key differs from the one pinned for this account. If the enclave was deliberately rekeyed, deleting %s accepts the next key it is offered, which is also what an attacker needs", teeSigningPksPath())
		}
		return func() {}, nil
	}
	att := attestedTeeSigningPk()
	if att == nil {
		return nil, ErrTeeAttestationUnavailable
	}
	if att.Ed != edB64 || att.Ml != mlB64 {
		return nil, errors.New("tee_signing_pk_untrusted: the served enclave signing key is not the attested one")
	}
	return func() { recordTeeSigningPk(edB64, mlB64) }, nil
}
