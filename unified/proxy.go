package unified

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/routing"
	"github.com/themisto/agent/core/transport"
	"github.com/themisto/agent/pkg/uid"
)

// DevProxy is a forward HTTP/HTTPS proxy that applies policy locally and
// forwards directly to upstream — no gateway in the middle.
//
// Request lifecycle:
//  1. Client connects to proxy (browser, curl, etc.)
//  2. Proxy receives HTTP request or CONNECT tunnel request
//  3. Build RequestContext (host, method, path)
//  4. Router evaluates policy → Forward / Block / Bypass
//  5a. Forward: attach wrapper headers, send to upstream, return response
//  5b. Block:   return 403 with block page
//  5c. Bypass:  send to upstream without wrapper headers
//  6. Log structured telemetry entry for every request
//
// TODO(gateway-split): In production, step 5a will relay through the
// remote gateway via mTLS (transport.MtlsGatewayClient.Relay) instead
// of connecting to upstream directly.
type DevProxy struct {
	router    routing.Router
	forwarder *DirectForwarder
	telem     *DevTelemetry
	cfg       *DevConfig

	policyVersion string
}

// NewDevProxy creates the development proxy handler.
func NewDevProxy(router routing.Router, fwd *DirectForwarder, telem *DevTelemetry, cfg *DevConfig, policyVersion string) *DevProxy {
	return &DevProxy{
		router:        router,
		forwarder:     fwd,
		telem:         telem,
		cfg:           cfg,
		policyVersion: policyVersion,
	}
}

// ServeHTTP dispatches each incoming request.
func (p *DevProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

// ---------------------------------------------------------------------------
// HTTP forward proxy
// ---------------------------------------------------------------------------

func (p *DevProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := uid.New()

	reqCtx := buildRequestContext(r, requestID)
	decision, ruleID, _ := p.router.Route(reqCtx)

	switch decision {
	case domain.DecisionBlock:
		p.telem.Counter("proxy.requests.blocked", 1, nil)
		p.logRequest(reqCtx, decision, ruleID, start, http.StatusForbidden)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, p.cfg.BlockPageBody)
		return

	case domain.DecisionBypass:
		p.telem.Counter("proxy.requests.bypassed", 1, nil)
		p.bypassHTTP(w, r, reqCtx, decision, ruleID, requestID, start)

	default: // Forward
		p.telem.Counter("proxy.requests.forwarded", 1, nil)
		p.forwardHTTP(w, r, reqCtx, decision, ruleID, requestID, start)
	}
	p.telem.Counter("proxy.requests.total", 1, nil)
}

func (p *DevProxy) forwardHTTP(w http.ResponseWriter, r *http.Request, reqCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID string, start time.Time) {
	// Attach wrapper headers.
	// TODO(identity): In production, ProcessInfo comes from
	// iface.ProcessResolver.ResolveByConnection. In dev mode we leave it empty.
	wrapHeaders := transport.BuildWrapperHeaders(
		"dev-agent",
		domain.ProcessInfo{},
		"", false,
		p.policyVersion,
		decision, "", "", ruleID, requestID,
	)

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	copyHeaders(outReq.Header, r.Header)
	for k, v := range wrapHeaders {
		outReq.Header.Set(k, v)
	}

	resp, err := p.forwarder.RoundTrip(outReq)
	if err != nil {
		p.logRequest(reqCtx, decision, ruleID, start, http.StatusBadGateway)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	p.logRequest(reqCtx, decision, ruleID, start, resp.StatusCode)
}

func (p *DevProxy) bypassHTTP(w http.ResponseWriter, r *http.Request, reqCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID string, start time.Time) {
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	copyHeaders(outReq.Header, r.Header)

	resp, err := p.forwarder.RoundTrip(outReq)
	if err != nil {
		p.logRequest(reqCtx, decision, ruleID, start, http.StatusBadGateway)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	p.logRequest(reqCtx, decision, ruleID, start, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// CONNECT tunnel
// ---------------------------------------------------------------------------

func (p *DevProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := uid.New()

	reqCtx := buildRequestContext(r, requestID)
	decision, ruleID, _ := p.router.Route(reqCtx)

	if decision == domain.DecisionBlock {
		p.telem.Counter("proxy.requests.blocked", 1, nil)
		p.logRequest(reqCtx, decision, ruleID, start, http.StatusForbidden)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// TODO(gateway-split): In production, Forward tunnels are relayed
	// through the gateway via MtlsGatewayClient.DialTunnel. In dev mode
	// we always connect directly regardless of Forward/Bypass.

	upstream, err := p.forwarder.DialUpstream(r.Host)
	if err != nil {
		p.logRequest(reqCtx, decision, ruleID, start, http.StatusBadGateway)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	client, _, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}

	p.telem.Counter("proxy.requests.total", 1, nil)
	if decision == domain.DecisionBypass {
		p.telem.Counter("proxy.requests.bypassed", 1, nil)
	} else {
		p.telem.Counter("proxy.requests.forwarded", 1, nil)
	}
	p.logRequest(reqCtx, decision, ruleID, start, http.StatusOK)

	copyBidirectional(client, upstream)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func buildRequestContext(r *http.Request, requestID string) *domain.RequestContext {
	host := r.Host
	var port uint16
	if h, p, err := net.SplitHostPort(r.Host); err == nil {
		host = h
		pn, _ := strconv.ParseUint(p, 10, 16)
		port = uint16(pn)
	}

	scheme := "http"
	if r.Method == http.MethodConnect || r.TLS != nil {
		scheme = "https"
	}

	return &domain.RequestContext{
		RequestID: requestID,
		Host:      host,
		Port:      port,
		Path:      r.URL.Path,
		Method:    r.Method,
		Scheme:    scheme,
		// TODO(identity): Populate Process field from OS adapter's
		// ProcessResolver.ResolveByConnection when running as a real agent.
	}
}

func (p *DevProxy) logRequest(ctx *domain.RequestContext, decision domain.Decision, ruleID string, start time.Time, status int) {
	p.telem.write("INFO", "request", map[string]interface{}{
		"request_id": ctx.RequestID,
		"host":       ctx.Host,
		"method":     ctx.Method,
		"scheme":     ctx.Scheme,
		"path":       ctx.Path,
		"decision":   decision.String(),
		"rule_id":    ruleID,
		"status":     status,
		"latency_ms": time.Since(start).Milliseconds(),
	})
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		lk := strings.ToLower(k)
		if lk == "proxy-connection" || lk == "proxy-authorization" {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
