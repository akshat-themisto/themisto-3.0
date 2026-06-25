package cursorcleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/themisto/agent/core/dlp"
)

type testPaths struct {
	workspaceRoot   string
	transcriptsRoot string
}

func (p testPaths) WorkspaceStorageRoot() (string, error) { return p.workspaceRoot, nil }
func (p testPaths) TranscriptsRoot() (string, error)      { return p.transcriptsRoot, nil }

func TestCleaner_FindsAndRedactsNestedJSONLTranscript(t *testing.T) {
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "project-a", "agent-transcripts", "session-1", "session-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "customer ssn: 123-45-6789\nopenai key: sk-test-1234567890abcdef1234567890\n"
	if err := os.WriteFile(transcriptPath, []byte(body), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cleaner := NewCleaner(testPaths{
		workspaceRoot:   filepath.Join(root, "workspaceStorage"),
		transcriptsRoot: root,
	})

	preview := cleaner.GetPreview()
	if preview.TranscriptCount != 1 {
		t.Fatalf("TranscriptCount = %d, want 1", preview.TranscriptCount)
	}

	scan := cleaner.Scan()
	if scan.TotalFindings == 0 {
		t.Fatal("expected findings for nested jsonl transcript")
	}

	clean := cleaner.Clean()
	if clean.RedactedCount != 1 {
		t.Fatalf("RedactedCount = %d, want 1", clean.RedactedCount)
	}

	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "123-45-6789") || strings.Contains(text, "sk-test-1234567890abcdef1234567890") {
		t.Fatalf("expected transcript to be redacted, got %q", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("expected redaction marker in %q", text)
	}
}

func TestCleanBlockedPromptTranscript_RedactsOnlyTargetTranscript(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "project-a", "agent-transcripts", "session-1", "session-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		t.Fatalf("MkdirAll target: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("token: sk-test-1234567890abcdef1234567890\n"), 0644); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}

	otherPath := filepath.Join(root, "globalStorage", "state.vscdb")
	if err := os.MkdirAll(filepath.Dir(otherPath), 0755); err != nil {
		t.Fatalf("MkdirAll other: %v", err)
	}
	otherContent := `{"user_email":"owner@themisto.test","token":"keep-this-local-state"}`
	if err := os.WriteFile(otherPath, []byte(otherContent), 0644); err != nil {
		t.Fatalf("WriteFile other: %v", err)
	}

	result := CleanBlockedPromptTranscript(targetPath)
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.RedactedCount != 1 {
		t.Fatalf("expected one transcript redacted, got %d", result.RedactedCount)
	}

	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	if strings.Contains(string(targetData), "sk-test-1234567890abcdef1234567890") {
		t.Fatalf("expected transcript secret to be redacted, got %q", string(targetData))
	}

	otherData, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatalf("ReadFile other: %v", err)
	}
	if string(otherData) != otherContent {
		t.Fatalf("expected unrelated local state to remain untouched, got %q", string(otherData))
	}
}

func TestCleanBlockedPromptTranscript_SkipsMissingPath(t *testing.T) {
	result := CleanBlockedPromptTranscript("")
	if !result.Success {
		t.Fatalf("expected skip result to be successful, got %+v", result)
	}
	if result.RedactedCount != 0 {
		t.Fatalf("expected no redactions, got %d", result.RedactedCount)
	}
	if !strings.Contains(result.Message, "skipped") {
		t.Fatalf("expected skip message, got %q", result.Message)
	}
}

func TestRedactSensitiveTextPreservesJSON(t *testing.T) {
	original := `{"user":{"name":"John","ssn":"123-45-6789"},"active":true}`
	redacted := redactSensitiveText(original, dlp.RedactionPatterns())
	if strings.Contains(redacted, "123-45-6789") {
		t.Fatalf("SSN still present after redaction: %s", redacted)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(redacted), &parsed); err != nil {
		t.Fatalf("redacted JSON is invalid: %v\n%s", err, redacted)
	}
}
