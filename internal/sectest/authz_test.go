package sectest

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuthz_PositiveControl_KeyAAuthenticates(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "cli",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        cliBody(t, "wh", nil),
	})
	res.requireStatus(t, http.StatusOK)
	if res.success == nil || !*res.success {
		t.Fatalf("key A did not authenticate (status=%d body=%s) — fix %s before trusting the authz suite", res.status, snippet(res.body), envKeyA)
	}
}

func TestAuthz_ForeignNodeNoRecipientLeak(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	if e.nodeB == "" {
		t.Skipf("%s not set — skipping cross-user IDOR probe", envNodeB)
	}
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "share-recipients-for-node",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]string{"nodeId": e.nodeB}),
	})
	res.requireStatus(t, http.StatusOK)

	var parsed struct {
		Success    bool `json:"success"`
		Recipients []struct {
			Username string `json:"username"`
		} `json:"recipients"`
	}
	if err := json.Unmarshal(res.body, &parsed); err != nil {
		t.Fatalf("unparseable response: %s", snippet(res.body))
	}
	if len(parsed.Recipients) != 0 {
		t.Errorf("IDOR: key A read %d recipient(s) for foreign node %s — ownership filter leaked", len(parsed.Recipients), e.nodeB)
	}
}

func TestAuthz_NonexistentNodeNoData(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "share-recipients-for-node",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]string{"nodeId": "ffffffffffffffffffffffffffffffff"}),
	})
	res.requireStatusIn(t, http.StatusOK, http.StatusBadRequest, http.StatusNotFound)
	if res.status != http.StatusOK {
		return
	}
	var parsed struct {
		Recipients []json.RawMessage `json:"recipients"`
	}
	if json.Unmarshal(res.body, &parsed) == nil && len(parsed.Recipients) != 0 {
		t.Errorf("nonexistent node returned %d recipient(s)", len(parsed.Recipients))
	}
}

func TestAuthz_CannotRevokeForeignSession(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	body := jsonBody(t, map[string]string{
		"scope":     "single",
		"sessionId": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	})
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "account-revoke-session",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        body,
	})
	res.requireStatusIn(t, http.StatusOK, http.StatusBadRequest, http.StatusNotFound)
	if res.status == http.StatusOK && res.success != nil && *res.success {
		t.Errorf("account-revoke-session reported success for a foreign/nonexistent session id — ownership filter leaked")
	}
}
