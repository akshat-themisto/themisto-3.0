package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/identity"
	"github.com/themisto/agent/pkg/dial"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/retry"
)

// MtlsGatewayClient implements GatewayClient. It maintains a persistent HTTP
// client with mTLS for all communication with the gateway.
type MtlsGatewayClient struct {
	identityStore *identity.IdentityStore
	log           log.Logger
	gatewayURL    string

	client  atomic.Value // *http.Client
	healthy atomic.Value // bool

	cb *retry.CircuitBreaker
}

// NewGatewayClient creates a gateway client. Call Init before first use.
func NewGatewayClient(store *identity.IdentityStore, gatewayURL string, cbThreshold int, cbCooldown time.Duration, logger log.Logger) *MtlsGatewayClient {
	g := &MtlsGatewayClient{
		identityStore: store,
		gatewayURL:    gatewayURL,
		log:           logger,
		cb:            retry.NewCircuitBreaker(cbThreshold, cbCooldown),
	}
	g.healthy.Store(false)
	return g
}

// Init builds the mTLS transport and performs an initial health check.
func (g *MtlsGatewayClient) Init(ctx context.Context) error {
	if err := g.rebuildTransport(); err != nil {
		return fmt.Errorf("gateway init: %w", err)
	}
	return g.healthCheck(ctx)
}

// Relay forwards a proxied request to the gateway and returns the response.
func (g *MtlsGatewayClient) Relay(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error) {
	if !g.cb.Allow() {
		return nil, fmt.Errorf("gateway: circuit open")
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, g.gatewayURL+"/relay", req.Body)
	if err != nil {
		return nil, fmt.Errorf("gateway relay: build request: %w", err)
	}
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	httpReq.Header.Set("X-Themisto-Original-URL", req.URL)
	httpReq.Header.Set("X-Themisto-Original-Host", req.Host)
	httpReq.Header.Set("X-Themisto-Original-Method", req.Method)

	client := g.httpClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		g.cb.RecordFailure()
		return nil, fmt.Errorf("gateway relay: %w", err)
	}
	g.cb.RecordSuccess()

	headers := make(map[string][]string)
	for k, vs := range resp.Header {
		headers[k] = vs
	}

	return &domain.ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       resp.Body,
	}, nil
}

// Do performs a control-plane HTTP request to the gateway.
func (g *MtlsGatewayClient) Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error) {
	if !g.cb.Allow() {
		return nil, fmt.Errorf("gateway: circuit open")
	}

	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("gateway do: build request: %w", err)
	}
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	client := g.httpClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		g.cb.RecordFailure()
		return nil, fmt.Errorf("gateway do: %w", err)
	}
	defer resp.Body.Close()
	g.cb.RecordSuccess()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gateway do: read response: %w", err)
	}

	headers := make(map[string][]string)
	for k, vs := range resp.Header {
		headers[k] = vs
	}

	return &domain.HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       respBody,
	}, nil
}

// Close drains connections and releases the transport.
func (g *MtlsGatewayClient) Close() error {
	if c := g.httpClient(); c != nil {
		c.CloseIdleConnections()
	}
	return nil
}

// Healthy reports whether the gateway passed its last health check.
func (g *MtlsGatewayClient) Healthy() bool {
	v, ok := g.healthy.Load().(bool)
	return ok && v
}

// RunHealthCheck runs periodic /healthz probes until ctx is cancelled.
func (g *MtlsGatewayClient) RunHealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.healthCheck(ctx); err != nil {
				g.log.Warn("gateway health check failed", "error", err)
			}
		}
	}
}

// RefreshTransport rebuilds the HTTP transport from the current identity
// credentials. Call after credential rotation.
func (g *MtlsGatewayClient) RefreshTransport() error {
	return g.rebuildTransport()
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func (g *MtlsGatewayClient) httpClient() *http.Client {
	if c, ok := g.client.Load().(*http.Client); ok {
		return c
	}
	return http.DefaultClient
}

func (g *MtlsGatewayClient) rebuildTransport() error {
	tlsCfg, err := g.identityStore.GetGatewayTLSConfig()
	if err != nil {
		return err
	}

	goTLS, err := identity.BuildTLSConfig(tlsCfg)
	if err != nil {
		return err
	}

	transport := &http.Transport{
		TLSClientConfig:     goTLS,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
		DialContext: dial.ContextDialer(&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
	}

	g.client.Store(&http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	})
	return nil
}

func (g *MtlsGatewayClient) healthCheck(ctx context.Context) error {
	resp, err := g.Do(ctx, &domain.HTTPRequest{
		Method: "GET",
		URL:    g.gatewayURL + "/healthz",
	})
	if err != nil {
		g.healthy.Store(false)
		return err
	}
	if resp.StatusCode >= 400 {
		g.healthy.Store(false)
		return fmt.Errorf("gateway /healthz returned %d", resp.StatusCode)
	}
	g.healthy.Store(true)
	return nil
}

// DialTunnel establishes a raw mTLS TCP connection to the gateway. Used by
// the proxy handler for CONNECT tunnel forwarding.
func (g *MtlsGatewayClient) DialTunnel(ctx context.Context) (net.Conn, error) {
	tlsCfg, err := g.identityStore.GetGatewayTLSConfig()
	if err != nil {
		return nil, err
	}
	goTLS, err := identity.BuildTLSConfig(tlsCfg)
	if err != nil {
		return nil, err
	}
	serverName := hostNameFromURL(g.gatewayURL)
	if serverName == "" {
		return nil, fmt.Errorf("gateway dial: cannot determine server name from gateway url %q", g.gatewayURL)
	}
	goTLS = goTLS.Clone()
	if goTLS.ServerName == "" {
		goTLS.ServerName = serverName
	}

	d := &net.Dialer{Timeout: 10 * time.Second}
	rawConn, err := dial.TCP(ctx, hostFromURL(g.gatewayURL), d)
	if err != nil {
		return nil, fmt.Errorf("gateway dial: %w", err)
	}

	tlsConn := tls.Client(rawConn, goTLS)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("gateway tls handshake: %w", err)
	}
	return tlsConn, nil
}

func hostFromURL(rawURL string) string {
	// Strip scheme.
	s := rawURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
			break
		}
	}
	// Strip path.
	if idx := indexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	// Add default port if missing.
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = s + ":443"
	}
	return s
}

func hostNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil {
		if h := u.Hostname(); h != "" {
			return h
		}
	}

	// Fallback for raw host:port or host strings without scheme.
	s := rawURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
			break
		}
	}
	if idx := indexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
