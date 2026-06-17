package integration

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/themisto/agent/core/identity"
	"github.com/themisto/agent/test/integration/testutil"
)

// T7.1 — TLS config can be built from valid DER credentials.
func TestT7_1_BuildTLSConfig(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	clientKeyDER, err := marshalECKey(pki.ClientKey)
	testutil.AssertNoError(t, err, "marshal client key")

	tlsCfg, err := identity.BuildTLSConfig(domainTLS(
		pki.ClientCertDER, clientKeyDER, pki.CACertDER,
	))
	testutil.AssertNoError(t, err, "build TLS config")

	testutil.AssertTrue(t, len(tlsCfg.Certificates) == 1, "one client cert")
	testutil.AssertTrue(t, tlsCfg.RootCAs != nil, "root CAs pool set")
	testutil.AssertEqual(t, tlsCfg.MinVersion, uint16(tls.VersionTLS13), "TLS 1.3")
}

// T7.1 negative — invalid DER fails.
func TestT7_1_InvalidDERFails(t *testing.T) {
	_, err := identity.BuildTLSConfig(domainTLS(
		[]byte{0xFF, 0xFF}, []byte{0xFF}, []byte{0xFF},
	))
	testutil.AssertError(t, err, "invalid DER should fail")
}

// T7.1 negative — nil config fails.
func TestT7_1_NilConfigFails(t *testing.T) {
	_, err := identity.BuildTLSConfig(nil)
	testutil.AssertError(t, err, "nil config should fail")
}

// T7.2 — Credential rotation in IdentityStore.
func TestT7_2_CredentialRotation(t *testing.T) {
	pki, err := testutil.GeneratePKI()
	testutil.AssertNoError(t, err, "generate PKI")

	store := identity.NewIdentityStore(&mockCertStore{}, &nopLogger{})

	clientKeyDER, _ := marshalECKey(pki.ClientKey)

	// Bootstrap initial credentials.
	store.Bootstrap("ca-1", "client-1", pki.ClientCertDER, clientKeyDER, pki.CACertDER)

	cfg1, err := store.GetGatewayTLSConfig()
	testutil.AssertNoError(t, err, "get TLS config v1")
	testutil.AssertTrue(t, len(cfg1.CertDER) > 0, "v1 cert present")

	// Rotate to new credentials.
	err = store.RotateCredentials("ca-2", "client-2", pki.ClientCertDER, clientKeyDER, pki.CACertDER)
	testutil.AssertNoError(t, err, "rotate credentials")

	cfg2, err := store.GetGatewayTLSConfig()
	testutil.AssertNoError(t, err, "get TLS config v2")
	testutil.AssertTrue(t, len(cfg2.CertDER) > 0, "v2 cert present")

	// Clear removes all.
	err = store.Clear()
	testutil.AssertNoError(t, err, "clear credentials")

	_, err = store.GetGatewayTLSConfig()
	testutil.AssertError(t, err, "no creds after clear")
}

// ---------------------------------------------------------------------------
// mockCertStore — satisfies iface.CertStore for unit tests
// ---------------------------------------------------------------------------

type mockCertStore struct {
	installed map[string][]byte
}

func (m *mockCertStore) InstallCA(_ context.Context, id string, der []byte) error {
	if m.installed == nil {
		m.installed = make(map[string][]byte)
	}
	m.installed[id] = der
	return nil
}

func (m *mockCertStore) RemoveCA(_ context.Context, id string) error {
	if m.installed != nil {
		delete(m.installed, id)
	}
	return nil
}

func (m *mockCertStore) HasCA(id string) (bool, error) {
	if m.installed == nil {
		return false, nil
	}
	_, ok := m.installed[id]
	return ok, nil
}
