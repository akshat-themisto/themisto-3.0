package dlp

import (
	"bytes"
	"mime/multipart"
	"testing"

	"github.com/themisto/agent/core/domain"
)

func TestScan_PII_SSN(t *testing.T) {
	body := []byte(`{"message": "My SSN is 123-45-6789, please keep it safe"}`)
	info := Scan(body, nil)
	if !info.Scanned {
		t.Error("expected Scanned=true")
	}
	if !info.ContainsPII {
		t.Error("expected ContainsPII=true for SSN")
	}
	found := false
	for _, m := range info.Matches {
		if m.Type == domain.DLPMatchPII && m.Pattern == "ssn" {
			found = true
		}
	}
	if !found {
		t.Error("expected ssn pattern match")
	}
}

func TestScan_PII_Email(t *testing.T) {
	body := []byte(`{"prompt": "Contact john.doe@example.com for details"}`)
	info := Scan(body, nil)
	if !info.ContainsPII {
		t.Errorf("expected ContainsPII=true for email, matches: %v", info.Matches)
	}
}

func TestScan_Credentials_OpenAI(t *testing.T) {
	body := []byte(`{"api_key": "sk-abcdefghijklmnopqrstuvwxyz012345"}`)
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Error("expected ContainsCredentials=true for OpenAI key")
	}
	if len(info.Matches) == 0 {
		t.Error("expected at least one match")
	}
	if info.Matches[0].Excerpt != "[REDACTED]" {
		t.Errorf("expected [REDACTED] excerpt for credentials, got %q", info.Matches[0].Excerpt)
	}
}

func TestScan_Credentials_OpenAITestKey(t *testing.T) {
	body := []byte(`{"api_key": "sk-test-1234567890abcdef1234567890"}`)
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Error("expected ContainsCredentials=true for OpenAI test key")
	}
}

func TestScan_Credentials_EnvironmentKeyIsNotSSN(t *testing.T) {
	body := []byte(`this is my live key sk-live-123456789`)
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Fatal("expected environment-prefixed key to be detected as credentials")
	}
	if info.ContainsPII {
		t.Fatalf("expected embedded key digits not to be classified as PII: %#v", info.Matches)
	}
	if info.Severity != "critical" {
		t.Fatalf("severity=%q want=critical", info.Severity)
	}
	if len(info.Matches) != 1 || info.Matches[0].Pattern != "api_key_environment" {
		t.Fatalf("matches=%#v want one api_key_environment match", info.Matches)
	}
}

func TestScan_Credentials_AWSKey(t *testing.T) {
	body := []byte(`AKIAIOSFODNN7EXAMPLE`)
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Error("expected ContainsCredentials=true for AWS key")
	}
}

func TestScan_Credentials_PrivateKey(t *testing.T) {
	body := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...")
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Error("expected ContainsCredentials=true for private key block")
	}
}

func TestScan_Credentials_GitHubToken(t *testing.T) {
	body := []byte(`ghp_abcdefghijklmnopqrstuvwxyz01234567890`)
	info := Scan(body, nil)
	if !info.ContainsCredentials {
		t.Error("expected ContainsCredentials=true for GitHub token")
	}
}

func TestScan_SourceCode_Python(t *testing.T) {
	body := []byte(`import os
from pathlib import Path

def process_file(path):
    with open(path) as f:
        return f.read()
`)
	info := Scan(body, nil)
	if !info.ContainsSourceCode {
		t.Error("expected ContainsSourceCode=true for Python code")
	}
}

func TestScan_SourceCode_Go(t *testing.T) {
	body := []byte(`package main

import (
	"fmt"
)

func main() {
	fmt.Println("hello")
}
`)
	info := Scan(body, nil)
	if !info.ContainsSourceCode {
		t.Error("expected ContainsSourceCode=true for Go code")
	}
}

func TestScan_CustomKeyword(t *testing.T) {
	body := []byte(`{"message": "The project is CONFIDENTIAL, do not share"}`)
	info := Scan(body, []string{"CONFIDENTIAL", "Project X"})
	found := false
	for _, m := range info.Matches {
		if m.Type == domain.DLPMatchKeyword {
			found = true
		}
	}
	if !found {
		t.Error("expected keyword match for CONFIDENTIAL")
	}
}

func TestScan_EmptyBody(t *testing.T) {
	info := Scan(nil, nil)
	if !info.Scanned {
		t.Error("expected Scanned=true for empty body")
	}
	if info.ContainsPII || info.ContainsCredentials || info.ContainsSourceCode {
		t.Error("expected no matches for empty body")
	}
}

func TestScan_CleanBody(t *testing.T) {
	body := []byte(`{"message": "What is the capital of France?"}`)
	info := Scan(body, nil)
	if info.ContainsPII || info.ContainsCredentials || info.ContainsSourceCode {
		t.Errorf("expected no matches for clean body, got: %v", info.Matches)
	}
}

func TestScan_LargeBody_Truncated(t *testing.T) {
	// Generate a body larger than MaxBodySize.
	big := make([]byte, MaxBodySize+1000)
	for i := range big {
		big[i] = 'a'
	}
	// Put a SSN (with surrounding whitespace for word boundaries) at the start.
	copy(big, []byte("SSN: 123-45-6789 "))
	info := Scan(big, nil)
	if !info.ContainsPII {
		t.Error("expected ContainsPII=true (SSN at start of large body)")
	}
}

func TestScanRequest_JSONExtraction(t *testing.T) {
	body := []byte(`{"messages":[{"content":"my token=sk-ant-abcdefghijklmnopqrstuvwxyz012345"}]}`)
	info := ScanRequest("application/json", "/v1/messages", body, nil)
	if !info.ContainsCredentials {
		t.Fatalf("expected credentials in JSON extraction, got %#v", info)
	}
	if info.Severity != "critical" {
		t.Fatalf("severity = %q, want critical", info.Severity)
	}
	if len(info.MatchedFields) == 0 {
		t.Fatal("expected matched fields to be populated")
	}
}

func TestScanRequest_MultipartExtraction(t *testing.T) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	fw, err := w.CreateFormField("prompt")
	if err != nil {
		t.Fatalf("CreateFormField: %v", err)
	}
	_, _ = fw.Write([]byte("employee ssn is 123-45-6789"))

	ffw, err := w.CreateFormFile("file", "snippet.py")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = ffw.Write([]byte("import os\n\ndef hello():\n    return os.getenv('X')\n"))
	_ = w.Close()

	info := ScanRequest("multipart/form-data; boundary="+w.Boundary(), "/upload", b.Bytes(), nil)
	if !info.ContainsPII {
		t.Fatalf("expected pii from multipart text field, got %#v", info)
	}
	if !info.ContainsSourceCode {
		t.Fatalf("expected source code from multipart file, got %#v", info)
	}
	if info.FileCount == 0 {
		t.Fatalf("expected file count > 0, got %d", info.FileCount)
	}
}

func TestScanRequest_PDFTextLayer(t *testing.T) {
	pdfLike := []byte("%PDF-1.4\nBT (api_key=sk-abcdefghijklmnopqrstuvwxyz012345) Tj ET\n%%EOF")
	info := ScanRequest("application/pdf", "/upload", pdfLike, nil)
	if !info.ContainsCredentials {
		t.Fatalf("expected credentials from pdf text layer, got %#v", info)
	}
}

func TestShouldSuppressPath(t *testing.T) {
	if !ShouldSuppressPath("/v1/telemetry/events") {
		t.Fatal("expected telemetry path to be suppressed")
	}
	if ShouldSuppressPath("/v1/chat/completions") {
		t.Fatal("did not expect normal chat path to be suppressed")
	}
}
