package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/themisto/backend/internal/api"
	"github.com/themisto/backend/internal/signing"
	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

// testEnv holds a fully wired test server and helpers.
type testEnv struct {
	server *httptest.Server
	db     *store.Store
	apiKey string
	runID  string
	t      *testing.T
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	db, err := store.New(dsn)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	certPath, keyPath, chainPath := generateTestCA(t)
	signer, err := signing.NewSigner(certPath, keyPath, chainPath, 90)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	apiKey := "test-api-key-" + fmt.Sprintf("%d", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := api.NewServer(db, signer, apiKey, "", "customer_ops", "", 24*time.Hour, "http://localhost:8443", "https://gateway.test", nil, logger, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &testEnv{
		server: server,
		db:     db,
		apiKey: apiKey,
		runID:  fmt.Sprintf("%d", time.Now().UnixNano()),
		t:      t,
	}
}

func generateTestCA(t *testing.T) (certPath, keyPath, chainPath string) {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA", Organization: []string{"TestOrg"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(caKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "ca.crt")
	keyPath = filepath.Join(dir, "ca.key")
	chainPath = filepath.Join(dir, "chain.pem")

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chainPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func (e *testEnv) createOrg(name, slug string) string {
	e.t.Helper()
	rawKey, hash, err := token.Generate()
	if err != nil {
		e.t.Fatalf("generate api key: %v", err)
	}
	_ = rawKey
	org, err := e.db.CreateOrganization(context.Background(), name, slug, hash)
	if err != nil {
		e.t.Fatalf("create org: %v", err)
	}
	return org.ID
}

func (e *testEnv) createUser(orgID, email, name, password, role string) *store.AdminUser {
	e.t.Helper()
	user, err := e.db.CreateUser(context.Background(), orgID, email, name, password, role)
	if err != nil {
		e.t.Fatalf("create user: %v", err)
	}
	return user
}

func (e *testEnv) login(email, password string) (string, map[string]interface{}) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(e.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("login failed: %d %s", resp.StatusCode, string(b))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.t.Fatalf("decode login response: %v", err)
	}
	return result["token"].(string), result
}

func (e *testEnv) authReq(method, path string, body io.Reader, sessionToken string) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.server.URL+path, body)
	if sessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+sessionToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

func (e *testEnv) apiKeyReq(method, path string, body io.Reader) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.server.URL+path, body)
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

func (e *testEnv) changePassword(sessionToken, currentPassword, newPassword string) string {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{
		"current_password": currentPassword,
		"new_password":     newPassword,
	})
	resp := e.authReq(http.MethodPut, "/api/v1/auth/password", bytes.NewReader(body), sessionToken)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		tFatalf(e.t, resp, "change password", b)
	}
	data := readJSON(e.t, resp)
	token, _ := data["token"].(string)
	if token == "" {
		e.t.Fatal("change password did not return replacement session token")
	}
	return token
}

func readJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return m
}

func (e *testEnv) insertDLPEvent(orgID, deviceID, host, severity, actionTaken, reviewStatus, semanticSource string) int64 {
	e.t.Helper()
	var id int64
	err := e.db.DB.QueryRowContext(context.Background(), `
		INSERT INTO dlp_events (
			device_id, org_id, request_host, request_path, request_method, source_app,
			service_category, ai_vendor, match_types, matched_patterns, matched_fields,
			match_count, severity, action_taken, semantic_source, review_status
		) VALUES (
			$1, $2, $3, '/prompt', 'POST', 'browser',
			'ai_llm', 'openai', $4, $5, $6,
			1, $7, $8, NULLIF($9, ''), $10
		) RETURNING id`,
		deviceID, orgID, host,
		pq.Array([]string{"credentials"}), pq.Array([]string{"api_key_environment"}), pq.Array([]string{"prompt"}),
		severity, actionTaken, semanticSource, reviewStatus,
	).Scan(&id)
	if err != nil {
		e.t.Fatalf("insert DLP event: %v", err)
	}
	return id
}

func generateCSRPEM(t *testing.T, cn, org string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{org},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
}

func tFatalf(t *testing.T, resp *http.Response, label string, body []byte) {
	t.Helper()
	t.Fatalf("%s: %d %s", label, resp.StatusCode, string(body))
}

// TestEnrollmentE2E tests the full enrollment flow:
// 1. Create org + admin user
// 2. Login
// 3. Change password (required for fresh admins)
// 4. Create enrollment package
// 5. Submit CSR with enrollment token -> cert issued
func TestEnrollmentE2E(t *testing.T) {
	env := setupTestEnv(t)

	orgID := env.createOrg("Enrollment Test Org", "enroll-test-"+env.runID)
	email := "admin+" + env.runID + "@enroll.test"
	env.createUser(orgID, email, "Admin", "password123", "admin")

	sessionToken, loginData := env.login(email, "password123")

	if user, ok := loginData["user"].(map[string]interface{}); ok {
		if mcp, ok := user["must_change_password"].(bool); !ok || !mcp {
			t.Fatal("expected must_change_password=true for new user")
		}
	}

	sessionToken = env.changePassword(sessionToken, "password123", "password456")

	body, _ := json.Marshal(map[string]interface{}{
		"device_name":     "test-device-" + env.runID,
		"os":              "linux",
		"token_ttl_hours": 24,
	})
	resp := env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(body), sessionToken)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		tFatalf(t, resp, "create enrollment package", b)
	}
	pkg := readJSON(t, resp)

	deviceID, ok := pkg["device_id"].(string)
	if !ok || deviceID == "" {
		t.Fatal("no device_id in enrollment package")
	}

	enrollToken, ok := pkg["enrollment_token"].(string)
	if !ok || enrollToken == "" {
		t.Fatal("no enrollment_token in enrollment package")
	}

	if enrollURL, ok := pkg["enrollment_url"].(string); !ok || enrollURL == "" {
		t.Error("expected enrollment_url in package response")
	}

	csrPEM := generateCSRPEM(t, deviceID, "Enrollment Test Org")
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/devices/"+deviceID+"/csr", bytes.NewReader(csrPEM))
	req.Header.Set("X-Enrollment-Token", enrollToken)
	csrResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit CSR: %v", err)
	}
	if csrResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(csrResp.Body)
		tFatalf(t, csrResp, "CSR submission failed", b)
	}
	certData := readJSON(t, csrResp)

	if certData["certificate"] == nil || certData["certificate"] == "" {
		t.Error("no certificate in CSR response")
	}
	if certData["renewal_token"] == nil || certData["renewal_token"] == "" {
		t.Error("no renewal_token in CSR response")
	}
	if certData["serial"] == nil || certData["serial"] == "" {
		t.Error("no serial in CSR response")
	}
}

// TestRenewalE2E tests certificate renewal flow:
// 1. Enroll device
// 2. Submit renewal CSR with renewal_token -> new cert, old cert superseded
func TestRenewalE2E(t *testing.T) {
	env := setupTestEnv(t)

	orgID := env.createOrg("Renewal Test Org", "renewal-test-"+env.runID)
	email := "admin+" + env.runID + "@renewal.test"
	env.createUser(orgID, email, "Admin", "password123", "admin")

	sessionToken, _ := env.login(email, "password123")
	sessionToken = env.changePassword(sessionToken, "password123", "password456")

	body, _ := json.Marshal(map[string]interface{}{
		"device_name": "renewal-device-" + env.runID,
		"os":          "darwin",
	})
	resp := env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(body), sessionToken)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		tFatalf(t, resp, "create renewal enrollment package", b)
	}
	pkg := readJSON(t, resp)

	deviceID := pkg["device_id"].(string)
	enrollToken := pkg["enrollment_token"].(string)

	csrPEM := generateCSRPEM(t, deviceID, "Renewal Test Org")
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/devices/"+deviceID+"/csr", bytes.NewReader(csrPEM))
	req.Header.Set("X-Enrollment-Token", enrollToken)
	csrResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initial CSR: %v", err)
	}
	if csrResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(csrResp.Body)
		tFatalf(t, csrResp, "initial enrollment failed", b)
	}
	certData := readJSON(t, csrResp)

	renewalToken := certData["renewal_token"].(string)
	firstSerial := certData["serial"].(string)

	csrPEM2 := generateCSRPEM(t, deviceID, "Renewal Test Org")
	req2, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/devices/"+deviceID+"/csr", bytes.NewReader(csrPEM2))
	req2.Header.Set("X-Renewal-Token", renewalToken)
	renewResp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("renewal CSR: %v", err)
	}
	if renewResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(renewResp.Body)
		tFatalf(t, renewResp, "renewal failed", b)
	}
	renewData := readJSON(t, renewResp)

	newSerial := renewData["serial"].(string)
	if newSerial == firstSerial {
		t.Error("renewal should produce a different serial")
	}
	if renewData["renewal_token"] == nil || renewData["renewal_token"] == "" {
		t.Error("renewal should return a new renewal_token")
	}

	oldCert, err := env.db.GetCertStatus(context.Background(), firstSerial)
	if err != nil {
		t.Fatalf("get old cert status: %v", err)
	}
	if oldCert == nil {
		t.Fatal("old cert not found")
	}
	if oldCert.Status != "superseded" {
		t.Errorf("old cert status = %q, want superseded", oldCert.Status)
	}
}

// TestMultiTenancyIsolation verifies that two orgs cannot see each other's data.
func TestMultiTenancyIsolation(t *testing.T) {
	env := setupTestEnv(t)

	orgA := env.createOrg("Org A", "org-a-"+env.runID)
	orgB := env.createOrg("Org B", "org-b-"+env.runID)

	emailA := "admin-a+" + env.runID + "@orga.test"
	emailB := "admin-b+" + env.runID + "@orgb.test"
	env.createUser(orgA, emailA, "Admin A", "password123", "admin")
	env.createUser(orgB, emailB, "Admin B", "password123", "admin")

	tokenA, _ := env.login(emailA, "password123")
	tokenB, _ := env.login(emailB, "password123")
	tokenA = env.changePassword(tokenA, "password123", "password456")
	tokenB = env.changePassword(tokenB, "password123", "password456")

	deviceAName := "device-a-" + env.runID
	deviceBName := "device-b-" + env.runID

	body, _ := json.Marshal(map[string]interface{}{"device_name": deviceAName, "os": "linux"})
	resp := env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(body), tokenA)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		tFatalf(t, resp, "create device A", b)
	}
	resp.Body.Close()

	body, _ = json.Marshal(map[string]interface{}{"device_name": deviceBName, "os": "windows"})
	resp = env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(body), tokenB)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		tFatalf(t, resp, "create device B", b)
	}
	resp.Body.Close()

	resp = env.authReq(http.MethodGet, "/api/v1/orgs/"+orgA+"/devices", nil, tokenA)
	devicesA := readJSON(t, resp)
	if devices, ok := devicesA["devices"].([]interface{}); ok {
		if len(devices) != 1 {
			t.Fatalf("Org A device list count = %d, want 1", len(devices))
		}
		for _, d := range devices {
			dm := d.(map[string]interface{})
			if dm["device_name"] != deviceAName {
				t.Errorf("Org A device list contains wrong device %v", dm["device_name"])
			}
		}
	}

	resp = env.authReq(http.MethodGet, "/api/v1/orgs/"+orgB+"/devices", nil, tokenB)
	devicesB := readJSON(t, resp)
	if devices, ok := devicesB["devices"].([]interface{}); ok {
		if len(devices) != 1 {
			t.Fatalf("Org B device list count = %d, want 1", len(devices))
		}
		for _, d := range devices {
			dm := d.(map[string]interface{})
			if dm["device_name"] != deviceBName {
				t.Errorf("Org B device list contains wrong device %v", dm["device_name"])
			}
		}
	}

	resp = env.authReq(http.MethodGet, "/api/v1/orgs/"+orgB+"/devices", nil, tokenA)
	crossCheck := readJSON(t, resp)
	if devices, ok := crossCheck["devices"].([]interface{}); ok {
		if len(devices) != 1 {
			t.Fatalf("cross-org query device count = %d, want 1", len(devices))
		}
		for _, d := range devices {
			dm := d.(map[string]interface{})
			if dm["device_name"] != deviceAName {
				t.Errorf("cross-org query returned wrong device %v", dm["device_name"])
			}
		}
	}
}

func TestDLPReviewWorkflowAndFilters(t *testing.T) {
	env := setupTestEnv(t)

	if err := env.db.EnsureDLPSchema(context.Background()); err != nil {
		t.Fatalf("ensure DLP schema: %v", err)
	}

	orgA := env.createOrg("DLP Review A "+env.runID, "dlp-review-a-"+env.runID)
	orgB := env.createOrg("DLP Review B "+env.runID, "dlp-review-b-"+env.runID)
	emailA := "review+" + env.runID + "@dlp.test"
	env.createUser(orgA, emailA, "DLP Reviewer", "initial123", "admin")
	sessionToken, _ := env.login(emailA, "initial123")
	sessionToken = env.changePassword(sessionToken, "initial123", "newpassword456")

	eventA := env.insertDLPEvent(orgA, "device-a-"+env.runID, "chatgpt.com", "critical", "block", "unreviewed", "local_deberta_nli")
	env.insertDLPEvent(orgA, "device-a2-"+env.runID, "copilot.microsoft.com", "medium", "alert", "reviewed", "semantic")
	eventB := env.insertDLPEvent(orgB, "device-b-"+env.runID, "chatgpt.com", "critical", "block", "unreviewed", "local_deberta_nli")

	listResp := env.authReq(http.MethodGet, "/api/v1/dlp/events?severity=critical&action_taken=block&review_status=unreviewed&semantic_source=deberta&sort=severity&order=desc&page=1&limit=10", nil, sessionToken)
	listData := readJSON(t, listResp)
	if got := int(listData["total"].(float64)); got != 1 {
		t.Fatalf("filtered DLP total = %d, want 1", got)
	}
	events := listData["events"].([]interface{})
	if gotID := int64(events[0].(map[string]interface{})["id"].(float64)); gotID != eventA {
		t.Fatalf("filtered DLP event id = %d, want %d", gotID, eventA)
	}

	invalidBody, _ := json.Marshal(map[string]string{"review_status": "closed"})
	invalidResp := env.authReq(http.MethodPatch, fmt.Sprintf("/api/v1/dlp/events/%d/review", eventA), bytes.NewReader(invalidBody), sessionToken)
	if invalidResp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(invalidResp.Body)
		tFatalf(t, invalidResp, "expected invalid review status rejection", b)
	}
	invalidData := readJSON(t, invalidResp)
	if invalidData["code"] != "INVALID_REVIEW_STATUS" {
		t.Fatalf("invalid review code = %v", invalidData["code"])
	}

	reviewBody, _ := json.Marshal(map[string]string{
		"review_status": "reviewed",
		"review_note":   "belongs to another org",
	})
	crossOrgResp := env.authReq(http.MethodPatch, fmt.Sprintf("/api/v1/dlp/events/%d/review", eventB), bytes.NewReader(reviewBody), sessionToken)
	if crossOrgResp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(crossOrgResp.Body)
		tFatalf(t, crossOrgResp, "expected cross-org review to be hidden", b)
	}
	crossOrgResp.Body.Close()

	escalateBody, _ := json.Marshal(map[string]string{
		"review_status": "escalated",
		"review_note":   "confirmed key exposure",
	})
	escalateResp := env.authReq(http.MethodPatch, fmt.Sprintf("/api/v1/dlp/events/%d/review", eventA), bytes.NewReader(escalateBody), sessionToken)
	if escalateResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(escalateResp.Body)
		tFatalf(t, escalateResp, "expected review update", b)
	}
	escalated := readJSON(t, escalateResp)
	if escalated["review_status"] != "escalated" || escalated["reviewed_by"] != emailA {
		t.Fatalf("unexpected escalated review payload: %#v", escalated)
	}
	if _, ok := escalated["reviewed_at"].(string); !ok {
		t.Fatalf("reviewed_at missing from escalated payload: %#v", escalated)
	}

	auditRows, total, err := env.db.ListAuditLog(context.Background(), store.AuditLogFilter{
		OrgID:  orgA,
		Action: "dlp.event.reviewed",
		Page:   1,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list audit log: %v", err)
	}
	if total < 1 {
		t.Fatal("expected DLP review audit entry")
	}
	foundAudit := false
	for _, entry := range auditRows {
		if entry.ResourceID == fmt.Sprintf("%d", eventA) && entry.Details["review_status"] == "escalated" {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Fatalf("expected audit entry for event %d, got %#v", eventA, auditRows)
	}

	clearBody, _ := json.Marshal(map[string]string{"review_status": "unreviewed"})
	clearResp := env.authReq(http.MethodPatch, fmt.Sprintf("/api/v1/dlp/events/%d/review", eventA), bytes.NewReader(clearBody), sessionToken)
	if clearResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(clearResp.Body)
		tFatalf(t, clearResp, "expected review clear", b)
	}
	clearResp.Body.Close()
	cleared, err := env.db.GetDLPEvent(context.Background(), orgA, eventA)
	if err != nil {
		t.Fatalf("get cleared event: %v", err)
	}
	if cleared == nil || cleared.ReviewStatus != "unreviewed" || cleared.ReviewNote != nil || cleared.ReviewedBy != nil || cleared.ReviewedAt != nil {
		t.Fatalf("review fields were not cleared: %#v", cleared)
	}
}

// TestRateLimiting verifies that auth rate limiting kicks in after rapid requests.
func TestRateLimiting(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{"email": "nobody@test.com", "password": "wrong"})

	var got429 bool
	for i := 0; i < 10; i++ {
		resp, err := http.Post(env.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login attempt %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}

	if !got429 {
		t.Error("expected 429 rate limit after rapid login attempts")
	}
}

// TestPasswordChangeFlow verifies forced password change, backend enforcement, and subsequent access.
func TestPasswordChangeFlow(t *testing.T) {
	env := setupTestEnv(t)

	orgID := env.createOrg("Password Test Org", "pw-test-"+env.runID)
	email := "user+" + env.runID + "@pw.test"
	env.createUser(orgID, email, "User", "initial123", "admin")

	sessionToken, loginData := env.login(email, "initial123")

	if user, ok := loginData["user"].(map[string]interface{}); ok {
		if mcp, ok := user["must_change_password"].(bool); !ok || !mcp {
			t.Fatal("expected must_change_password=true")
		}
	}

	blockedBody, _ := json.Marshal(map[string]interface{}{
		"device_name": "blocked-device-" + env.runID,
		"os":          "linux",
	})
	blockedResp := env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(blockedBody), sessionToken)
	if blockedResp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(blockedResp.Body)
		tFatalf(t, blockedResp, "expected password-change gate before enrollment package", b)
	}
	blockedData := readJSON(t, blockedResp)
	if blockedData["code"] != "PASSWORD_CHANGE_REQUIRED" {
		t.Fatalf("expected PASSWORD_CHANGE_REQUIRED, got %v", blockedData["code"])
	}

	oldSessionToken := sessionToken
	sessionToken = env.changePassword(sessionToken, "initial123", "newpassword456")

	expiredResp := env.authReq(http.MethodGet, "/api/v1/auth/me", nil, oldSessionToken)
	if expiredResp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(expiredResp.Body)
		tFatalf(t, expiredResp, "expected old session to be revoked after password change", b)
	}
	expiredResp.Body.Close()

	resp := env.authReq(http.MethodGet, "/api/v1/auth/me", nil, sessionToken)
	me := readJSON(t, resp)
	if mcp, ok := me["must_change_password"].(bool); ok && mcp {
		t.Error("must_change_password should be false after password change")
	}

	allowedBody, _ := json.Marshal(map[string]interface{}{
		"device_name": "allowed-device-" + env.runID,
		"os":          "linux",
	})
	allowedResp := env.authReq(http.MethodPost, "/api/v1/devices/enrollment-package", bytes.NewReader(allowedBody), sessionToken)
	if allowedResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(allowedResp.Body)
		tFatalf(t, allowedResp, "expected enrollment package after password change", b)
	}
	allowedResp.Body.Close()
}
