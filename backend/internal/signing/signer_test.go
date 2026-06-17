package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	os.WriteFile(certPath, certPEM, 0600)
	os.WriteFile(keyPath, keyPEM, 0600)
	os.WriteFile(chainPath, certPEM, 0600)
	return
}

func generateTestCSR(t *testing.T, cn, org string) []byte {
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
	return csrDER
}

func TestNewSigner(t *testing.T) {
	certPath, keyPath, chainPath := generateTestCA(t)
	s, err := NewSigner(certPath, keyPath, chainPath, 365)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if s.CACert == nil {
		t.Fatal("CACert is nil")
	}
	if s.CAKey == nil {
		t.Fatal("CAKey is nil")
	}
	if s.ValidityDays != 365 {
		t.Errorf("ValidityDays = %d, want 365", s.ValidityDays)
	}
}

func TestNewSigner_MissingFiles(t *testing.T) {
	_, err := NewSigner("/nonexistent/cert", "/nonexistent/key", "/nonexistent/chain", 365)
	if err == nil {
		t.Fatal("expected error for missing files")
	}
}

func TestSignCSR(t *testing.T) {
	certPath, keyPath, chainPath := generateTestCA(t)
	s, err := NewSigner(certPath, keyPath, chainPath, 30)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	csrDER := generateTestCSR(t, "device-1", "TestOrg")
	result, err := s.SignCSR(csrDER)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	if result.Serial == "" {
		t.Error("serial is empty")
	}
	if result.CertPEM == "" {
		t.Error("cert PEM is empty")
	}
	if result.NotBefore.IsZero() || result.NotAfter.IsZero() {
		t.Error("validity times are zero")
	}
	if result.NotAfter.Before(result.NotBefore) {
		t.Error("NotAfter before NotBefore")
	}

	block, _ := pem.Decode([]byte(result.CertPEM))
	if block == nil {
		t.Fatal("could not decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse signed cert: %v", err)
	}
	if cert.Subject.CommonName != "device-1" {
		t.Errorf("CN = %q, want device-1", cert.Subject.CommonName)
	}
	if len(cert.ExtKeyUsage) == 0 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Error("expected ClientAuth ext key usage")
	}
}

func TestSignCSR_InvalidDER(t *testing.T) {
	certPath, keyPath, chainPath := generateTestCA(t)
	s, _ := NewSigner(certPath, keyPath, chainPath, 30)

	_, err := s.SignCSR([]byte("not a CSR"))
	if err == nil {
		t.Fatal("expected error for invalid CSR DER")
	}
}
