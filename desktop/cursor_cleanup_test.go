package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/themisto/agent/core/cursorcleanup"
	"github.com/themisto/agent/core/dlp"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temp state.vscdb with the given key/value rows.
func createTestDB(t *testing.T, dir string, rows map[string]string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for k, v := range rows {
		if _, err := db.Exec("INSERT INTO ItemTable (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	return dbPath
}

func TestScanVscdbFindsSSN(t *testing.T) {
	dir := t.TempDir()
	dbPath := createTestDB(t, dir, map[string]string{
		"composer.composerData": `{"messages":[{"text":"my ssn is 123-45-6789"}]}`,
	})

	patterns := dlp.RedactionPatterns()
	findings, err := cursorcleanup.ScanVscdb(dbPath, patterns)
	if err != nil {
		t.Fatalf("ScanVscdb: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least one finding for SSN")
	}

	found := false
	for _, f := range findings {
		if f.PatternName == "ssn" {
			found = true
			if f.MatchCount < 1 {
				t.Error("match_count should be >= 1")
			}
			if f.Source != "database" {
				t.Errorf("source should be database, got %q", f.Source)
			}
		}
	}
	if !found {
		t.Error("ssn pattern not found in findings")
	}
}

func TestScanVscdbFindsCredentials(t *testing.T) {
	dir := t.TempDir()
	dbPath := createTestDB(t, dir, map[string]string{
		"chat.data": "here is my key: sk-abcdefghijklmnopqrstuvwx",
	})

	patterns := dlp.RedactionPatterns()
	findings, err := cursorcleanup.ScanVscdb(dbPath, patterns)
	if err != nil {
		t.Fatalf("ScanVscdb: %v", err)
	}

	found := false
	for _, f := range findings {
		if f.PatternName == "api_key_openai" {
			found = true
		}
	}
	if !found {
		t.Error("api_key_openai pattern not found in findings")
	}
}

func TestScanVscdbNoFindings(t *testing.T) {
	dir := t.TempDir()
	dbPath := createTestDB(t, dir, map[string]string{
		"settings": `{"theme":"dark","fontSize":14}`,
	})

	patterns := dlp.RedactionPatterns()
	findings, err := cursorcleanup.ScanVscdb(dbPath, patterns)
	if err != nil {
		t.Fatalf("ScanVscdb: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestRedactVscdb(t *testing.T) {
	dir := t.TempDir()
	ssn := "123-45-6789"
	dbPath := createTestDB(t, dir, map[string]string{
		"chat": "my ssn is " + ssn + " please help",
	})

	patterns := dlp.RedactionPatterns()
	n, err := cursorcleanup.RedactVscdb(dbPath, patterns)
	if err != nil {
		t.Fatalf("RedactVscdb: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row updated, got %d", n)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow("SELECT value FROM ItemTable WHERE key = 'chat'").Scan(&value); err != nil {
		t.Fatalf("query: %v", err)
	}
	if strings.Contains(value, ssn) {
		t.Error("SSN still present after redaction")
	}
	if !strings.Contains(value, dlp.Replacement) {
		t.Error("replacement label not found after redaction")
	}
}

func TestRedactionPreservesJSON(t *testing.T) {
	dir := t.TempDir()
	original := `{"user":{"name":"John","ssn":"123-45-6789"},"active":true}`
	dbPath := createTestDB(t, dir, map[string]string{
		"data": original,
	})

	patterns := dlp.RedactionPatterns()
	_, err := cursorcleanup.RedactVscdb(dbPath, patterns)
	if err != nil {
		t.Fatalf("RedactVscdb: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow("SELECT value FROM ItemTable WHERE key = 'data'").Scan(&value); err != nil {
		t.Fatalf("query: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		t.Errorf("redacted value is not valid JSON: %v\nvalue: %s", err, value)
	}
	if strings.Contains(value, "123-45-6789") {
		t.Error("SSN still present in JSON")
	}
}

func TestScanTranscriptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.md")
	content := "# Chat\nUser: my key is sk-abcdefghijklmnopqrstuvwx\nAssistant: ok\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	patterns := dlp.RedactionPatterns()
	findings, err := cursorcleanup.ScanTranscriptFile(path, patterns)
	if err != nil {
		t.Fatalf("ScanTranscriptFile: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings for API key in transcript")
	}
	if findings[0].Source != "transcript" {
		t.Errorf("source should be transcript, got %q", findings[0].Source)
	}
}

func TestRedactTranscriptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.md")
	apiKey := "sk-abcdefghijklmnopqrstuvwx"
	content := "# Chat\nUser: my key is " + apiKey + "\nAssistant: ok\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	patterns := dlp.RedactionPatterns()
	n, err := cursorcleanup.RedactTranscriptFile(path, patterns)
	if err != nil {
		t.Fatalf("RedactTranscriptFile: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 file redacted, got %d", n)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), apiKey) {
		t.Error("API key still present after redaction")
	}
	if !strings.Contains(string(data), dlp.Replacement) {
		t.Error("replacement label not found after redaction")
	}
}

func TestSafeCleanupExcerpt(t *testing.T) {
	text := "my ssn is 123-45-6789 ok"
	loc := []int{10, 21}
	excerpt := cursorcleanup.SafeExcerpt(text, loc)
	if !strings.Contains(excerpt, "***") {
		t.Errorf("excerpt should contain ***, got %q", excerpt)
	}
	if strings.Contains(excerpt, "123-45-6789") {
		t.Error("excerpt should not contain full SSN")
	}
}
