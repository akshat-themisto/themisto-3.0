package mtls

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestIsValidHexSerial(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abcdef0123456789", true},
		{"ABCDEF0123456789", true},
		{"00", true},
		{"", false},
		{"zzzz", false},
		{"abc/def", false},
		{"ab cd", false},
	}
	for _, tt := range tests {
		if got := isValidHexSerial(tt.input); got != tt.want {
			t.Errorf("isValidHexSerial(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsRevoked_InvalidSerial(t *testing.T) {
	v := NewCertVerifier("http://localhost", "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("not-hex!!!")
	if err == nil {
		t.Fatal("expected error for invalid serial")
	}
	if !revoked {
		t.Error("invalid serial should be treated as revoked")
	}
}

func TestIsRevoked_ActiveCert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "aabbccdd",
			Status:   "active",
			DeviceID: "dev-1",
			OrgID:    "org-1",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Error("active cert should not be revoked")
	}
}

func TestIsRevoked_RevokedCert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "aabbccdd",
			Status:   "revoked",
			DeviceID: "dev-1",
			OrgID:    "org-1",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Error("revoked cert should be marked revoked")
	}
}

func TestIsRevoked_CacheHit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial: "aabbccdd",
			Status: "active",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())

	v.IsRevoked("aabbccdd")
	v.IsRevoked("aabbccdd")

	if calls != 1 {
		t.Errorf("expected 1 backend call (cache hit), got %d", calls)
	}
}

func TestIsRevoked_CacheExpiry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial: "aabbccdd",
			Status: "active",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 1*time.Millisecond, 100, testLogger())

	v.IsRevoked("aabbccdd")
	time.Sleep(5 * time.Millisecond)
	v.IsRevoked("aabbccdd")

	if calls != 2 {
		t.Errorf("expected 2 backend calls (cache expired), got %d", calls)
	}
}

func TestIsRevoked_NotFound_TreatedAsRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Error("unknown cert should be treated as revoked")
	}
}

func TestIsRevoked_BackendDown_FailOpen(t *testing.T) {
	v := NewCertVerifier("http://127.0.0.1:1", "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err == nil {
		t.Fatal("expected error when backend is down")
	}
	if revoked {
		t.Error("fail-open should not treat as revoked")
	}
}

func TestIsRevoked_BackendDown_FailClosed(t *testing.T) {
	v := NewCertVerifier("http://127.0.0.1:1", "closed", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err == nil {
		t.Fatal("expected error when backend is down")
	}
	if !revoked {
		t.Error("fail-closed should treat as revoked")
	}
}

func TestIsRevoked_InternalTokenSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "secret-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial: "aabbccdd",
			Status: "active",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "secret-tok", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Error("should not be revoked when token is valid")
	}
}

func TestCacheEviction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{Status: "active"})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 2, testLogger())

	v.IsRevoked("aa")
	v.IsRevoked("bb")
	v.IsRevoked("cc") // should evict oldest

	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.cache) > 2 {
		t.Errorf("cache should not exceed maxSize=2, got %d", len(v.cache))
	}
}

func TestLookupIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "aabbccdd",
			Status:   "active",
			DeviceID: "dev-1",
			OrgID:    "org-1",
		})
	}))
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", "", "", "", 0, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked {
		t.Fatal("expected active cert")
	}

	deviceID, orgID, ok := v.LookupIdentity("aabbccdd")
	if !ok {
		t.Fatal("expected cached identity")
	}
	if deviceID != "dev-1" || orgID != "org-1" {
		t.Fatalf("unexpected identity: %s %s", deviceID, orgID)
	}
}

func TestIsRevoked_NormalizesOddLengthCertificateSerial(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/cert-status/02ba", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "02ba",
			Status:   "active",
			DeviceID: "dev-odd",
			OrgID:    "org-1",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "closed", "", "", "", "", 0, time.Minute, 100, testLogger())
	revoked, err := v.IsRevoked("2ba")
	if err != nil {
		t.Fatalf("odd-length serial should normalize: %v", err)
	}
	if revoked {
		t.Fatal("normalized active certificate was treated as revoked")
	}
	deviceID, _, ok := v.LookupIdentity("2ba")
	if !ok || deviceID != "dev-odd" {
		t.Fatalf("normalized identity lookup failed: device=%q ok=%v", deviceID, ok)
	}
}

func TestIsRevoked_ControlPlaneSuspendedOrg(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/cert-status/aabbccdd", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "aabbccdd",
			Status:   "active",
			DeviceID: "dev-1",
			OrgID:    "org-1",
		})
	})
	mux.HandleFunc("/api/v1/enforcement/org-status/org-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "suspended"})
	})
	mux.HandleFunc("/api/v1/enforcement/device-status/dev-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "active", "organization_status": "suspended"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	v := NewCertVerifier(srv.URL, "open", "", srv.URL, "cp-token", "closed", 30*time.Second, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("suspended org should be denied at gateway")
	}
}

func TestIsRevoked_ControlPlaneUnavailableFailOpen(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(certStatusResponse{
			Serial:   "aabbccdd",
			Status:   "active",
			DeviceID: "dev-1",
			OrgID:    "org-1",
		})
	}))
	defer backend.Close()

	v := NewCertVerifier(backend.URL, "open", "", "http://127.0.0.1:1", "cp-token", "open", 30*time.Second, 60*time.Second, 100, testLogger())
	revoked, err := v.IsRevoked("aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error in control-plane fail-open mode: %v", err)
	}
	if revoked {
		t.Fatal("control-plane fail-open should allow traffic when control plane is unavailable")
	}
}
