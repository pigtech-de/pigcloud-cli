package sectest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (e env) publicLinkURL(token string) string {
	base := e.endpoint
	if idx := strings.LastIndex(base, "/cloud/"); idx >= 0 {
		base = base[:idx+len("/cloud/")]
	} else {
		base = strings.TrimSuffix(base, "/") + "/"
	}
	return base + "s/" + token
}

func (e env) fetchPublicLink(t *testing.T, token string) (status int, body []byte, elapsed time.Duration) {
	t.Helper()
	start := time.Now()
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.publicLinkURL(token), nil)
		if err != nil {
			cancel()
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("X-Cli-Client", "pigcloud-sectest")
		resp, err := e.client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		cancel()
		return resp.StatusCode, raw, time.Since(start)
	}
	t.Fatalf("fetch public link failed after %d attempts: %v", maxAttempts, lastErr)
	return 0, nil, 0
}

func randomHexToken(t *testing.T, bytesLen int) string {
	t.Helper()
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestPublicLink_NonexistentTokensReturn404(t *testing.T) {
	e := loadEnv(t)
	for i := 0; i < 5; i++ {
		token := randomHexToken(t, 16)
		status, _, _ := e.fetchPublicLink(t, token)
		if status != http.StatusNotFound {
			t.Errorf("token=%s status=%d want 404 (random 16-byte hex must not exist)", token, status)
		}
	}
}

func TestPublicLink_MalformedTokensReturn404(t *testing.T) {
	e := loadEnv(t)
	cases := []string{
		"!!!!!!!!!!!!!!!!",
		"GGGGGGGGGGGGGGGG",
		"short",
		"verylongtokenwellabovenormalsizebutshouldstillgracefullyreject" + strings.Repeat("a", 200),
	}
	for _, token := range cases {
		t.Run(token[:min(len(token), 20)], func(t *testing.T) {
			status, _, _ := e.fetchPublicLink(t, token)
			if status != http.StatusNotFound {
				t.Errorf("token=%q status=%d want 404", token, status)
			}
		})
	}
}

func TestPublicLink_ResponseSizeConstantAcrossNonexistent(t *testing.T) {
	e := loadEnv(t)
	const samples = 5
	sizes := make([]int, samples)
	for i := 0; i < samples; i++ {
		_, body, _ := e.fetchPublicLink(t, randomHexToken(t, 16))
		sizes[i] = len(body)
	}
	min, max := sizes[0], sizes[0]
	for _, s := range sizes[1:] {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	if max-min > 256 {
		t.Errorf("response sizes vary too much across nonexistent tokens (min=%d max=%d) — possible enumeration oracle", min, max)
	}
}

func TestPublicLink_NoBodyLeaksToken(t *testing.T) {
	e := loadEnv(t)
	token := randomHexToken(t, 16)
	_, body, _ := e.fetchPublicLink(t, token)
	if bytes.Contains(body, []byte(token)) {
		t.Errorf("404 body echoed token %s — reflected XSS / enumeration aid", token)
	}
}
