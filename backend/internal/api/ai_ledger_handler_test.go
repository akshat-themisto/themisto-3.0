package api

import "testing"

func TestAILedgerExportFieldsExcludeContent(t *testing.T) {
	forbidden := map[string]struct{}{
		"prompt": {}, "response_body": {}, "request_body": {}, "file": {}, "file_path": {},
		"matched_sensitive_values": {}, "mcp_payload": {}, "mcp_arguments": {},
		"credentials": {}, "api_key": {}, "access_token": {}, "command_arguments": {},
		"repository_contents": {},
	}
	for _, field := range aiLedgerExportFields {
		if _, found := forbidden[field]; found {
			t.Fatalf("AI Ledger export exposes forbidden field %q", field)
		}
	}
}
