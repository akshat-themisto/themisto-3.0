// Package testutil provides shared helpers for integration tests.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"time"
)

// TestPKI holds all certificates and keys needed for integration testing.
type TestPKI struct {
	// Gateway CA — signs server and client certs.
	CACert    *x509.Certificate
	CAKey     *ecdsa.PrivateKey
	CACertDER []byte

	// Gateway server cert — presented by the mock gateway.
	ServerCert    *x509.Certificate
	ServerKey     *ecdsa.PrivateKey
	ServerCertDER []byte

	// Valid agent client cert — for successful mTLS tests.
	ClientCert    *x509.Certificate
	ClientKey     *ecdsa.PrivateKey
	ClientCertDER []byte
	ClientKeyDER  []byte

	// Expired client cert — for T1.2.
	ExpiredCert    *x509.Certificate
	ExpiredKey     *ecdsa.PrivateKey
	ExpiredCertDER []byte
	ExpiredKeyDER  []byte

	// Rogue CA — NOT in the trust chain.
	RogueCACert    *x509.Certificate
	RogueCAKey     *ecdsa.PrivateKey
	RogueCACertDER []byte

	// Client cert signed by Rogue CA — for T1.3.
	RogueClientCert    *x509.Certificate
	RogueClientKey     *ecdsa.PrivateKey
	RogueClientCertDER []byte
	RogueClientKeyDER  []byte

	// Server cert signed by Rogue CA — for T1.5.
	RogueServerCert    *x509.Certificate
	RogueServerKey     *ecdsa.PrivateKey
	RogueServerCertDER []byte
}

// GeneratePKI creates the full test certificate infrastructure.
func GeneratePKI() (*TestPKI, error) {
	pki := &TestPKI{}

	// ── Gateway CA ───────────────────────────────────────────────────
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Themisto Test CA", Organization: []string{"Themisto"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	caCert, _ := x509.ParseCertificate(caDER)
	pki.CACert = caCert
	pki.CAKey = caKey
	pki.CACertDER = caDER

	// ── Gateway server cert ──────────────────────────────────────────
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serverDER, err := createSignedCert(&x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gateway.themisto.test"},
		DNSNames:     []string{"gateway.themisto.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &serverKey.PublicKey, caCert, caKey)
	if err != nil {
		return nil, err
	}
	pki.ServerCert, _ = x509.ParseCertificate(serverDER)
	pki.ServerKey = serverKey
	pki.ServerCertDER = serverDER

	// ── Valid client cert ────────────────────────────────────────────
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	clientDER, err := createSignedCert(&x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-agent"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, &clientKey.PublicKey, caCert, caKey)
	if err != nil {
		return nil, err
	}
	pki.ClientCert, _ = x509.ParseCertificate(clientDER)
	pki.ClientKey = clientKey
	pki.ClientCertDER = clientDER
	pki.ClientKeyDER, _ = x509.MarshalECPrivateKey(clientKey)

	// ── Expired client cert ─────────────────────────────────────────
	expKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	expDER, err := createSignedCert(&x509.Certificate{
		SerialNumber: big.NewInt(4),
		Subject:      pkix.Name{CommonName: "expired-agent"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-1 * time.Hour), // already expired
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, &expKey.PublicKey, caCert, caKey)
	if err != nil {
		return nil, err
	}
	pki.ExpiredCert, _ = x509.ParseCertificate(expDER)
	pki.ExpiredKey = expKey
	pki.ExpiredCertDER = expDER
	pki.ExpiredKeyDER, _ = x509.MarshalECPrivateKey(expKey)

	// ── Rogue CA ─────────────────────────────────────────────────────
	rogueCAKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rogueCATmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "Rogue CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rogueCaDER, err := x509.CreateCertificate(rand.Reader, rogueCATmpl, rogueCATmpl, &rogueCAKey.PublicKey, rogueCAKey)
	if err != nil {
		return nil, err
	}
	pki.RogueCACert, _ = x509.ParseCertificate(rogueCaDER)
	pki.RogueCAKey = rogueCAKey
	pki.RogueCACertDER = rogueCaDER

	// ── Rogue client cert (signed by Rogue CA) ──────────────────────
	rogueClientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rogueClientDER, _ := createSignedCert(&x509.Certificate{
		SerialNumber: big.NewInt(101),
		Subject:      pkix.Name{CommonName: "rogue-agent"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, &rogueClientKey.PublicKey, pki.RogueCACert, rogueCAKey)
	pki.RogueClientCert, _ = x509.ParseCertificate(rogueClientDER)
	pki.RogueClientKey = rogueClientKey
	pki.RogueClientCertDER = rogueClientDER
	pki.RogueClientKeyDER, _ = x509.MarshalECPrivateKey(rogueClientKey)

	// ── Rogue server cert (signed by Rogue CA) ──────────────────────
	rogueServerKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rogueServerDER, _ := createSignedCert(&x509.Certificate{
		SerialNumber: big.NewInt(102),
		Subject:      pkix.Name{CommonName: "gateway.themisto.test"},
		DNSNames:     []string{"gateway.themisto.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &rogueServerKey.PublicKey, pki.RogueCACert, rogueCAKey)
	pki.RogueServerCert, _ = x509.ParseCertificate(rogueServerDER)
	pki.RogueServerKey = rogueServerKey
	pki.RogueServerCertDER = rogueServerDER

	return pki, nil
}

func createSignedCert(tmpl *x509.Certificate, pub interface{}, parent *x509.Certificate, signerKey *ecdsa.PrivateKey) ([]byte, error) {
	return x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signerKey)
}
