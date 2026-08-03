package sectest

import (
	"net/http"
	"testing"
)

func TestAuthGate_MethodEnforcement(t *testing.T) {
	e := loadEnv(t)
	cases := []struct {
		name   string
		action string
	}{
		{"cli", "cli"},
		{"update-preferences", "update-preferences"},
		{"account-profile", "account-profile"},
		{"account-srp-init", "account-srp-init"},
		{"login-srp-init", "login-srp-init"},
		{"login-srp-verify", "login-srp-verify"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := e.do(t, request{method: http.MethodGet, action: c.action})
			res.requireStatus(t, http.StatusMethodNotAllowed)
		})
	}
}

func TestAuthGate_UnauthenticatedRejected(t *testing.T) {
	e := loadEnv(t)
	cases := []struct {
		name   string
		action string
		body   []byte
	}{
		{"cli", "cli", cliBody(t, "wh", nil)},
		{"account-profile", "account-profile", jsonBody(t, map[string]any{})},
		{"account-sessions", "account-sessions", jsonBody(t, map[string]any{})},
		{"update-preferences", "update-preferences", jsonBody(t, map[string]any{"theme": "dark"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := e.do(t, request{
				method:      http.MethodPost,
				action:      c.action,
				contentType: "application/json",
				body:        c.body,
			})
			res.requireNoLeak(t)
			res.requireStatusIn(t, http.StatusUnauthorized, http.StatusForbidden)
		})
	}
}

func TestAuthGate_BadApiKeyRejected(t *testing.T) {
	e := loadEnv(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "cli",
		apiKey:      "pc_not_a_real_key_000000000000000000",
		contentType: "application/json",
		body:        cliBody(t, "wh", nil),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusUnauthorized, http.StatusForbidden)
}

func TestAuthGate_TeeAttestationRequiresAuth(t *testing.T) {
	e := loadEnv(t)
	res := e.do(t, request{method: http.MethodGet, action: "tee-attestation"})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusUnauthorized, http.StatusForbidden)
}

func TestAuthGate_NonPostMethodsRejectedOnCli(t *testing.T) {
	e := loadEnv(t)
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			res := e.do(t, request{method: method, action: "cli"})
			res.requireStatus(t, http.StatusMethodNotAllowed)
		})
	}
}

func TestAuthGate_MalformedJsonBodyRejected(t *testing.T) {
	e := loadEnv(t)
	e.requireKeyA(t)
	res := e.do(t, request{
		method:      http.MethodPost,
		action:      "cli",
		apiKey:      e.keyA,
		contentType: "application/json",
		body:        []byte("not-json-at-all"),
	})
	res.requireNoLeak(t)
	res.requireStatusIn(t, http.StatusBadRequest)
}
