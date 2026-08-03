package sectest

import (
	"net/http"
	"testing"
)

func TestSudo_SetPrimary2faRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "account-set-primary-2fa",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{"method": "totp"}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized)
	if res.messageKey != "" && res.messageKey != "sudoRequired" && res.messageKey != "accountUpdateErrorUnauthorized" {
		t.Errorf("unexpected messageKey=%q for sensitive endpoint without sudo (want sudoRequired)", res.messageKey)
	}
}

func TestSudo_PasskeyRenameRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "passkeys-rename",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{"passkeyId": 999999, "name": "sectest"}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized)
	if res.messageKey != "" && res.messageKey != "sudoRequired" && res.messageKey != "accountUpdateErrorUnauthorized" {
		t.Errorf("unexpected messageKey=%q for passkeys-rename without sudo", res.messageKey)
	}
}

func TestSudo_PasskeySetFlagsRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "passkeys-set-flags",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{"passkeyId": 999999, "useForSignin": true, "useForTwoFa": false}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized)
	if res.messageKey != "" && res.messageKey != "sudoRequired" && res.messageKey != "accountUpdateErrorUnauthorized" {
		t.Errorf("unexpected messageKey=%q for passkeys-set-flags without sudo", res.messageKey)
	}
}

func TestSudo_PasskeyRemoveRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "passkeys-remove",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{"passkeyId": 999999}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized)
	if res.messageKey != "" && res.messageKey != "sudoRequired" && res.messageKey != "accountUpdateErrorUnauthorized" {
		t.Errorf("unexpected messageKey=%q for passkeys-remove without sudo", res.messageKey)
	}
}

func TestSudo_AccountUpdateEmailRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "account-update",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{"emailUpdate": map[string]any{"email": "sectest@example.com"}}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized, http.StatusBadRequest)
	if res.success != nil && *res.success {
		t.Errorf("account-update returned success without sudo — sensitive-action gate bypassed")
	}
}

func TestSudo_AccountUpdatePasswordRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "account-update",
		apiKey:      e.keyA,
		contentType: "application/json",
		body: jsonBody(t, map[string]any{
			"passwordUpdate": map[string]any{
				"srp_salt":     "00",
				"srp_verifier": "00",
			},
		}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized, http.StatusBadRequest)
	if res.success != nil && *res.success {
		t.Errorf("account-update password change reported success without sudo — sensitive-action gate bypassed")
	}
}

func TestSudo_CreateApiKeyRejectedWithoutSudo(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "account-api-create",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        jsonBody(t, map[string]any{}),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusForbidden, http.StatusUnauthorized, http.StatusBadRequest, http.StatusNotFound)
	if res.success != nil && *res.success {
		t.Errorf("API key created without sudo — sensitive-action gate bypassed")
	}
}
