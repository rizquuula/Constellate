package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"
)

// TestHandlerRateLimit_TOTP_429AfterBurst exercises the per-IP rate limiter via
// repeated POST /api/auth/totp requests. After loginIPMax (5) attempts the next
// call must return 429 with a Retry-After header.
func TestHandlerRateLimit_TOTP_429AfterBurst(t *testing.T) {
	ts, _ := buildMiddlewareTestServer(t)
	defer ts.Close()

	body := []byte(`{"code":"999999"}`) // always-wrong code — we want to hit the limiter, not succeed

	const ipMax = 5 // mirrors loginIPMax constant
	for i := 0; i < ipMax; i++ {
		resp, err := http.Post(ts.URL+"/api/auth/totp", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("got 429 on request %d, expected to be within budget", i+1)
		}
	}

	// The (ipMax+1)-th request must be denied.
	resp, err := http.Post(ts.URL+"/api/auth/totp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("final request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}
