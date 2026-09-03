package handler

import (
	"testing"
	"time"
)

func validAIActivityPayload() map[string]interface{} {
	return map[string]interface{}{
		"vendor_key": "acme", "product_key": "assistant", "surface": "desktop",
		"activity_kind": "request", "observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"count": float64(1), "origin_kind": "endpoint",
		"source_evidence_level": "observed", "reconciliation_status": "not_applicable",
		"evidence_level": "observed", "source_identifier": "agent-1",
		"source_key": "8f14e45fceea167a5a36dedd4bea2543", "freshness_at": time.Now().UTC().Format(time.RFC3339Nano),
		"scope": "device",
	}
}

func TestDecodeAIActivityRejectsContentFields(t *testing.T) {
	for _, field := range []string{"prompt", "request_body", "response_body", "file_path", "mcp_payload", "mcp_arguments", "credentials", "api_key", "access_token", "command_arguments", "repository_contents"} {
		t.Run(field, func(t *testing.T) {
			payload := validAIActivityPayload()
			payload[field] = "secret"
			if _, err := decodeAIActivity(payload, time.Now(), "device", "org"); err == nil {
				t.Fatalf("expected %s to be rejected", field)
			}
		})
	}
}

func TestDecodeAIActivityForcesEndpointIdentity(t *testing.T) {
	event, err := decodeAIActivity(validAIActivityPayload(), time.Now(), "device-from-mtls", "org-from-mtls")
	if err != nil {
		t.Fatal(err)
	}
	if event.DeviceID != "device-from-mtls" || event.OrgID != "org-from-mtls" {
		t.Fatalf("event did not use mTLS identity: %+v", event)
	}
}

func TestCentralTelemetryRejectsContentFieldsRecursively(t *testing.T) {
	for _, field := range []string{
		"path", "prompt", "prompt_text", "request_body", "response_body", "file_path",
		"matched_patterns", "matched_fields", "matched_excerpts", "mcp_payload",
		"mcp_arguments", "credentials", "api_key", "access_token", "command_arguments",
		"repository_contents", "reason_detail", "semantic_reason",
	} {
		t.Run(field, func(t *testing.T) {
			if got, found := forbiddenCentralTelemetryField(map[string]interface{}{
				"metadata": map[string]interface{}{field: "private"},
			}); !found || got != field {
				t.Fatalf("content field %q was not rejected: got=%q found=%v", field, got, found)
			}
		})
	}
}
