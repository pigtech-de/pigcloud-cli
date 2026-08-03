package sectest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	envEndpoint  = "PIGCLOUD_SECTEST_ENDPOINT"
	envKeyA      = "PIGCLOUD_SECTEST_KEY_A"
	envKeyB      = "PIGCLOUD_SECTEST_KEY_B"
	envNodeB     = "PIGCLOUD_SECTEST_NODE_B"
	envAllowProd = "PIGCLOUD_SECTEST_ALLOW_PROD"
	envCAFile    = "PIGCLOUD_SECTEST_CA_FILE"
)

const requestTimeout = 15 * time.Second

type env struct {
	endpoint  string
	keyA      string
	keyB      string
	nodeB     string
	allowProd bool
	client    *http.Client
}

func loadEnv(t *testing.T) env {
	t.Helper()
	endpoint := strings.TrimSpace(os.Getenv(envEndpoint))
	if endpoint == "" {
		t.Skipf("%s not set — skipping live security tests", envEndpoint)
	}
	return env{
		endpoint:  endpoint,
		keyA:      strings.TrimSpace(os.Getenv(envKeyA)),
		keyB:      strings.TrimSpace(os.Getenv(envKeyB)),
		nodeB:     strings.TrimSpace(os.Getenv(envNodeB)),
		allowProd: os.Getenv(envAllowProd) == "1",
		client:    buildClient(t),
	}
}

func buildClient(t *testing.T) *http.Client {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if caPath := strings.TrimSpace(os.Getenv(envCAFile)); caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			t.Fatalf("read %s=%q: %v", envCAFile, caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			t.Fatalf("%s=%q: no certificates parsed", envCAFile, caPath)
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	return &http.Client{Timeout: requestTimeout, Transport: tr}
}

func (e env) requireKeyA(t *testing.T) {
	t.Helper()
	if e.keyA == "" {
		t.Skipf("%s not set — skipping authenticated test", envKeyA)
	}
}

func (e env) isLocal() bool {
	u, err := url.Parse(e.endpoint)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost")
}

func (e env) requireSafeForLoad(t *testing.T) {
	t.Helper()
	if !e.isLocal() && !e.allowProd {
		t.Skipf("endpoint %q is not local and %s != 1 — refusing to consume rate-limit buckets", e.endpoint, envAllowProd)
	}
}

type request struct {
	method      string
	action      string
	apiKey      string
	contentType string
	body        []byte
	headers     map[string]string
}

type result struct {
	status     int
	retryAfter string
	body       []byte
	success    *bool
	messageKey string
}

func (e env) do(t *testing.T, r request) result {
	t.Helper()

	target := e.endpoint
	if r.action != "" {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + "action=" + url.QueryEscape(r.action)
	}

	method := r.method
	if method == "" {
		method = http.MethodGet
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			cancel()
			t.Fatalf("build request (%s %s): %v", method, r.action, err)
		}
		if r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		if r.apiKey != "" {
			req.Header.Set("X-Api-Key", r.apiKey)
		}
		req.Header.Set("X-Cli-Client", "pigcloud-sectest")
		for k, v := range r.headers {
			req.Header.Set(k, v)
		}

		resp, err := e.client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		cancel()

		out := result{
			status:     resp.StatusCode,
			retryAfter: resp.Header.Get("Retry-After"),
			body:       raw,
		}
		var parsed struct {
			Success    *bool  `json:"success"`
			MessageKey string `json:"messageKey"`
		}
		if json.Unmarshal(raw, &parsed) == nil {
			out.success = parsed.Success
			out.messageKey = parsed.MessageKey
		}
		return out
	}

	t.Fatalf("send request (%s %s) failed after %d attempts: %v", method, r.action, maxAttempts, lastErr)
	return result{}
}

func jsonBody(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return b
}

func cliBody(t *testing.T, command string, options map[string]string) []byte {
	t.Helper()
	if options == nil {
		options = map[string]string{}
	}
	return jsonBody(t, map[string]any{"command": command, "options": options})
}

func (r result) requireStatus(t *testing.T, want int) {
	t.Helper()
	if r.status != want {
		t.Errorf("status = %d, want %d (messageKey=%q body=%s)", r.status, want, r.messageKey, snippet(r.body))
	}
}

func (r result) requireStatusIn(t *testing.T, want ...int) {
	t.Helper()
	for _, w := range want {
		if r.status == w {
			return
		}
	}
	t.Errorf("status = %d, want one of %v (messageKey=%q body=%s)", r.status, want, r.messageKey, snippet(r.body))
}

func (r result) requireNoLeak(t *testing.T) {
	t.Helper()
	if r.status == http.StatusOK && (r.success == nil || *r.success) {
		t.Errorf("expected rejection, got 200 without success=false (messageKey=%q body=%s)", r.messageKey, snippet(r.body))
	}
}

func snippet(b []byte) string {
	const max = 240
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
