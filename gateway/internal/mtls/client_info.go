package mtls

import (
	"context"
	"crypto/x509"
	"strings"
)

type contextKey int

const (
	deviceIDKey contextKey = iota
	orgIDKey
	certSerialKey
)

func DeviceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(deviceIDKey).(string)
	return v
}

func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(orgIDKey).(string)
	return v
}

func CertSerialFromContext(ctx context.Context) string {
	v, _ := ctx.Value(certSerialKey).(string)
	return v
}

func ContextWithClientInfo(ctx context.Context, cert *x509.Certificate) context.Context {
	cn := cert.Subject.CommonName
	org := ""
	if len(cert.Subject.Organization) > 0 {
		candidate := strings.TrimSpace(cert.Subject.Organization[0])
		if isUUID(candidate) {
			org = candidate
		}
	}
	serial := cert.SerialNumber.Text(16)

	ctx = context.WithValue(ctx, deviceIDKey, cn)
	ctx = context.WithValue(ctx, orgIDKey, org)
	ctx = context.WithValue(ctx, certSerialKey, serial)
	return ctx
}

func ContextWithVerifiedIdentity(ctx context.Context, deviceID, orgID string) context.Context {
	if deviceID != "" {
		ctx = context.WithValue(ctx, deviceIDKey, deviceID)
	}
	if orgID != "" {
		ctx = context.WithValue(ctx, orgIDKey, orgID)
	}
	return ctx
}

func isUUID(raw string) bool {
	parts := strings.Split(raw, "-")
	if len(parts) != 5 {
		return false
	}
	expected := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expected[i] {
			return false
		}
		for _, c := range part {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}
