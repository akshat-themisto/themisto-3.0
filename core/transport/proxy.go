package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/aiactivity"
	"github.com/themisto/agent/core/config"
	"github.com/themisto/agent/core/dlp"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/routing"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/pkg/dial"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/uid"
)

// HTTPProxy implements LocalProxy. It accepts HTTP and HTTPS CONNECT traffic
// from local applications, evaluates routing decisions, and either forwards
// via the gateway, bypasses directly, or blocks.
type HTTPProxy struct {
	cfg                config.Provider
	router             routing.Router
	gateway            *MtlsGatewayClient
	resolver           iface.ProcessResolver
	netInfo            iface.NetworkInfo
	certStore          iface.CertStore
	notifier           iface.UserNotifier
	metrics            *telemetry.Collector
	log                log.Logger
	policyVersionFn    func() string
	managedInterceptFn func() domain.PolicyInterception
	productCatalogFn   func() []domain.AIProductCatalogEntry

	server      *http.Server
	sem         chan struct{} // concurrency limiter
	interceptMu sync.Mutex
	interceptCA *interceptCertManager
	notifyMu    sync.Mutex
	notifyLast  map[string]time.Time
}

// ProxyDeps bundles the dependencies needed by the proxy handler.
type ProxyDeps struct {
	Config                config.Provider
	Router                routing.Router
	Gateway               *MtlsGatewayClient
	Resolver              iface.ProcessResolver
	NetInfo               iface.NetworkInfo
	CertStore             iface.CertStore
	Notifier              iface.UserNotifier
	Metrics               *telemetry.Collector
	Logger                log.Logger
	PolicyVersionFn       func() string
	ManagedInterceptionFn func() domain.PolicyInterception
	ProductCatalogFn      func() []domain.AIProductCatalogEntry
}

// NewHTTPProxy creates the local forward proxy.
func NewHTTPProxy(deps ProxyDeps) *HTTPProxy {
	agentCfg := deps.Config.Get()
	return &HTTPProxy{
		cfg:                deps.Config,
		router:             deps.Router,
		gateway:            deps.Gateway,
		resolver:           deps.Resolver,
		netInfo:            deps.NetInfo,
		certStore:          deps.CertStore,
		notifier:           deps.Notifier,
		metrics:            deps.Metrics,
		log:                deps.Logger,
		policyVersionFn:    deps.PolicyVersionFn,
		managedInterceptFn: deps.ManagedInterceptionFn,
		productCatalogFn:   deps.ProductCatalogFn,
		sem:                make(chan struct{}, agentCfg.MaxConcurrentConns),
		notifyLast:         make(map[string]time.Time),
	}
}

// Listen starts the proxy on addr. It blocks until Stop is called or ctx
// is cancelled.
func (p *HTTPProxy) Listen(ctx context.Context, addr string) error {
	p.server = &http.Server{
		Addr:              addr,
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("proxy listen: %w", err)
	}
	p.log.Info("proxy listening", "addr", addr)

	errCh := make(chan error, 1)
	go func() { errCh <- p.server.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), p.cfg.Get().ShutdownTimeout)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// Stop gracefully shuts down the proxy.
func (p *HTTPProxy) Stop() error {
	if p.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Get().ShutdownTimeout)
	defer cancel()
	return p.server.Shutdown(ctx)
}

// ServeHTTP dispatches incoming requests.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	default:
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

// ---------------------------------------------------------------------------
// HTTP forward proxy
// ---------------------------------------------------------------------------

func (p *HTTPProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := uid.New()
	agentCfg := p.cfg.Get()

	reqCtx := p.buildRequestContext(r, requestID)
	reqCtx.Protocol = detectRequestProtocol(r)

	p.classifyAIRequest(reqCtx, r.Header.Get("Origin"), r.Header.Get("Referer"))

	// Scan body before routing so body_* policy conditions can be evaluated.
	// This preserves the original body stream for downstream forwarding.
	p.inspectRequestBody(r, reqCtx, requestID)

	decision, ruleID, _ := p.router.Route(reqCtx)
	if shouldBypassInfrastructureProcess(reqCtx.Process.Name) {
		p.log.Debug("infrastructure process bypass applied", "request_id", requestID, "process", reqCtx.Process.Name, "host", reqCtx.Host)
		decision = domain.DecisionBypass
		ruleID = ""
	}
	matchedRule := p.lookupMatchedRule(ruleID)
	status := 0

	if len(reqCtx.DLP.Matches) > 0 {
		actionTaken := dlpActionForDecision(decision)
		reasonCode, reasonDetail := dlpReasonForDecision(decision, matchedRule)
		p.emitDLPTelemetry(reqCtx, reqCtx.DLP, requestID, actionTaken, ruleID, reasonCode, reasonDetail)
		p.notifyUserForDLP(reqCtx.Host, dlpCategory(reqCtx.DLP, reqCtx.ServiceCategory), decision, matchedRule)
	}

	p.logDecision(reqCtx, decision, ruleID, agentCfg.LogURLPaths)

	switch decision {
	case domain.DecisionBlock:
		p.metrics.Counter("proxy.requests.blocked", 1, nil)
		category := dlpCategory(reqCtx.DLP, reqCtx.ServiceCategory)
		reason := blockReasonForRule(matchedRule, category)
		p.writeBlockedResponse(w, agentCfg.BlockPageBody, category, reason)
		status = http.StatusForbidden

	case domain.DecisionBypass:
		p.metrics.Counter("proxy.requests.bypassed", 1, nil)
		status = p.bypassHTTP(w, r)

	case domain.DecisionForward:
		p.metrics.Counter("proxy.requests.forwarded", 1, nil)
		status = p.forwardHTTP(w, r, reqCtx, decision, ruleID, requestID)

	case domain.DecisionAlert:
		p.metrics.Counter("proxy.requests.forwarded", 1, nil)
		p.metrics.Counter("proxy.requests.alerted", 1, nil)
		status = p.forwardHTTP(w, r, reqCtx, decision, ruleID, requestID)
	}

	p.metrics.Counter("proxy.requests.total", 1, nil)
	p.metrics.Histogram("proxy.latency.ms", float64(time.Since(start).Milliseconds()), nil)
	p.emitRequestTelemetry(reqCtx, decision, ruleID, status, time.Since(start))
}

func (p *HTTPProxy) forwardHTTP(w http.ResponseWriter, r *http.Request, reqCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID string) int {
	agentCfg := p.cfg.Get()

	wrapHeaders := BuildWrapperHeaders(
		agentCfg.AgentID,
		reqCtx.Process,
		p.netInfo.PrimaryInterface(),
		p.netInfo.IsVPNActive(),
		p.policyVersionFn(),
		decision,
		reqCtx.ServiceCategory,
		reqCtx.AIVendor,
		ruleID,
		requestID,
	)

	proxyReq := &domain.ProxyRequest{
		Method:  r.Method,
		URL:     r.URL.String(),
		Host:    r.Host,
		Headers: cloneHeaders(r.Header),
		Body:    r.Body,
	}
	for k, v := range wrapHeaders {
		proxyReq.Headers[k] = []string{v}
	}

	relayStart := time.Now()
	resp, err := p.gateway.Relay(r.Context(), proxyReq)
	if err != nil {
		p.log.Error("relay failed", "request_id", requestID, "error", err)
		if shouldFallbackToDirectOnGatewayError() && isSafeRetryHTTPMethod(r.Method) {
			p.log.Warn("gateway relay failed; falling back to direct HTTP forwarding on windows", "request_id", requestID, "method", r.Method, "host", reqCtx.Host)
			return p.bypassHTTP(w, r)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer resp.Body.Close()
	p.metrics.Histogram("gateway.relay.latency.ms", float64(time.Since(relayStart).Milliseconds()), nil)

	for k, vs := range resp.Headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return resp.StatusCode
}

func (p *HTTPProxy) bypassHTTP(w http.ResponseWriter, r *http.Request) int {
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	outReq.Header = cloneHeaders(r.Header)

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return resp.StatusCode
}

// ---------------------------------------------------------------------------
// HTTPS CONNECT tunnel
// ---------------------------------------------------------------------------

func (p *HTTPProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := uid.New()
	agentCfg := p.cfg.Get()
	interceptCfg := p.effectiveInterceptionConfig(agentCfg)

	reqCtx := p.buildRequestContext(r, requestID)
	reqCtx.Protocol = domain.InterceptProtocolHTTP
	p.classifyAIRequest(reqCtx, r.Header.Get("Origin"), r.Header.Get("Referer"))

	decision, ruleID, _ := p.router.Route(reqCtx)
	if shouldBypassInfrastructureProcess(reqCtx.Process.Name) {
		p.log.Debug("infrastructure process bypass applied", "request_id", requestID, "process", reqCtx.Process.Name, "host", reqCtx.Host)
		decision = domain.DecisionBypass
		ruleID = ""
	}
	matchedRule := p.lookupMatchedRule(ruleID)
	status := 0

	p.logDecision(reqCtx, decision, ruleID, agentCfg.LogURLPaths)

	switch decision {
	case domain.DecisionBlock:
		p.metrics.Counter("proxy.requests.blocked", 1, nil)
		category := dlpCategory(reqCtx.DLP, reqCtx.ServiceCategory)
		reason := blockReasonForRule(matchedRule, category)
		http.Error(w, fmt.Sprintf("blocked by policy (%s): %s", category, reason), http.StatusForbidden)
		status = http.StatusForbidden

	case domain.DecisionBypass:
		p.metrics.Counter("proxy.requests.bypassed", 1, nil)
		status = p.tunnelDirect(w, r)

	case domain.DecisionForward:
		p.metrics.Counter("proxy.requests.forwarded", 1, nil)
		if shouldInterceptHTTPS(reqCtx.Host, interceptCfg) {
			p.metrics.Counter("proxy.requests.intercepted_https", 1, nil)
			_ = p.interceptCONNECT(w, r, reqCtx, decision, ruleID, requestID, interceptCfg)
			return
		}
		if shouldUseDirectConnectOnDesktop() {
			p.log.Debug("desktop CONNECT direct mode", "request_id", requestID, "host", reqCtx.Host)
			status = p.tunnelDirect(w, r)
		} else {
			status = p.tunnelViaGateway(w, r, reqCtx, decision, ruleID, requestID)
		}

	case domain.DecisionAlert:
		p.metrics.Counter("proxy.requests.forwarded", 1, nil)
		p.metrics.Counter("proxy.requests.alerted", 1, nil)
		if shouldInterceptHTTPS(reqCtx.Host, interceptCfg) {
			p.metrics.Counter("proxy.requests.intercepted_https", 1, nil)
			_ = p.interceptCONNECT(w, r, reqCtx, decision, ruleID, requestID, interceptCfg)
			return
		}
		if shouldUseDirectConnectOnDesktop() {
			p.log.Debug("desktop CONNECT direct mode", "request_id", requestID, "host", reqCtx.Host)
			status = p.tunnelDirect(w, r)
		} else {
			status = p.tunnelViaGateway(w, r, reqCtx, decision, ruleID, requestID)
		}
	}

	p.metrics.Counter("proxy.requests.total", 1, nil)
	p.metrics.Histogram("proxy.latency.ms", float64(time.Since(start).Milliseconds()), nil)
	p.emitRequestTelemetry(reqCtx, decision, ruleID, status, time.Since(start))
}

func (p *HTTPProxy) tunnelDirect(w http.ResponseWriter, r *http.Request) int {
	dialCtx, dialCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer dialCancel()
	targetConn, err := dial.TCP(dialCtx, r.Host, &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		targetConn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		targetConn.Close()
		return http.StatusInternalServerError
	}
	if _, err := io.WriteString(clientRW, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return http.StatusBadGateway
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return http.StatusBadGateway
	}

	pipe(&bufferedConn{Conn: clientConn, reader: clientRW.Reader}, targetConn)
	p.log.Debug("tunnel direct closed", "host", r.Host)
	return http.StatusOK
}

func (p *HTTPProxy) tunnelViaGateway(w http.ResponseWriter, r *http.Request, reqCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID string) int {
	agentCfg := p.cfg.Get()

	gatewayConn, err := p.gateway.DialTunnel(r.Context())
	if err != nil {
		p.log.Error("gateway tunnel dial failed", "request_id", requestID, "error", err)
		if shouldFallbackToDirectOnGatewayError() {
			p.log.Warn("gateway tunnel dial failed; falling back to direct CONNECT tunneling on windows", "request_id", requestID, "host", reqCtx.Host)
			return p.tunnelDirect(w, r)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}

	wrapHeaders := BuildWrapperHeaders(
		agentCfg.AgentID,
		reqCtx.Process,
		p.netInfo.PrimaryInterface(),
		p.netInfo.IsVPNActive(),
		p.policyVersionFn(),
		decision,
		reqCtx.ServiceCategory,
		reqCtx.AIVendor,
		ruleID,
		requestID,
	)

	// Use "/" as request-target so Go 1.22+ ServeMux route matching on the
	// gateway can dispatch this CONNECT request to the proxy handler.
	// The actual upstream tunnel target remains in Host.
	connectReq := fmt.Sprintf("CONNECT / HTTP/1.1\r\nHost: %s\r\n", r.Host)
	for k, v := range wrapHeaders {
		connectReq += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	connectReq += "\r\n"

	if _, err := gatewayConn.Write([]byte(connectReq)); err != nil {
		gatewayConn.Close()
		if shouldFallbackToDirectOnGatewayError() {
			p.log.Warn("gateway CONNECT request failed; falling back to direct CONNECT tunneling on windows", "request_id", requestID, "host", reqCtx.Host, "error", err)
			return p.tunnelDirect(w, r)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}

	gatewayReader := bufio.NewReader(gatewayConn)
	connectResp, err := http.ReadResponse(gatewayReader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		p.log.Error("gateway CONNECT response read failed", "request_id", requestID, "error", err)
		gatewayConn.Close()
		if shouldFallbackToDirectOnGatewayError() {
			p.log.Warn("gateway CONNECT response read failed; falling back to direct CONNECT tunneling on windows", "request_id", requestID, "host", reqCtx.Host)
			return p.tunnelDirect(w, r)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	if connectResp.Body != nil {
		_ = connectResp.Body.Close()
	}
	if connectResp.StatusCode != http.StatusOK {
		p.log.Error("gateway CONNECT rejected", "request_id", requestID, "status", connectResp.StatusCode)
		gatewayConn.Close()
		if shouldFallbackToDirectOnGatewayError() {
			p.log.Warn("gateway CONNECT rejected; falling back to direct CONNECT tunneling on windows", "request_id", requestID, "host", reqCtx.Host, "status", connectResp.StatusCode)
			return p.tunnelDirect(w, r)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		gatewayConn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		gatewayConn.Close()
		return http.StatusInternalServerError
	}
	if _, err := io.WriteString(clientRW, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = gatewayConn.Close()
		return http.StatusBadGateway
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		_ = gatewayConn.Close()
		return http.StatusBadGateway
	}

	pipe(
		&bufferedConn{Conn: clientConn, reader: clientRW.Reader},
		&bufferedConn{Conn: gatewayConn, reader: gatewayReader},
	)
	p.log.Debug("tunnel via gateway closed", "request_id", requestID, "host", reqCtx.Host)
	return http.StatusOK
}

func (p *HTTPProxy) interceptCONNECT(w http.ResponseWriter, r *http.Request, baseCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID string, interceptCfg interceptionSettings) int {
	agentCfg := p.cfg.Get()
	start := time.Now()

	caMgr, err := p.ensureInterceptCA(r.Context())
	if err != nil {
		p.log.Warn("https intercept unavailable", "request_id", requestID, "host", baseCtx.Host, "error", err)
		p.metrics.Counter("proxy.https_intercept.failed", 1, nil)
		if shouldFailOpen(interceptCfg) {
			p.metrics.Counter("proxy.https_intercept.fail_open", 1, nil)
			status := p.tunnelViaGateway(w, r, baseCtx, decision, ruleID, requestID)
			p.emitRequestTelemetry(baseCtx, decision, ruleID, status, time.Since(start))
			return status
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		p.emitRequestTelemetry(baseCtx, decision, ruleID, http.StatusBadGateway, time.Since(start))
		return http.StatusBadGateway
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		return http.StatusInternalServerError
	}

	if _, err := io.WriteString(clientRW, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		return http.StatusBadGateway
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		return http.StatusBadGateway
	}

	rawClient := &bufferedConn{Conn: clientConn, reader: clientRW.Reader}
	leafCert, err := caMgr.LeafCertificate(baseCtx.Host)
	if err != nil {
		p.log.Warn("https intercept leaf cert generation failed", "request_id", requestID, "host", baseCtx.Host, "error", err)
		if shouldFailOpen(interceptCfg) {
			p.metrics.Counter("proxy.https_intercept.fail_open", 1, nil)
			return p.tunnelDirectHijacked(rawClient, r.Host)
		}
		_ = rawClient.Close()
		return http.StatusBadGateway
	}

	clientTLS := tls.Server(rawClient, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*leafCert},
		// M1 interception inspects HTTP/1.1 payloads. Do not advertise h2 to
		// avoid blind pass-through when clients pick HTTP/2.
		NextProtos: []string{"http/1.1"},
	})
	_ = clientTLS.SetDeadline(time.Now().Add(15 * time.Second))
	if err := clientTLS.Handshake(); err != nil {
		p.log.Warn("https intercept client handshake failed", "request_id", requestID, "host", baseCtx.Host, "error", err)
		if shouldFailOpen(interceptCfg) {
			p.metrics.Counter("proxy.https_intercept.fail_open", 1, nil)
			_ = clientTLS.Close()
			return http.StatusBadGateway
		}
		_ = clientTLS.Close()
		return http.StatusBadGateway
	}
	_ = clientTLS.SetDeadline(time.Time{})

	negotiated := strings.TrimSpace(clientTLS.ConnectionState().NegotiatedProtocol)
	if negotiated == "h2" {
		ctxCopy := *baseCtx
		ctxCopy.Protocol = domain.InterceptProtocolHTTP
		ctxCopy.InterceptedHTTPS = true
		ctxCopy.InspectionQuality = domain.InspectionQualitySkipped
		ctxCopy.InspectionSkipReason = "http2_passthrough"
		status := p.proxyTLSBlind(clientTLS, r.Host, negotiated, requestID)
		p.metrics.Counter("proxy.requests.total", 1, nil)
		p.metrics.Histogram("proxy.latency.ms", float64(time.Since(start).Milliseconds()), nil)
		p.emitRequestTelemetry(&ctxCopy, decision, ruleID, status, time.Since(start))
		return status
	}

	reader := bufio.NewReader(clientTLS)
	writer := bufio.NewWriter(clientTLS)
	lastStatus := http.StatusOK
	requests := 0

	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			if err == io.EOF || strings.Contains(strings.ToLower(err.Error()), "closed network connection") {
				break
			}
			p.log.Warn("https intercept parse request failed", "request_id", requestID, "host", baseCtx.Host, "error", err)
			lastStatus = http.StatusBadGateway
			break
		}
		requests++
		requestStart := time.Now()

		reqCtx := *baseCtx
		if host := domain.NormalizeHost(req.Host); host != "" {
			reqCtx.Host = host
		}
		reqCtx.Method = req.Method
		reqCtx.Path = req.URL.Path
		reqCtx.Scheme = "https"
		reqCtx.Protocol = detectRequestProtocol(req)
		reqCtx.Direction = domain.TrafficDirectionOutbound
		reqCtx.InterceptedHTTPS = true
		reqCtx.InspectionQuality = domain.InspectionQualityFull
		reqCtx.InspectionSkipReason = ""
		if req.URL == nil {
			req.URL = &url.URL{}
		}
		req.URL.Scheme = "https"
		req.URL.Host = r.Host
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		if req.Host == "" {
			req.Host = r.Host
		}
		req.RequestURI = ""

		if ai, ok := domain.InferAIService(reqCtx.Host, req.Header.Get("Origin"), req.Header.Get("Referer")); ok {
			reqCtx.ServiceCategory = ai.Category
			reqCtx.AIVendor = ai.Vendor
		}

		if !isInterceptProtocolEnabled(interceptCfg, reqCtx.Protocol) {
			reqCtx.InspectionQuality = domain.InspectionQualitySkipped
			reqCtx.InspectionSkipReason = "protocol_not_enabled"
			status, upgraded := p.passThroughInterceptedRequest(clientTLS, writer, req, r.Host, requestID)
			lastStatus = status
			p.emitRequestTelemetry(&reqCtx, decision, ruleID, status, time.Since(requestStart))
			if upgraded || req.Close {
				break
			}
			continue
		}

		if reqCtx.Protocol != domain.InterceptProtocolHTTP {
			reqCtx.InspectionQuality = domain.InspectionQualitySkipped
			reqCtx.InspectionSkipReason = "protocol_not_implemented"
			status, upgraded := p.passThroughInterceptedRequest(clientTLS, writer, req, r.Host, requestID)
			lastStatus = status
			p.emitRequestTelemetry(&reqCtx, decision, ruleID, status, time.Since(requestStart))
			if upgraded || req.Close {
				break
			}
			continue
		}

		p.inspectRequestBody(req, &reqCtx, requestID)
		reqDecision, reqRuleID, _ := p.router.Route(&reqCtx)
		if shouldBypassInfrastructureProcess(reqCtx.Process.Name) {
			reqDecision = domain.DecisionBypass
			reqRuleID = ""
		}
		matchedRule := p.lookupMatchedRule(reqRuleID)

		if len(reqCtx.DLP.Matches) > 0 {
			actionTaken := dlpActionForDecision(reqDecision)
			reasonCode, reasonDetail := dlpReasonForDecision(reqDecision, matchedRule)
			p.emitDLPTelemetry(&reqCtx, reqCtx.DLP, requestID, actionTaken, reqRuleID, reasonCode, reasonDetail)
		}
		p.notifyUserForDLP(reqCtx.Host, dlpCategory(reqCtx.DLP, reqCtx.ServiceCategory), reqDecision, matchedRule)

		status := http.StatusBadGateway
		switch reqDecision {
		case domain.DecisionBlock:
			p.metrics.Counter("proxy.requests.blocked", 1, nil)
			category := dlpCategory(reqCtx.DLP, reqCtx.ServiceCategory)
			reason := blockReasonForRule(matchedRule, category)
			status = writeBlockedMITMResponse(writer, agentCfg.BlockPageBody, category, reason)

		case domain.DecisionBypass:
			p.metrics.Counter("proxy.requests.bypassed", 1, nil)
			status, _ = p.passThroughInterceptedRequest(clientTLS, writer, req, r.Host, requestID)

		case domain.DecisionAlert:
			p.metrics.Counter("proxy.requests.forwarded", 1, nil)
			p.metrics.Counter("proxy.requests.alerted", 1, nil)
			status = p.forwardInterceptedViaGateway(writer, req, &reqCtx, reqDecision, reqRuleID, requestID, r.Host, interceptCfg)

		default:
			p.metrics.Counter("proxy.requests.forwarded", 1, nil)
			status = p.forwardInterceptedViaGateway(writer, req, &reqCtx, reqDecision, reqRuleID, requestID, r.Host, interceptCfg)
		}

		lastStatus = status
		p.metrics.Counter("proxy.requests.total", 1, nil)
		p.metrics.Histogram("proxy.latency.ms", float64(time.Since(requestStart).Milliseconds()), nil)
		p.emitRequestTelemetry(&reqCtx, reqDecision, reqRuleID, status, time.Since(requestStart))

		if req.Close || status == http.StatusSwitchingProtocols {
			break
		}
	}

	_ = clientTLS.Close()
	if requests == 0 {
		p.metrics.Counter("proxy.requests.total", 1, nil)
		p.metrics.Histogram("proxy.latency.ms", float64(time.Since(start).Milliseconds()), nil)
	}
	return lastStatus
}

func (p *HTTPProxy) ensureInterceptCA(ctx context.Context) (*interceptCertManager, error) {
	p.interceptMu.Lock()
	defer p.interceptMu.Unlock()
	if p.interceptCA == nil {
		p.interceptCA = newInterceptCertManager(p.certStore, p.log)
	}
	if err := p.interceptCA.EnsureReady(ctx); err != nil {
		return nil, err
	}
	return p.interceptCA, nil
}

func (p *HTTPProxy) tunnelDirectHijacked(clientConn net.Conn, target string) int {
	dialCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targetConn, err := dial.TCP(dialCtx, normalizeConnectTarget(target), &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		_ = clientConn.Close()
		return http.StatusBadGateway
	}
	pipe(clientConn, targetConn)
	return http.StatusOK
}

func (p *HTTPProxy) forwardInterceptedViaGateway(clientWriter *bufio.Writer, req *http.Request, reqCtx *domain.RequestContext, decision domain.Decision, ruleID, requestID, targetHost string, interceptCfg interceptionSettings) int {
	agentCfg := p.cfg.Get()
	headers := cloneHeaders(req.Header)
	for k, v := range BuildWrapperHeaders(
		agentCfg.AgentID,
		reqCtx.Process,
		p.netInfo.PrimaryInterface(),
		p.netInfo.IsVPNActive(),
		p.policyVersionFn(),
		decision,
		reqCtx.ServiceCategory,
		reqCtx.AIVendor,
		ruleID,
		requestID,
	) {
		headers[k] = []string{v}
	}

	proxyReq := &domain.ProxyRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Host:    targetHost,
		Headers: headers,
		Body:    req.Body,
	}

	resp, err := p.gateway.Relay(req.Context(), proxyReq)
	if err != nil {
		p.log.Warn("intercept relay failed", "request_id", requestID, "host", reqCtx.Host, "error", err)
		if shouldFailOpen(interceptCfg) && isSafeRetryHTTPMethod(req.Method) {
			p.metrics.Counter("proxy.https_intercept.fail_open", 1, nil)
			status, _ := p.passThroughInterceptedRequest(nil, clientWriter, req, targetHost, requestID)
			return status
		}
		return writeSimpleMITMResponse(clientWriter, http.StatusBadGateway, "bad gateway")
	}
	defer resp.Body.Close()

	httpResp := &http.Response{
		StatusCode: resp.StatusCode,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       resp.Body,
	}
	for k, vs := range resp.Headers {
		for _, v := range vs {
			httpResp.Header.Add(k, v)
		}
	}
	if err := httpResp.Write(clientWriter); err != nil {
		return http.StatusBadGateway
	}
	if err := clientWriter.Flush(); err != nil {
		return http.StatusBadGateway
	}
	return resp.StatusCode
}

func (p *HTTPProxy) passThroughInterceptedRequest(clientConn net.Conn, clientWriter *bufio.Writer, req *http.Request, targetHost, requestID string) (int, bool) {
	_ = requestID
	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rawUpstream, err := dial.TCP(dialCtx, normalizeConnectTarget(targetHost), &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		return writeSimpleMITMResponse(clientWriter, http.StatusBadGateway, "bad gateway"), false
	}

	hostName, _, err := net.SplitHostPort(normalizeConnectTarget(targetHost))
	if err != nil {
		hostName = targetHost
	}
	upstreamTLS := tls.Client(rawUpstream, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: hostName,
		NextProtos: []string{"http/1.1"},
	})
	if err := upstreamTLS.HandshakeContext(dialCtx); err != nil {
		_ = upstreamTLS.Close()
		return writeSimpleMITMResponse(clientWriter, http.StatusBadGateway, "bad gateway"), false
	}

	req.URL.Scheme = "https"
	req.URL.Host = targetHost
	req.RequestURI = ""
	if req.Host == "" {
		req.Host = targetHost
	}
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	if err := req.Write(upstreamTLS); err != nil {
		_ = upstreamTLS.Close()
		return writeSimpleMITMResponse(clientWriter, http.StatusBadGateway, "bad gateway"), false
	}

	upstreamReader := bufio.NewReader(upstreamTLS)
	resp, err := http.ReadResponse(upstreamReader, req)
	if err != nil {
		_ = upstreamTLS.Close()
		return writeSimpleMITMResponse(clientWriter, http.StatusBadGateway, "bad gateway"), false
	}

	status := resp.StatusCode
	if err := resp.Write(clientWriter); err != nil {
		_ = resp.Body.Close()
		_ = upstreamTLS.Close()
		return http.StatusBadGateway, false
	}
	if err := clientWriter.Flush(); err != nil {
		_ = resp.Body.Close()
		_ = upstreamTLS.Close()
		return http.StatusBadGateway, false
	}

	if status == http.StatusSwitchingProtocols && clientConn != nil {
		pipe(clientConn, &bufferedConn{Conn: upstreamTLS, reader: upstreamReader})
		return status, true
	}

	_ = resp.Body.Close()
	_ = upstreamTLS.Close()
	return status, false
}

func (p *HTTPProxy) proxyTLSBlind(clientTLS net.Conn, targetHost, alpn, requestID string) int {
	_ = requestID
	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rawUpstream, err := dial.TCP(dialCtx, normalizeConnectTarget(targetHost), &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		_ = clientTLS.Close()
		return http.StatusBadGateway
	}
	hostName, _, err := net.SplitHostPort(normalizeConnectTarget(targetHost))
	if err != nil {
		hostName = targetHost
	}
	upstreamTLS := tls.Client(rawUpstream, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: hostName,
		NextProtos: []string{alpn, "http/1.1"},
	})
	if err := upstreamTLS.HandshakeContext(dialCtx); err != nil {
		_ = clientTLS.Close()
		_ = upstreamTLS.Close()
		return http.StatusBadGateway
	}
	pipe(clientTLS, upstreamTLS)
	return http.StatusOK
}

func writeSimpleMITMResponse(w *bufio.Writer, status int, body string) int {
	resp := &http.Response{
		StatusCode:    status,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Close:         true,
	}
	resp.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp.Header.Set("Connection", "close")
	if err := resp.Write(w); err != nil {
		return http.StatusBadGateway
	}
	if err := w.Flush(); err != nil {
		return http.StatusBadGateway
	}
	return status
}

func writeBlockedMITMResponse(w *bufio.Writer, blockBody, category, reason string) int {
	message := strings.TrimSpace(blockBody)
	if message == "" {
		message = "Request blocked by security policy."
	}
	payload := fmt.Sprintf("%s\nCategory: %s\nReason: %s\n", message, strings.TrimSpace(category), strings.TrimSpace(reason))
	return writeSimpleMITMResponse(w, http.StatusForbidden, payload)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (p *HTTPProxy) buildRequestContext(r *http.Request, requestID string) *domain.RequestContext {
	host, portStr, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	port, _ := strconv.ParseUint(portStr, 10, 16)

	scheme := "http"
	if r.Method == http.MethodConnect || r.TLS != nil {
		scheme = "https"
	}

	ctx := &domain.RequestContext{
		RequestID:            requestID,
		Host:                 host,
		Port:                 uint16(port),
		Path:                 r.URL.Path,
		Method:               r.Method,
		Scheme:               scheme,
		Protocol:             domain.InterceptProtocolHTTP,
		Direction:            domain.TrafficDirectionOutbound,
		InspectionQuality:    domain.InspectionQualityFull,
		InspectionSkipReason: "",
	}

	proc := p.resolveProcess(r)
	ctx.Process = proc
	return ctx
}

func (p *HTTPProxy) classifyAIRequest(ctx *domain.RequestContext, signals ...string) {
	catalog := domain.BuiltInAIProductCatalog()
	if p.productCatalogFn != nil {
		if managed := p.productCatalogFn(); len(managed) > 0 {
			catalog = managed
		}
	}
	entry, ok := domain.LookupAIProduct(catalog, ctx.Host, ctx.Process)
	if !ok {
		for _, signal := range signals {
			if entry, ok = domain.LookupAIProduct(catalog, signal, ctx.Process); ok {
				break
			}
		}
	}
	if !ok {
		return
	}
	ctx.AIVendor = entry.VendorKey
	ctx.AIProduct = entry.ProductKey
	ctx.ServiceCategory = entry.FunctionalCategory
	if ctx.CaptureSurface == "" {
		ctx.CaptureSurface = inferCaptureSurface(ctx.Process, entry.Surfaces)
	}
}

func inferCaptureSurface(process domain.ProcessInfo, supported []string) string {
	name := strings.ToLower(strings.TrimSpace(process.Name))
	bundle := strings.ToLower(strings.TrimSpace(process.BundleID))
	selectSupported := func(wanted string) string {
		for _, candidate := range supported {
			if strings.EqualFold(candidate, wanted) {
				return strings.ToLower(candidate)
			}
		}
		return ""
	}
	switch {
	case strings.Contains(name, "safari") || bundle == "com.apple.safari":
		if surface := selectSupported("browser_safari"); surface != "" {
			return surface
		}
	case strings.Contains(name, "chrome"), strings.Contains(name, "chromium"),
		strings.Contains(name, "brave"), strings.Contains(name, "edge"),
		strings.Contains(name, "arc"):
		if surface := selectSupported("browser_chromium"); surface != "" {
			return surface
		}
	}
	for _, candidate := range supported {
		token := strings.ReplaceAll(strings.ToLower(candidate), "_", " ")
		if token != "" && (strings.Contains(name, token) || strings.Contains(bundle, strings.ReplaceAll(token, " ", "."))) {
			return strings.ToLower(candidate)
		}
	}
	for _, preferred := range []string{"local_model", "coding_agent", "desktop", "native_https", "api"} {
		if surface := selectSupported(preferred); surface != "" {
			return surface
		}
	}
	return "unknown"
}

func (p *HTTPProxy) resolveProcess(r *http.Request) domain.ProcessInfo {
	if p.resolver == nil {
		return domain.ProcessInfo{}
	}
	clientAddr := r.RemoteAddr
	clientHost, clientPortStr, err := net.SplitHostPort(clientAddr)
	if err != nil {
		return domain.ProcessInfo{}
	}
	clientPort, _ := strconv.ParseUint(clientPortStr, 10, 16)

	listenAddr := p.cfg.Get().ListenAddr
	proxyHost, proxyPortStr, _ := net.SplitHostPort(listenAddr)
	proxyPort, _ := strconv.ParseUint(proxyPortStr, 10, 16)

	info, err := p.resolver.ResolveByConnection(
		clientHost, uint16(clientPort),
		proxyHost, uint16(proxyPort),
	)
	if err != nil {
		p.log.Debug("process resolution failed", "remote", clientAddr, "error", err)
		return domain.ProcessInfo{}
	}
	return info
}

func (p *HTTPProxy) logDecision(ctx *domain.RequestContext, decision domain.Decision, ruleID string, logPaths bool) {
	fields := []interface{}{
		"request_id", ctx.RequestID,
		"host", ctx.Host,
		"decision", decision.String(),
	}
	if ruleID != "" {
		fields = append(fields, "rule_id", ruleID)
	}
	if ctx.Process.Name != "" {
		fields = append(fields, "process", ctx.Process.Name)
	}
	if logPaths && ctx.Path != "" {
		fields = append(fields, "path", ctx.Path)
	}

	switch decision {
	case domain.DecisionBlock:
		p.log.Info("request blocked", fields...)
	default:
		p.log.Debug("request routed", fields...)
	}
}

func pipe(a, b net.Conn) {
	var once sync.Once
	setDrainDeadline := func() {
		deadline := time.Now().Add(30 * time.Second)
		_ = a.SetDeadline(deadline)
		_ = b.SetDeadline(deadline)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		once.Do(setDrainDeadline)
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

func cloneHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		if strings.EqualFold(k, "Proxy-Connection") || strings.EqualFold(k, "Proxy-Authorization") {
			continue
		}
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

func detectRequestProtocol(r *http.Request) string {
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return domain.InterceptProtocolWebSocket
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/grpc") {
		return domain.InterceptProtocolGRPC
	}
	return domain.InterceptProtocolHTTP
}

type interceptionSettings struct {
	enabled     bool
	domains     []string
	protocols   []string
	failMode    string
	captureMode string
	scope       string
}

func shouldInterceptHTTPS(host string, cfg interceptionSettings) bool {
	if !cfg.enabled {
		return false
	}
	host = normalizeInterceptDomain(host)
	if host == "" {
		return false
	}
	if isAPIOnlyScope(cfg.scope) && !isCanonicalAIAPIHost(host) {
		return false
	}
	if isCloudflareProtectedFrontendHost(host) {
		return false
	}
	hostInfo, hostIsAI := domain.LookupAIService(host)
	for _, allowed := range cfg.domains {
		d := normalizeInterceptDomain(allowed)
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
		if !hostIsAI {
			continue
		}
		allowedInfo, ok := domain.LookupAIService(d)
		if !ok {
			continue
		}
		if inheritsVendorSiblingInterception(host, hostInfo, d, allowedInfo) {
			return true
		}
	}
	return false
}

func isAPIOnlyScope(scope string) bool {
	return strings.EqualFold(strings.TrimSpace(scope), domain.InterceptScopeAPIOnly)
}

func isCanonicalAIAPIHost(host string) bool {
	host = normalizeInterceptDomain(host)
	switch host {
	case "api.openai.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
		"content-gemini.googleapis.com",
		"api.githubcopilot.com",
		"copilot-proxy.githubusercontent.com",
		"api2.cursor.sh",
		"api.cohere.ai",
		"api.cohere.com",
		"api.mistral.ai",
		"api.together.ai",
		"api.perplexity.ai",
		"api.replicate.com",
		"api-inference.huggingface.co",
		"api.stability.ai",
		"api.groq.com",
		"api.x.ai":
		return true
	default:
		return false
	}
}

func inheritsVendorSiblingInterception(host string, hostInfo domain.AIServiceInfo, allowedDomain string, allowedInfo domain.AIServiceInfo) bool {
	if hostInfo.Vendor != allowedInfo.Vendor {
		return false
	}
	// Gemini web traffic can fan out to helper subdomains hosted under
	// clients6.google.com. Preserve interception coverage for those helpers when
	// Gemini domains are explicitly allowlisted.
	if hostInfo.Vendor == "google" {
		if strings.HasSuffix(host, ".clients6.google.com") {
			return strings.Contains(allowedDomain, "gemini") || allowedDomain == "generativelanguage.googleapis.com"
		}
	}
	return false
}

func isCloudflareProtectedFrontendHost(host string) bool {
	host = normalizeInterceptDomain(host)
	if host == "" {
		return false
	}
	// Interactive AI web frontends are controlled with endpoint pre-send capture
	// and intentionally excluded from HTTPS MITM to avoid challenge loops,
	// brittle web breakage, and false-positive DLP blocks on app/session tokens.
	switch {
	case host == "claude.ai", strings.HasSuffix(host, ".claude.ai"):
		return true
	case host == "chatgpt.com", strings.HasSuffix(host, ".chatgpt.com"):
		return true
	case host == "chat.openai.com", strings.HasSuffix(host, ".chat.openai.com"):
		return true
	case host == "gemini.google.com", strings.HasSuffix(host, ".gemini.google.com"):
		return true
	case host == "bard.google.com", strings.HasSuffix(host, ".bard.google.com"):
		return true
	case host == "clients6.google.com", strings.HasSuffix(host, ".clients6.google.com"):
		return true
	default:
		return false
	}
}

func isInterceptProtocolEnabled(cfg interceptionSettings, protocol string) bool {
	if len(cfg.protocols) == 0 {
		return false
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	for _, p := range cfg.protocols {
		if p == protocol {
			return true
		}
	}
	return false
}

func shouldFailOpen(cfg interceptionSettings) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.failMode), domain.HTTPSInterceptFailOpen)
}

func normalizeConnectTarget(hostport string) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	return net.JoinHostPort(hostport, "443")
}

func shouldFallbackToDirectOnGatewayError() bool {
	return runtime.GOOS == "windows"
}

func isSafeRetryHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func shouldBypassInfrastructureProcess(processName string) bool {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(processName)) {
	case "com.docker.backend", "com.docker.backend.exe",
		"docker desktop", "docker desktop.exe",
		"docker", "docker.exe", "dockerd", "dockerd.exe",
		"vpnkit", "vpnkit.exe":
		return true
	default:
		return false
	}
}

func shouldUseDirectConnectOnDesktop() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func (p *HTTPProxy) effectiveInterceptionConfig(local *domain.AgentConfig) interceptionSettings {
	cfg := interceptionSettings{
		enabled:     false,
		domains:     []string{},
		protocols:   []string{},
		failMode:    domain.HTTPSInterceptFailOpen,
		captureMode: domain.InterceptCaptureModeEncryptedFullBody,
		scope:       domain.InterceptScopeAPIOnly,
	}
	if local != nil {
		cfg.enabled = local.HTTPSInterceptEnabled
		cfg.failMode = strings.ToLower(strings.TrimSpace(local.HTTPSInterceptFailMode))
		if cfg.failMode == "" {
			cfg.failMode = domain.HTTPSInterceptFailOpen
		}
		if v := strings.ToLower(strings.TrimSpace(local.HTTPSInterceptCaptureMode)); v != "" {
			cfg.captureMode = v
		}
		cfg.domains = append(cfg.domains, local.HTTPSInterceptDomains...)
		cfg.protocols = append(cfg.protocols, local.HTTPSInterceptProtocols...)
	}

	if p.managedInterceptFn != nil {
		managed := p.managedInterceptFn()
		cfg.enabled = cfg.enabled || managed.Enabled
		cfg.domains = append(cfg.domains, managed.Domains...)
		cfg.protocols = append(cfg.protocols, managed.Protocols...)
		if v := strings.ToLower(strings.TrimSpace(managed.FailMode)); v != "" {
			cfg.failMode = v
		}
		if v := strings.ToLower(strings.TrimSpace(managed.CaptureMode)); v != "" {
			cfg.captureMode = v
		}
		if v := strings.ToLower(strings.TrimSpace(managed.Scope)); v != "" {
			cfg.scope = v
		}
	}

	domainSet := make(map[string]struct{}, len(cfg.domains)+12)
	for _, d := range cfg.domains {
		if n := normalizeInterceptDomain(d); n != "" {
			domainSet[n] = struct{}{}
		}
	}
	for d := range domainSet {
		for _, alias := range vendorAliasesForDomain(d) {
			if n := normalizeInterceptDomain(alias); n != "" {
				domainSet[n] = struct{}{}
			}
		}
	}
	cfg.domains = cfg.domains[:0]
	for d := range domainSet {
		cfg.domains = append(cfg.domains, d)
	}

	protocolSet := make(map[string]struct{}, len(cfg.protocols))
	for _, p := range cfg.protocols {
		v := strings.ToLower(strings.TrimSpace(p))
		switch v {
		case domain.InterceptProtocolHTTP, domain.InterceptProtocolWebSocket, domain.InterceptProtocolGRPC:
			protocolSet[v] = struct{}{}
		}
	}
	if len(protocolSet) == 0 {
		protocolSet[domain.InterceptProtocolHTTP] = struct{}{}
	}
	cfg.protocols = cfg.protocols[:0]
	for p := range protocolSet {
		cfg.protocols = append(cfg.protocols, p)
	}
	return cfg
}

func normalizeInterceptDomain(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "*.")
	if strings.Contains(v, "://") {
		if parsed, err := url.Parse(v); err == nil {
			v = parsed.Host
		}
	}
	if strings.Contains(v, "/") {
		v = strings.SplitN(v, "/", 2)[0]
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimPrefix(v, ".")
	if v == "" || !strings.Contains(v, ".") {
		return ""
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return ""
	}
	return v
}

func vendorAliasesForDomain(domain string) []string {
	switch {
	case domain == "api.openai.com":
		return []string{"api.openai.com"}
	case domain == "openai.com", domain == "chat.openai.com", domain == "chatgpt.com":
		return []string{"api.openai.com"}
	case strings.HasSuffix(domain, "anthropic.com") || domain == "claude.ai":
		return []string{"api.anthropic.com"}
	case strings.HasSuffix(domain, "google.com") || strings.HasSuffix(domain, "googleapis.com") || strings.Contains(domain, "gemini"):
		return []string{"generativelanguage.googleapis.com", "content-gemini.googleapis.com"}
	case strings.HasSuffix(domain, "githubcopilot.com") || strings.HasSuffix(domain, "githubusercontent.com"):
		return []string{"api.githubcopilot.com", "copilot-proxy.githubusercontent.com"}
	case strings.Contains(domain, "cursor"):
		return []string{"api2.cursor.sh"}
	default:
		return nil
	}
}

func (p *HTTPProxy) inspectRequestBody(r *http.Request, reqCtx *domain.RequestContext, requestID string) {
	if r.Body == nil || r.ContentLength == 0 {
		return
	}

	// Read a bounded prefix for DLP inspection and then reconstruct the body so
	// downstream forwarding still receives the full payload.
	prefetch, err := io.ReadAll(io.LimitReader(r.Body, int64(dlp.MaxBodySize+1)))
	if err != nil {
		p.log.Warn("dlp scan skipped: read body failed", "request_id", requestID, "error", err)
		return
	}

	scanBody := prefetch
	bodyTruncated := false
	if len(prefetch) > dlp.MaxBodySize {
		scanBody = prefetch[:dlp.MaxBodySize]
		bodyTruncated = true
	}

	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if dlp.ShouldSuppressPath(reqCtx.Path) {
		reqCtx.InspectionQuality = domain.InspectionQualitySkipped
		reqCtx.InspectionSkipReason = "suppressed_path"
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefetch), r.Body))
		return
	}

	info := dlp.ScanRequest(contentType, reqCtx.Path, scanBody, nil)
	if info.BodySample == "" {
		info.BodySample = string(scanBody)
	}
	info.BodyTruncated = bodyTruncated
	reqCtx.DLP = info
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefetch), r.Body))
}

func (p *HTTPProxy) emitRequestTelemetry(ctx *domain.RequestContext, decision domain.Decision, ruleID string, status int, latency time.Duration) {
	cfg := p.cfg.Get()

	data := map[string]interface{}{
		"method":     ctx.Method,
		"host":       ctx.Host,
		"decision":   telemetryDecision(decision),
		"latency_ms": latency.Milliseconds(),
	}
	if status > 0 {
		data["status"] = status
	}
	if ctx.Port > 0 {
		data["port"] = int(ctx.Port)
	}
	if ruleID != "" {
		data["rule_id"] = ruleID
	}
	if ctx.Process.Name != "" {
		data["source_app"] = ctx.Process.Name
	}
	if ctx.ServiceCategory != "" {
		data["service_category"] = ctx.ServiceCategory
	}
	if ctx.AIVendor != "" {
		data["ai_vendor"] = ctx.AIVendor
	}
	if ctx.CaptureSurface != "" {
		data["capture_surface"] = ctx.CaptureSurface
	}
	if ctx.Protocol != "" {
		data["protocol"] = ctx.Protocol
	}
	if ctx.Direction != "" {
		data["direction"] = ctx.Direction
	}
	if ctx.InterceptedHTTPS {
		data["intercepted_https"] = true
	}
	if ctx.InspectionQuality != "" {
		data["inspection_quality"] = ctx.InspectionQuality
	}
	if ctx.InspectionSkipReason != "" {
		data["inspection_skip_reason"] = ctx.InspectionSkipReason
	}

	_ = p.metrics.Emit("proxy.request", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
		Data:      data,
	})

	if ctx.AIVendor != "" && ctx.AIProduct != "" {
		activity := aiactivity.NewRequestEvent(
			ctx.AIVendor,
			ctx.AIProduct,
			ctx.CaptureSurface,
			ctx.Process.Name,
			cfg.AgentID,
			ctx.RequestID,
			time.Now(),
		)
		if err := activity.Validate(); err != nil {
			p.log.Warn("ai activity event rejected locally", "error", err)
			return
		}
		_ = p.metrics.Emit(aiactivity.EventName, &domain.EventPayload{
			AgentID:   cfg.AgentID,
			Timestamp: activity.ObservedAt,
			Data:      activity.Data(),
		})
	}
}

func (p *HTTPProxy) emitDLPTelemetry(ctx *domain.RequestContext, info domain.DLPInfo, requestID, action, policyRuleID, reasonCode, _ string) {
	cfg := p.cfg.Get()
	matchTypes := make([]string, 0, len(info.Matches))
	seen := make(map[string]bool)
	for _, m := range info.Matches {
		t := string(m.Type)
		if !seen[t] {
			matchTypes = append(matchTypes, t)
			seen[t] = true
		}
	}

	data := map[string]interface{}{
		"host":                  ctx.Host,
		"method":                ctx.Method,
		"request_id":            requestID,
		"match_types":           matchTypes,
		"match_count":           len(info.Matches),
		"action_taken":          action,
		"severity":              info.Severity,
		"classification_reason": info.ClassificationReason,
		"content_type":          info.ContentType,
		"file_count":            info.FileCount,
	}
	if sev, ok := data["severity"].(string); ok && strings.TrimSpace(strings.ToLower(sev)) == "low" && strings.EqualFold(action, "block") {
		data["severity"] = "medium"
	}
	if policyRuleID != "" {
		data["policy_rule_id"] = policyRuleID
	}
	if reasonCode != "" {
		data["reason_code"] = reasonCode
	}
	if ctx.ServiceCategory != "" {
		data["service_category"] = ctx.ServiceCategory
	}
	if ctx.AIVendor != "" {
		data["ai_vendor"] = ctx.AIVendor
	}
	if ctx.Process.Name != "" {
		data["source_app"] = ctx.Process.Name
	}
	if ctx.CaptureSurface != "" {
		data["capture_surface"] = ctx.CaptureSurface
	}
	if ctx.Protocol != "" {
		data["protocol"] = ctx.Protocol
	}
	if ctx.Direction != "" {
		data["direction"] = ctx.Direction
	}
	if ctx.InterceptedHTTPS {
		data["intercepted_https"] = true
	}
	if ctx.InspectionQuality != "" {
		data["inspection_quality"] = ctx.InspectionQuality
	}
	if ctx.InspectionSkipReason != "" {
		data["inspection_skip_reason"] = ctx.InspectionSkipReason
	}

	_ = p.metrics.Emit("dlp.match", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func telemetryDecision(decision domain.Decision) string {
	switch decision {
	case domain.DecisionBlock:
		return "block"
	case domain.DecisionBypass:
		return "bypass"
	case domain.DecisionAlert:
		return "alert"
	default:
		return "allow"
	}
}

type matchedRuleLookup interface {
	LookupRule(id string) *domain.PolicyRule
}

func (p *HTTPProxy) lookupMatchedRule(ruleID string) *domain.PolicyRule {
	if ruleID == "" {
		return nil
	}
	lookup, ok := p.router.(matchedRuleLookup)
	if !ok {
		return nil
	}
	return lookup.LookupRule(ruleID)
}

func dlpActionForDecision(decision domain.Decision) string {
	if decision == domain.DecisionBlock {
		return "block"
	}
	return "alert"
}

func dlpReasonForDecision(decision domain.Decision, matchedRule *domain.PolicyRule) (string, string) {
	if decision == domain.DecisionBlock {
		if matchedRule != nil && strings.TrimSpace(matchedRule.BlockReason) != "" {
			return "policy_block", strings.TrimSpace(matchedRule.BlockReason)
		}
		return "policy_block", "Blocked by organization policy"
	}
	if decision == domain.DecisionAlert {
		if matchedRule != nil && strings.TrimSpace(matchedRule.BlockReason) != "" {
			return "policy_alert", strings.TrimSpace(matchedRule.BlockReason)
		}
		return "policy_alert", "Matched alert policy"
	}
	return "dlp_match", "Sensitive content detected by DLP scanner"
}

func (p *HTTPProxy) notifyUserForDLP(host, category string, decision domain.Decision, matchedRule *domain.PolicyRule) {
	if p.notifier == nil {
		return
	}
	if decision != domain.DecisionAlert && decision != domain.DecisionBlock {
		return
	}
	host = normalizeInterceptDomain(host)
	if host == "" {
		host = strings.TrimSpace(host)
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = "sensitive"
	}

	key := fmt.Sprintf("%s|%s|%s", decision.String(), host, category)
	now := time.Now()
	p.notifyMu.Lock()
	if last, ok := p.notifyLast[key]; ok && now.Sub(last) < 30*time.Second {
		p.notifyMu.Unlock()
		return
	}
	p.notifyLast[key] = now
	p.notifyMu.Unlock()

	title := "Themisto Policy Warning"
	message := fmt.Sprintf("Potentially %s content detected for %s. Please review company policy before sending.", category, host)
	if decision == domain.DecisionBlock {
		title = "Themisto Blocked Sensitive Content"
		reason := blockReasonForRule(matchedRule, category)
		message = fmt.Sprintf("This request to %s was blocked because %s", host, strings.TrimSpace(reason))
	}
	_ = p.notifier.Notify(context.Background(), title, message)
}

func dlpCategory(info domain.DLPInfo, serviceCategory string) string {
	if info.ContainsCredentials {
		return "credentials"
	}
	if info.ContainsPII {
		return "pii"
	}
	if info.ContainsSourceCode {
		return "source_code"
	}
	if len(info.Matches) > 0 {
		return string(info.Matches[0].Type)
	}
	if strings.TrimSpace(serviceCategory) != "" {
		return serviceCategory
	}
	return "policy"
}

func blockReasonForRule(rule *domain.PolicyRule, category string) string {
	if rule != nil && strings.TrimSpace(rule.BlockReason) != "" {
		return strings.TrimSpace(rule.BlockReason)
	}
	switch category {
	case "credentials":
		return "Credentials were detected in this request."
	case "pii":
		return "Personal data was detected in this request."
	case "source_code":
		return "Source code was detected in this request."
	default:
		return "Request blocked by active organization policy."
	}
}

func (p *HTTPProxy) writeBlockedResponse(w http.ResponseWriter, blockBody, category, reason string) {
	message := strings.TrimSpace(blockBody)
	if message == "" {
		message = "Request blocked by security policy."
	}
	safeMessage := strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " ")
	safeCategory := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(category), "\r", " "), "\n", " ")
	safeReason := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(reason), "\r", " "), "\n", " ")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, "%s\nCategory: %s\nReason: %s\n", safeMessage, safeCategory, safeReason)
}
