package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnforceOrganizationActive_DeniesSuspendedOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"suspended","reason":"billing hold"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "secret", "closed", time.Minute)
	err := client.EnforceOrganizationActive(context.Background(), "org-1")
	if err == nil {
		t.Fatal("expected suspended org to be denied")
	}
	if err != nil && err.Error() == "" {
		t.Fatal("expected useful error")
	}
}

func TestEnforceDeviceActive_FailOpenOnControlPlaneError(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "secret", "open", time.Minute)
	if err := client.EnforceDeviceActive(context.Background(), "device-1"); err != nil {
		t.Fatalf("expected fail-open behavior, got %v", err)
	}
}
