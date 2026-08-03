package sectest

import (
	"net/http"
	"testing"
)

func TestRateLimit_SrpInitBucket(t *testing.T) {
	e := loadEnv(t)
	e.requireSafeForLoad(t)

	const bucketLimit = 30
	const attempts = bucketLimit + 10

	var got429 bool
	var retryAfter string
	for i := 0; i < attempts; i++ {
		res := e.do(t, request{method: http.MethodGet, action: "auth-csrf"})
		if res.status == http.StatusTooManyRequests {
			got429 = true
			retryAfter = res.retryAfter
			t.Logf("429 after %d requests (limit=%d)", i+1, bucketLimit)
			break
		}
		if res.status != http.StatusOK {
			t.Fatalf("attempt %d: status %d, want 200 or 429 (body=%s)", i+1, res.status, snippet(res.body))
		}
	}

	if !got429 {
		t.Errorf("no 429 after %d requests to auth-csrf — srp_init limit (%d) not enforced", attempts, bucketLimit)
	}
	if got429 && retryAfter == "" {
		t.Errorf("429 returned without Retry-After header")
	}
}

func TestRateLimit_SensitiveAccountBucket(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	e.requireSafeForLoad(t)

	const bucketLimit = 5
	const attempts = bucketLimit + 5

	var got429 bool
	var retryAfter string
	for i := 0; i < attempts; i++ {
		res := e.do(t, request{
			method:      http.MethodPost,
			action:      "account-srp-init",
			apiKey:      e.keyA,
			contentType: "application/json",
			body:        jsonBody(t, map[string]string{"context": "sudo"}),
		})
		if res.status == http.StatusTooManyRequests {
			got429 = true
			retryAfter = res.retryAfter
			t.Logf("429 after %d requests (limit=%d)", i+1, bucketLimit)
			break
		}
		res.requireStatusIn(t, http.StatusOK, http.StatusBadRequest)
	}

	if !got429 {
		t.Errorf("no 429 after %d requests to account-srp-init — sensitive_account limit (%d) not enforced", attempts, bucketLimit)
	}
	if got429 && retryAfter == "" {
		t.Errorf("429 returned without Retry-After header")
	}
}

func TestRateLimit_ApiKeyAttemptBucket(t *testing.T) {
	e := loadEnv(t)
	e.requireSafeForLoad(t)

	const bucketLimit = 10
	const attempts = bucketLimit + 5

	var got429 bool
	for i := 0; i < attempts; i++ {
		res := e.do(t, request{
			method:      http.MethodPost,
			action:      "cli",
			apiKey:      "BADKEYIDEN.deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			contentType: "application/json",
			body:        cliBody(t, "wh", nil),
		})
		if res.status == http.StatusTooManyRequests {
			got429 = true
			t.Logf("429 after %d bad-key attempts (limit=%d)", i+1, bucketLimit)
			break
		}
		res.requireStatusIn(t, http.StatusUnauthorized, http.StatusForbidden)
	}
	if !got429 {
		t.Errorf("no 429 after %d bad-key requests — api_key_attempt bucket (%d) not enforced; lockout missing", attempts, bucketLimit)
	}
}
