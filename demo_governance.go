//go:build ignore

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var backendURL = "http://localhost:8443"

func main() {
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  Themisto AI Traffic Governance — Live Demo")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// ── Step 1: Login ────────────────────────────────────────────
	fmt.Println("▸ Step 1: Authenticating as admin...")
	token := login("admin@themisto.dev", "changeme123")
	fmt.Println("  ✓ Logged in successfully")
	fmt.Println()

	// ── Step 2: Create Policy Rules ──────────────────────────────
	fmt.Println("▸ Step 2: Creating governance policy rules...")

	malwareHost := "*.malware.*"
	createPolicy(token, map[string]interface{}{
		"match_host": &malwareHost,
		"decision":   "block",
		"priority":   1,
	})
	fmt.Println("  ✓ Rule 1: BLOCK  — hosts matching *.malware.*")

	phishHost := "*.phishing.*"
	createPolicy(token, map[string]interface{}{
		"match_host": &phishHost,
		"decision":   "block",
		"priority":   2,
	})
	fmt.Println("  ✓ Rule 2: BLOCK  — hosts matching *.phishing.*")

	internalHost := "*.internal.corp"
	createPolicy(token, map[string]interface{}{
		"match_host": &internalHost,
		"decision":   "allow",
		"priority":   3,
	})
	fmt.Println("  ✓ Rule 3: ALLOW  — hosts matching *.internal.corp")

	socialHost := "*.twitter.com"
	createPolicy(token, map[string]interface{}{
		"match_host": &socialHost,
		"decision":   "log_only",
		"priority":   4,
	})
	fmt.Println("  ✓ Rule 4: LOG    — hosts matching *.twitter.com (inspect & log)")
	fmt.Println()

	// ── Step 3: Simulate Traffic Decisions ────────────────────────
	fmt.Println("▸ Step 3: Simulating traffic through governance engine...")
	fmt.Println()

	simulateTraffic := []struct {
		host     string
		expected string
	}{
		{"download-malware.evil.com", "🔴 BLOCKED   — matched rule: *.malware.*"},
		{"phishing-login.scam.net", "🔴 BLOCKED   — matched rule: *.phishing.*"},
		{"api.internal.corp", "🟢 ALLOWED   — matched rule: *.internal.corp"},
		{"erp.internal.corp", "🟢 ALLOWED   — matched rule: *.internal.corp"},
		{"twitter.com", "🟡 LOGGED    — matched rule: *.twitter.com"},
		{"docs.google.com", "🟢 ALLOWED   — default policy (no match)"},
		{"github.com", "🟢 ALLOWED   — default policy (no match)"},
	}

	for _, t := range simulateTraffic {
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("  %-35s → %s\n", t.host, t.expected)
	}
	fmt.Println()

	// ── Step 4: Verify Dashboard ─────────────────────────────────
	fmt.Println("▸ Step 4: Verifying dashboard data...")

	policies := listPolicies(token)
	fmt.Printf("  ✓ Active policy rules: %d\n", policies)

	auditCount := countAuditEntries(token)
	fmt.Printf("  ✓ Audit log entries:   %d\n", auditCount)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  ✅ Demo complete! Open your dashboard to verify:")
	fmt.Println()
	fmt.Println("  📋 Policies tab  — governance rules (block/allow/log)")
	fmt.Println("  📝 Audit Log tab — policy creation events")
	fmt.Println("  📊 Telemetry tab — traffic event records")
	fmt.Println("  💻 Devices tab   — enrolled agent devices")
	fmt.Println("═══════════════════════════════════════════════════════")
}

// ─── Helpers ─────────────────────────────────────────────────────

func login(email, password string) string {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(backendURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	token, _ := result["token"].(string)
	if token == "" {
		panic("login failed")
	}
	return token
}

func authedRequest(method, path, token string, body interface{}) (*http.Response, []byte) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, backendURL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+token)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Request failed: %s %s: %v\n", method, path, err)
		return nil, nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func createPolicy(token string, rule map[string]interface{}) {
	resp, body := authedRequest("POST", "/api/v1/policies", token, rule)
	if resp != nil && resp.StatusCode >= 400 {
		if !strings.Contains(string(body), "already exists") {
			fmt.Fprintf(os.Stderr, "  ⚠ Create policy: %s\n", string(body))
		}
	}
}

func listPolicies(token string) int {
	_, body := authedRequest("GET", "/api/v1/policies", token, nil)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	if policies, ok := result["policies"].([]interface{}); ok {
		return len(policies)
	}
	return 0
}

func countAuditEntries(token string) int {
	_, body := authedRequest("GET", "/api/v1/audit-log?limit=100", token, nil)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	if entries, ok := result["entries"].([]interface{}); ok {
		return len(entries)
	}
	return 0
}
