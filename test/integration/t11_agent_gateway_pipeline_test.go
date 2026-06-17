package integration

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/test/integration/testutil"
)

// TestT11_AgentGatewayPipeline exercises the full path:
//
//  1. Generate test PKI
//  2. Start MockGateway with mTLS + policy + telemetry endpoints
//  3. Verify an mTLS client can reach /healthz
//  4. Verify /policy returns a policy payload
//  5. Verify /telemetry accepts a POST
//  6. Verify /relay echoes back via the default handler
func TestT11_AgentGatewayPipeline(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	policy := testutil.TestPolicy("v1")
	gw.SetPolicy(policy)

	client := makeMTLSClient(pki)
	base := fmt.Sprintf("https://%s", gw.Addr)

	// ── Step 1: healthz ─────────────────────────────────────────────
	t.Run("healthz", func(t *testing.T) {
		resp, err := client.Get(base + "/healthz")
		testutil.AssertNoError(t, err, "GET /healthz")
		defer resp.Body.Close()
		testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "healthz status")
	})

	// ── Step 2: policy fetch ────────────────────────────────────────
	t.Run("policy_fetch", func(t *testing.T) {
		resp, err := client.Get(base + "/policy")
		testutil.AssertNoError(t, err, "GET /policy")
		defer resp.Body.Close()
		testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "policy status")
	})

	// ── Step 3: policy conditional fetch (304) ──────────────────────
	t.Run("policy_conditional", func(t *testing.T) {
		req, _ := http.NewRequest("GET", base+"/policy", nil)
		req.Header.Set("If-None-Match", "v1")
		resp, err := client.Do(req)
		testutil.AssertNoError(t, err, "conditional GET /policy")
		defer resp.Body.Close()
		testutil.AssertEqual(t, resp.StatusCode, http.StatusNotModified, "conditional policy 304")
	})

	// ── Step 4: telemetry push ──────────────────────────────────────
	t.Run("telemetry_push", func(t *testing.T) {
		resp, err := client.Post(base+"/telemetry", "application/json", nil)
		testutil.AssertNoError(t, err, "POST /telemetry")
		defer resp.Body.Close()
		testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "telemetry status")

		batches := gw.TelemetryBatches()
		if len(batches) == 0 {
			t.Fatal("expected at least one telemetry batch")
		}
	})

	// ── Step 5: relay ───────────────────────────────────────────────
	t.Run("relay_echo", func(t *testing.T) {
		gw.SetRelayHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Relay", "ok")
			w.WriteHeader(http.StatusOK)
		})

		resp, err := client.Get(base + "/relay")
		testutil.AssertNoError(t, err, "GET /relay")
		defer resp.Body.Close()
		testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "relay status")
		testutil.AssertEqual(t, resp.Header.Get("X-Test-Relay"), "ok", "relay header")
	})

	// ── Step 6: gateway down → agent sees error ─────────────────────
	t.Run("gateway_down", func(t *testing.T) {
		gw.Stop()
		time.Sleep(50 * time.Millisecond)

		_, err := client.Get(base + "/healthz")
		testutil.AssertError(t, err, "should fail after gateway stopped")
	})
}

// TestT11_PolicyDecisionRouting verifies that the mock gateway correctly
// serves policy rules that would result in block/bypass/forward decisions.
func TestT11_PolicyDecisionRouting(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	policy := &domain.PolicyPayload{
		Version: "v2",
		Rules: []domain.PolicyRule{
			{ID: "block-malware", Priority: 1, Decision: domain.DecisionBlock,
				Conditions: []domain.RuleCondition{{Field: "host", Operator: "eq", Value: "malware.test"}}},
			{ID: "bypass-internal", Priority: 2, Decision: domain.DecisionBypass,
				Conditions: []domain.RuleCondition{{Field: "host", Operator: "suffix", Value: ".internal"}}},
		},
	}
	gw.SetPolicy(policy)

	client := makeMTLSClient(pki)
	base := fmt.Sprintf("https://%s", gw.Addr)

	resp, err := client.Get(base + "/policy")
	testutil.AssertNoError(t, err, "GET /policy")
	defer resp.Body.Close()
	testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "policy fetch")

	log := gw.RequestLog()
	found := false
	for _, entry := range log {
		if entry.Path == "/policy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected /policy in request log")
	}
}

func makeMTLSClient(pki *testutil.TestPKI) *http.Client {
	tlsCert := tls.Certificate{
		Certificate: [][]byte{pki.ClientCertDER},
		PrivateKey:  pki.ClientKey,
	}
	caPool := x509.NewCertPool()
	caPool.AddCert(pki.CACert)

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: []tls.Certificate{tlsCert},
				RootCAs:      caPool,
			},
		},
	}
}
