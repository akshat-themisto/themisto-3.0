package integration

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"

	"github.com/themisto/agent/test/integration/testutil"
)

// T1.1 — Successful mTLS connection.
func TestT1_1_SuccessfulMTLS(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	client := mtlsClient(pki.CACertDER, pki.ClientCertDER, pki.ClientKey, pki.CACert)
	resp, err := client.Get("https://" + gw.Addr + "/healthz")
	testutil.AssertNoError(t, err, "GET /healthz")
	defer resp.Body.Close()

	testutil.AssertEqual(t, resp.StatusCode, http.StatusOK, "healthz status")
	testutil.AssertEqual(t, resp.TLS.Version, uint16(tls.VersionTLS13), "TLS 1.3 negotiated")
}

// T1.2 — Agent with expired client certificate.
func TestT1_2_ExpiredClientCert(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	client := mtlsClient(pki.CACertDER, pki.ExpiredCertDER, pki.ExpiredKey, pki.CACert)
	_, err = client.Get("https://" + gw.Addr + "/healthz")
	testutil.AssertError(t, err, "expired cert should be rejected")
	testutil.AssertContains(t, err.Error(), "certificate", "error mentions certificate")
}

// T1.3 — Agent with wrong CA (rogue CA).
func TestT1_3_WrongCA(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	// Client cert signed by Rogue CA — gateway should reject.
	client := mtlsClient(pki.CACertDER, pki.RogueClientCertDER, pki.RogueClientKey, pki.CACert)
	_, err = client.Get("https://" + gw.Addr + "/healthz")
	testutil.AssertError(t, err, "rogue client cert should be rejected")
}

// T1.4 — Agent without client certificate.
func TestT1_4_NoClientCert(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	gw, err := testutil.NewMockGateway(pki)
	testutil.AssertNoError(t, err, "create mock gateway")
	gw.Start()
	defer gw.Stop()

	// Client with CA trust but no client certificate.
	caPool := x509.NewCertPool()
	caPool.AddCert(pki.CACert)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    caPool,
			},
		},
	}
	_, err = client.Get("https://" + gw.Addr + "/healthz")
	testutil.AssertError(t, err, "no client cert should be rejected")
}

// T1.5 — Gateway with wrong server certificate.
func TestT1_5_WrongServerCert(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	// Gateway uses rogue server cert.
	gw, err := testutil.NewMockGatewayWithRogueServerCert(pki)
	testutil.AssertNoError(t, err, "create rogue gateway")
	gw.Start()
	defer gw.Stop()

	// Agent trusts only the real CA, not the rogue CA.
	client := mtlsClient(pki.CACertDER, pki.ClientCertDER, pki.ClientKey, pki.CACert)
	_, err = client.Get("https://" + gw.Addr + "/healthz")
	testutil.AssertError(t, err, "rogue server cert should be rejected")
	testutil.AssertContains(t, err.Error(), "certificate", "error mentions certificate")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mtlsClient(caCertDER, clientCertDER []byte, clientKey interface{}, caCert *x509.Certificate) *http.Client {
	tlsCert := tls.Certificate{
		Certificate: [][]byte{clientCertDER},
		PrivateKey:  clientKey,
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

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

func gatewayURL(addr string) string {
	return fmt.Sprintf("https://%s", addr)
}
