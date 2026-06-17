package mtls

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
)

func TestContextWithClientInfo_IgnoresOrgNameFallback(t *testing.T) {
	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:   "device-1",
			Organization: []string{"Themisto Dev Org"},
		},
		SerialNumber: big.NewInt(42),
	}

	ctx := ContextWithClientInfo(context.Background(), cert)

	if got := DeviceIDFromContext(ctx); got != "device-1" {
		t.Fatalf("device id = %q, want device-1", got)
	}
	if got := OrgIDFromContext(ctx); got != "" {
		t.Fatalf("org id = %q, want empty when cert only carries org name", got)
	}
	if got := CertSerialFromContext(ctx); got == "" {
		t.Fatal("expected serial in context")
	}
}

func TestContextWithClientInfo_PreservesUUIDOrgID(t *testing.T) {
	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:   "device-1",
			Organization: []string{"a0000000-0000-0000-0000-000000000001"},
		},
		SerialNumber: big.NewInt(42),
	}

	ctx := ContextWithClientInfo(context.Background(), cert)

	if got := OrgIDFromContext(ctx); got != "a0000000-0000-0000-0000-000000000001" {
		t.Fatalf("org id = %q, want UUID org id", got)
	}
}
