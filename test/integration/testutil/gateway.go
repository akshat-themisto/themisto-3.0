package testutil

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/themisto/agent/core/domain"
)

// MockGateway is a configurable mTLS gateway server for integration testing.
type MockGateway struct {
	Server   *http.Server
	Listener net.Listener
	Addr     string

	mu              sync.Mutex
	policy          *domain.PolicyPayload
	telemetryBatches []json.RawMessage
	relayHandler    func(w http.ResponseWriter, r *http.Request)
	requestLog      []RequestLogEntry
	healthStatus    int
	stopped         atomic.Bool
}

// RequestLogEntry records metadata about a request received by the mock gateway.
type RequestLogEntry struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

// NewMockGateway creates a mock gateway with mTLS enforcement using the given PKI.
func NewMockGateway(pki *TestPKI) (*MockGateway, error) {
	serverCert := tls.Certificate{
		Certificate: [][]byte{pki.ServerCertDER},
		PrivateKey:  pki.ServerKey,
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(pki.CACert)

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	gw := &MockGateway{
		healthStatus: http.StatusOK,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", gw.handleHealthz)
	mux.HandleFunc("/policy", gw.handlePolicy)
	mux.HandleFunc("/telemetry", gw.handleTelemetry)
	mux.HandleFunc("/relay", gw.handleRelay)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	gw.Listener = ln
	gw.Addr = ln.Addr().String()
	gw.Server = &http.Server{Handler: mux}
	return gw, nil
}

// NewMockGatewayWithRogueServerCert creates a gateway using a rogue server cert (for T1.5).
func NewMockGatewayWithRogueServerCert(pki *TestPKI) (*MockGateway, error) {
	serverCert := tls.Certificate{
		Certificate: [][]byte{pki.RogueServerCertDER},
		PrivateKey:  pki.RogueServerKey,
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(pki.CACert)

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	gw := &MockGateway{healthStatus: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", gw.handleHealthz)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		return nil, err
	}
	gw.Listener = ln
	gw.Addr = ln.Addr().String()
	gw.Server = &http.Server{Handler: mux}
	return gw, nil
}

// Start begins serving in a background goroutine.
func (gw *MockGateway) Start() {
	go gw.Server.Serve(gw.Listener)
}

// Stop shuts down the gateway.
func (gw *MockGateway) Stop() {
	gw.stopped.Store(true)
	gw.Server.Close()
}

// IsStopped reports whether the gateway has been stopped.
func (gw *MockGateway) IsStopped() bool {
	return gw.stopped.Load()
}

// SetPolicy sets the policy payload returned by /policy.
func (gw *MockGateway) SetPolicy(p *domain.PolicyPayload) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.policy = p
}

// SetHealthStatus sets the HTTP status returned by /healthz.
func (gw *MockGateway) SetHealthStatus(code int) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.healthStatus = code
}

// SetRelayHandler sets a custom handler for /relay requests.
func (gw *MockGateway) SetRelayHandler(h func(w http.ResponseWriter, r *http.Request)) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.relayHandler = h
}

// RequestLog returns all recorded request log entries.
func (gw *MockGateway) RequestLog() []RequestLogEntry {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	out := make([]RequestLogEntry, len(gw.requestLog))
	copy(out, gw.requestLog)
	return out
}

// TelemetryBatches returns all telemetry payloads received.
func (gw *MockGateway) TelemetryBatches() []json.RawMessage {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	out := make([]json.RawMessage, len(gw.telemetryBatches))
	copy(out, gw.telemetryBatches)
	return out
}

// ---------------------------------------------------------------------------
// handlers
// ---------------------------------------------------------------------------

func (gw *MockGateway) handleHealthz(w http.ResponseWriter, r *http.Request) {
	gw.logRequest(r)
	gw.mu.Lock()
	status := gw.healthStatus
	gw.mu.Unlock()
	w.WriteHeader(status)
	fmt.Fprint(w, "ok")
}

func (gw *MockGateway) handlePolicy(w http.ResponseWriter, r *http.Request) {
	gw.logRequest(r)
	gw.mu.Lock()
	p := gw.policy
	gw.mu.Unlock()

	if p == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Support conditional fetch.
	if etag := r.Header.Get("If-None-Match"); etag != "" && etag == p.Version {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func (gw *MockGateway) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	gw.logRequest(r)
	body, _ := io.ReadAll(r.Body)
	gw.mu.Lock()
	gw.telemetryBatches = append(gw.telemetryBatches, json.RawMessage(body))
	gw.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (gw *MockGateway) handleRelay(w http.ResponseWriter, r *http.Request) {
	gw.logRequest(r)
	gw.mu.Lock()
	h := gw.relayHandler
	gw.mu.Unlock()

	if h != nil {
		h(w, r)
		return
	}

	// Default: echo back request info.
	w.Header().Set("X-Themisto-Relayed", "true")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "relayed: %s %s", r.Method, r.Header.Get("X-Themisto-Original-URL"))
}

func (gw *MockGateway) logRequest(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	gw.mu.Lock()
	gw.requestLog = append(gw.requestLog, RequestLogEntry{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	})
	gw.mu.Unlock()
}
