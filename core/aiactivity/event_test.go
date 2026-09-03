package aiactivity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEndpointEventRemainsObserved(t *testing.T) {
	event := NewRequestEvent("OpenAI", "ChatGPT", "browser_safari", "Safari", "agent-1", "request-1", time.Now())
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	if event.OriginKind != "endpoint" || event.EvidenceLevel != "observed" {
		t.Fatalf("unexpected provenance: %+v", event)
	}
}

func TestPayloadRejectsContentBearingFields(t *testing.T) {
	for _, field := range []string{
		"prompt", "response_body", "request_body", "file_path", "mcp_payload",
		"mcp_arguments", "credentials", "api_key", "access_token",
		"command_arguments", "repository_contents", "matched_sensitive_values",
	} {
		t.Run(field, func(t *testing.T) {
			if err := ValidatePayloadKeys(map[string]interface{}{field: "secret"}); err == nil {
				t.Fatalf("expected %q to be rejected", field)
			}
		})
	}
}

func FuzzPayloadFieldAllowlist(f *testing.F) {
	for _, seed := range []string{"prompt", "path", "vendor_key", "request_body", "model_identifier"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, field string) {
		err := ValidatePayloadKeys(map[string]interface{}{field: "value"})
		_, allowed := allowedPayloadFields[field]
		if allowed && err != nil {
			t.Fatalf("allowed field rejected: %v", err)
		}
		if !allowed && err == nil {
			t.Fatalf("unknown field accepted: %q", field)
		}
	})
}

func TestDiscoverMacOSMCPReturnsAggregatesOnly(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"mcpServers":{"private":{"command":"/secret/bin","args":["--token","secret"]},"remote":{"url":"https://private.example"}}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverMacOSMCP(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ServerCount != 2 {
		t.Fatalf("unexpected observations: %+v", got)
	}
	if len(got[0].TransportKinds) != 2 {
		t.Fatalf("unexpected transports: %+v", got[0])
	}
}
