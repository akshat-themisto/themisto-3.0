package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
)

func makeCSRPEM(t *testing.T, cn, org string, curve elliptic.Curve) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
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

func TestValidateCSR_Valid_P256(t *testing.T) {
	csrPEM := makeCSRPEM(t, "device-1", "TestOrg", elliptic.P256())
	der, err := ValidateCSR(csrPEM, "device-1", "TestOrg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(der) == 0 {
		t.Error("expected non-empty DER output")
	}
}

func TestValidateCSR_Valid_P384(t *testing.T) {
	csrPEM := makeCSRPEM(t, "device-2", "OrgX", elliptic.P384())
	_, err := ValidateCSR(csrPEM, "device-2", "OrgX")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCSR_CNMismatch(t *testing.T) {
	csrPEM := makeCSRPEM(t, "device-1", "TestOrg", elliptic.P256())
	_, err := ValidateCSR(csrPEM, "wrong-cn", "TestOrg")
	if err == nil {
		t.Fatal("expected CN mismatch error")
	}
	csrErr, ok := err.(*CSRValidationError)
	if !ok {
		t.Fatalf("expected CSRValidationError, got %T", err)
	}
	if csrErr.Code != "CSR_INVALID" {
		t.Errorf("code = %q, want CSR_INVALID", csrErr.Code)
	}
}

func TestValidateCSR_OrgMismatch(t *testing.T) {
	csrPEM := makeCSRPEM(t, "device-1", "TestOrg", elliptic.P256())
	_, err := ValidateCSR(csrPEM, "device-1", "WrongOrg")
	if err == nil {
		t.Fatal("expected org mismatch error")
	}
	csrErr := err.(*CSRValidationError)
	if csrErr.Code != "ORG_MISMATCH" {
		t.Errorf("code = %q, want ORG_MISMATCH", csrErr.Code)
	}
}

func TestValidateCSR_RSAKeyRejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "device-1",
			Organization: []string{"TestOrg"},
		},
	}
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	_, err = ValidateCSR(csrPEM, "device-1", "TestOrg")
	if err == nil {
		t.Fatal("expected RSA key rejection")
	}
	csrErr := err.(*CSRValidationError)
	if csrErr.Code != "KEY_ALGORITHM_REJECTED" {
		t.Errorf("code = %q, want KEY_ALGORITHM_REJECTED", csrErr.Code)
	}
}

func TestValidateCSR_NoPEM(t *testing.T) {
	_, err := ValidateCSR([]byte("not PEM"), "device-1", "TestOrg")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestValidateCSR_EmptyOrg(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "device-1"},
	}
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	_, err := ValidateCSR(csrPEM, "device-1", "TestOrg")
	if err == nil {
		t.Fatal("expected org mismatch error for empty org")
	}
}
