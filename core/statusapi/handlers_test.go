package statusapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestValidateCAChainPEM(t *testing.T) {
	valid := makeTestCertPEM(t)

	if err := validateCAChainPEM(valid); err != nil {
		t.Fatalf("validateCAChainPEM(valid) error = %v", err)
	}

	if err := validateCAChainPEM([]byte("not pem")); err == nil {
		t.Fatal("validateCAChainPEM(invalid) = nil, want error")
	}
}

func makeTestCertPEM(t *testing.T) []byte {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestDeriveRuntimePhase(t *testing.T) {
	tests := []struct {
		name               string
		uptime             time.Duration
		gatewayConnected   bool
		proxyListenerReady bool
		enrolled           bool
		wantGatewayState   string
		wantProxyState     string
		wantRuntimePhase   string
	}{
		{
			name:               "waiting for enrollment",
			uptime:             5 * time.Minute,
			gatewayConnected:   false,
			proxyListenerReady: false,
			enrolled:           false,
			wantGatewayState:   "not_enrolled",
			wantProxyState:     "waiting_for_enrollment",
			wantRuntimePhase:   "needs_enrollment",
		},
		{
			name:               "fully ready",
			uptime:             5 * time.Minute,
			gatewayConnected:   true,
			proxyListenerReady: true,
			enrolled:           true,
			wantGatewayState:   "connected",
			wantProxyState:     "running",
			wantRuntimePhase:   "ready",
		},
		{
			name:               "proxy still starting",
			uptime:             10 * time.Second,
			gatewayConnected:   false,
			proxyListenerReady: false,
			enrolled:           true,
			wantGatewayState:   "starting",
			wantProxyState:     "starting",
			wantRuntimePhase:   "starting",
		},
		{
			name:               "gateway retrying after boot",
			uptime:             90 * time.Second,
			gatewayConnected:   false,
			proxyListenerReady: true,
			enrolled:           true,
			wantGatewayState:   "retrying",
			wantProxyState:     "running",
			wantRuntimePhase:   "retrying_gateway",
		},
		{
			name:               "long lived gateway outage",
			uptime:             5 * time.Minute,
			gatewayConnected:   false,
			proxyListenerReady: true,
			enrolled:           true,
			wantGatewayState:   "offline",
			wantProxyState:     "running",
			wantRuntimePhase:   "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGatewayState, gotProxyState, gotRuntimePhase := deriveRuntimePhase(tt.uptime, tt.gatewayConnected, tt.proxyListenerReady, tt.enrolled)
			if gotGatewayState != tt.wantGatewayState {
				t.Fatalf("gatewayState = %q, want %q", gotGatewayState, tt.wantGatewayState)
			}
			if gotProxyState != tt.wantProxyState {
				t.Fatalf("proxyState = %q, want %q", gotProxyState, tt.wantProxyState)
			}
			if gotRuntimePhase != tt.wantRuntimePhase {
				t.Fatalf("runtimePhase = %q, want %q", gotRuntimePhase, tt.wantRuntimePhase)
			}
		})
	}
}

func TestProbeLoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	if !probeLoopbackListener(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond) {
		t.Fatal("expected active listener to be detected")
	}

	if probeLoopbackListener("127.0.0.1:65001", 50*time.Millisecond) {
		t.Fatal("expected closed port to return false")
	}
}
