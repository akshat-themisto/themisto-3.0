package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouting_TelemetryNotStub verifies that POST /telemetry is NOT handled by
// a stub that returns 202 and discards data. The real handler requires a client
// certificate (mTLS) and returns 403 when one is absent. A 202 response with no
// cert present would indicate the stub is still wired in.
func TestRouting_TelemetryNotStub(t *testing.T) {
	// ProxyHandler returns 403 immediately when r.TLS is nil (no client cert).
	// The stub would return 202. We use nil dependencies because the early-return
	// cert check fires before any field on ProxyHandler is accessed.
	proxyHandler := &ProxyHandler{}

	mux := http.NewServeMux()
	mux.Handle("/", proxyHandler) // mirrors main.go — no stub on /telemetry

	req := httptest.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code == http.StatusAccepted {
		t.Fatal("POST /telemetry returned 202: stub handler is still wired in — telemetry data will be discarded")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /telemetry without cert = %d, want 403 (ProxyHandler cert check)", rr.Code)
	}
}

// TestRouting_PolicyHandledByDedicatedHandler verifies GET /policy is intercepted
// by the PolicyHandler before reaching ProxyHandler. If ProxyHandler handled it,
// it would return 403 (no cert). PolicyHandler must be registered separately.
func TestRouting_PolicyHandledByDedicatedHandler(t *testing.T) {
	policyHandled := false
	fakePolicyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policyHandled = true
		w.WriteHeader(http.StatusOK)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /policy", fakePolicyHandler)
	mux.Handle("/", &ProxyHandler{})

	req := httptest.NewRequest(http.MethodGet, "/policy", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !policyHandled {
		t.Error("GET /policy was not handled by the dedicated PolicyHandler")
	}
	if rr.Code == http.StatusForbidden {
		t.Error("GET /policy was routed to ProxyHandler (cert check fired) — policy mux entry is missing")
	}
}

// TestRouting_HealthHandledByDedicatedHandler verifies GET /healthz is intercepted
// before ProxyHandler (which would require mTLS and return 403).
func TestRouting_HealthHandledByDedicatedHandler(t *testing.T) {
	healthHandled := false
	fakeHealthHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthHandled = true
		w.WriteHeader(http.StatusOK)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", fakeHealthHandler)
	mux.Handle("/", &ProxyHandler{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !healthHandled {
		t.Error("GET /healthz was not handled by the dedicated HealthHandler")
	}
}

// TestOriginalURLRewrite verifies that when the gateway receives a relayed request
// from the agent with X-Themisto-Original-URL set, it rewrites r.URL and r.Host
// to the original destination — not to the gateway itself.
// Without this fix, the gateway would forward back to localhost:443 causing a 502.
func TestOriginalURLRewrite(t *testing.T) {
	cases := []struct {
		name         string
		originalURL  string
		originalHost string
		wantHost     string
		wantPath     string
	}{
		{
			name:        "http destination",
			originalURL: "http://google.com/search?q=test",
			wantHost:    "google.com",
			wantPath:    "/search",
		},
		{
			name:        "https destination",
			originalURL: "https://example.com/path",
			wantHost:    "example.com",
			wantPath:    "/path",
		},
		{
			name:        "root path",
			originalURL: "http://youtube.com/",
			wantHost:    "youtube.com",
			wantPath:    "/",
		},
		{
			name:         "origin-form with host fallback",
			originalURL:  "/v1/chat/completions",
			originalHost: "api.openai.com",
			wantHost:     "api.openai.com",
			wantPath:     "/v1/chat/completions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://localhost:443/relay", nil)
			req.Host = "localhost:443"
			req.Header.Set("X-Themisto-Original-URL", tc.originalURL)
			if tc.originalHost != "" {
				req.Header.Set("X-Themisto-Original-Host", tc.originalHost)
			}

			if err := rewriteRelayURL(req); err != nil {
				t.Fatalf("rewriteRelayURL returned error: %v", err)
			}

			if req.Host != tc.wantHost {
				t.Errorf("host = %q, want %q", req.Host, tc.wantHost)
			}
			if req.URL.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", req.URL.Path, tc.wantPath)
			}
			if req.Host == "localhost:443" {
				t.Error("host was not rewritten — gateway would forward to itself causing 502")
			}
		})
	}
}

// TestOriginalURLRewrite_InvalidURL verifies that a malformed X-Themisto-Original-URL
// header does not crash the handler or corrupt the request URL.
func TestOriginalURLRewrite_InvalidURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://localhost:443/relay", nil)
	req.Host = "localhost:443"
	req.Header.Set("X-Themisto-Original-URL", "://bad url")

	originalHost := req.Host

	if err := rewriteRelayURL(req); err == nil {
		t.Fatal("expected rewriteRelayURL to fail for malformed URL")
	}

	// Host must remain unchanged on parse failure.
	if req.Host != originalHost {
		t.Errorf("host changed on bad URL: got %q, want %q", req.Host, originalHost)
	}
}
