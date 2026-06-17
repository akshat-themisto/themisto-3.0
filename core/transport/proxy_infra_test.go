package transport

import (
	"context"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
)

func TestShouldBypassInfrastructureProcess_DockerBackend(t *testing.T) {
	gotWin := shouldBypassInfrastructureProcess("com.docker.backend.exe")
	gotMac := shouldBypassInfrastructureProcess("com.docker.backend")

	if runtime.GOOS == "windows" && !gotWin {
		t.Fatal("expected docker backend process to be bypassed on windows")
	}
	if runtime.GOOS == "darwin" && !gotMac {
		t.Fatal("expected docker backend process to be bypassed on darwin")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" && (gotWin || gotMac) {
		t.Fatal("did not expect docker backend process bypass outside windows/darwin")
	}
}

func TestShouldBypassInfrastructureProcess_NormalBrowser(t *testing.T) {
	if shouldBypassInfrastructureProcess("msedge.exe") {
		t.Fatal("did not expect browser process to be infrastructure-bypassed")
	}
}

func TestShouldUseDirectConnectOnDesktop(t *testing.T) {
	got := shouldUseDirectConnectOnDesktop()
	if (runtime.GOOS == "windows" || runtime.GOOS == "darwin") && !got {
		t.Fatal("expected desktop direct CONNECT mode enabled on windows and darwin")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" && got {
		t.Fatal("did not expect desktop direct CONNECT mode outside windows/darwin")
	}
}

func TestShouldInterceptHTTPS(t *testing.T) {
	agentCfg := &domain.AgentConfig{
		HTTPSInterceptEnabled: true,
		HTTPSInterceptDomains: []string{"api.openai.com", "anthropic.com"},
	}
	p := &HTTPProxy{}
	cfg := p.effectiveInterceptionConfig(agentCfg)
	if !shouldInterceptHTTPS("api.openai.com", cfg) {
		t.Fatal("expected exact domain match to be intercepted")
	}
	if shouldInterceptHTTPS("chat.api.openai.com", cfg) {
		t.Fatal("did not expect api.openai.com subdomain interception in api_only mode")
	}
	if !shouldInterceptHTTPS("api.anthropic.com", cfg) {
		t.Fatal("expected api.anthropic.com interception via anthropic.com allowlist in api_only mode")
	}
	if shouldInterceptHTTPS("example.com", cfg) {
		t.Fatal("did not expect non-allowlisted domain to be intercepted")
	}
	if !shouldInterceptHTTPS("API.OPENAI.COM:443", cfg) {
		t.Fatal("expected normalization of host + port to match allowlist")
	}
	if shouldInterceptHTTPS("chat.openai.com", cfg) {
		t.Fatal("did not expect chat.openai.com interception when only api.openai.com is allowlisted")
	}
	if shouldInterceptHTTPS("chatgpt.com", cfg) {
		t.Fatal("did not expect chatgpt.com interception when only api.openai.com is allowlisted")
	}
	if shouldInterceptHTTPS("claude.ai", cfg) {
		t.Fatal("did not expect claude.ai interception due Cloudflare frontend exclusion")
	}
}

func TestEffectiveInterceptionConfig_MergesManagedAndLocal(t *testing.T) {
	p := &HTTPProxy{
		managedInterceptFn: func() domain.PolicyInterception {
			return domain.PolicyInterception{
				Enabled:   true,
				Domains:   []string{"claude.ai"},
				Protocols: []string{"grpc"},
				FailMode:  domain.HTTPSInterceptFailOpen,
			}
		},
	}
	cfg := p.effectiveInterceptionConfig(&domain.AgentConfig{
		HTTPSInterceptEnabled:   false,
		HTTPSInterceptDomains:   []string{"api.openai.com"},
		HTTPSInterceptProtocols: []string{"websocket"},
		HTTPSInterceptFailMode:  domain.HTTPSInterceptFailClosed,
	})
	if !cfg.enabled {
		t.Fatal("expected managed enabled flag to enable interception")
	}
	if !shouldInterceptHTTPS("api.openai.com", cfg) {
		t.Fatal("expected local domain to remain in merged interception config")
	}
	if !shouldInterceptHTTPS("api.anthropic.com", cfg) {
		t.Fatal("expected Anthropic API domain to remain interceptable")
	}
	if shouldInterceptHTTPS("claude.ai", cfg) {
		t.Fatal("did not expect claude.ai interception even when managed list includes it")
	}
	if !isInterceptProtocolEnabled(cfg, domain.InterceptProtocolGRPC) || !isInterceptProtocolEnabled(cfg, domain.InterceptProtocolWebSocket) {
		t.Fatalf("expected merged protocols, got %#v", cfg.protocols)
	}
}

type testNotifier struct {
	count int
}

func (n *testNotifier) Notify(_ context.Context, _, _ string) error {
	n.count++
	return nil
}

func TestNotifyUserForDLP_RateLimited(t *testing.T) {
	notifier := &testNotifier{}
	p := &HTTPProxy{
		notifier:   notifier,
		notifyLast: make(map[string]time.Time),
	}
	p.notifyUserForDLP("api.openai.com", "credentials", domain.DecisionAlert, nil)
	p.notifyUserForDLP("api.openai.com", "credentials", domain.DecisionAlert, nil)
	if notifier.count != 1 {
		t.Fatalf("notifier count = %d, want 1 due to rate limiting", notifier.count)
	}
}

func TestShouldInterceptHTTPS_KnownVendorSiblingHost(t *testing.T) {
	agentCfg := &domain.AgentConfig{
		HTTPSInterceptEnabled: true,
		HTTPSInterceptDomains: []string{"gemini.google.com"},
	}
	p := &HTTPProxy{}
	cfg := p.effectiveInterceptionConfig(agentCfg)

	if shouldInterceptHTTPS("alkalimakersuite-pa.clients6.google.com", cfg) {
		t.Fatal("did not expect clients6.google.com Gemini helper host interception in frontend-bypass mode")
	}
}

func TestIsCloudflareProtectedFrontendHost(t *testing.T) {
	if !isCloudflareProtectedFrontendHost("claude.ai") {
		t.Fatal("expected claude.ai to be excluded from HTTPS MITM interception")
	}
	if !isCloudflareProtectedFrontendHost("WWW.CLAUDE.AI") {
		t.Fatal("expected claude.ai subdomains to be excluded from HTTPS MITM interception")
	}
	if !isCloudflareProtectedFrontendHost("chatgpt.com") {
		t.Fatal("expected chatgpt.com to be excluded from HTTPS MITM interception")
	}
	if !isCloudflareProtectedFrontendHost("chat.openai.com") {
		t.Fatal("expected chat.openai.com to be excluded from HTTPS MITM interception")
	}
	if !isCloudflareProtectedFrontendHost("gemini.google.com") {
		t.Fatal("expected gemini.google.com to be excluded from HTTPS MITM interception")
	}
	if !isCloudflareProtectedFrontendHost("clients6.google.com") {
		t.Fatal("expected clients6.google.com to be excluded from HTTPS MITM interception")
	}
	if isCloudflareProtectedFrontendHost("api.anthropic.com") {
		t.Fatal("did not expect api.anthropic.com to be excluded")
	}
	if isCloudflareProtectedFrontendHost("api.openai.com") {
		t.Fatal("did not expect api.openai.com to be excluded")
	}
	if isCloudflareProtectedFrontendHost("generativelanguage.googleapis.com") {
		t.Fatal("did not expect generativelanguage.googleapis.com to be excluded")
	}
}

func TestDetectRequestProtocol(t *testing.T) {
	wsReq, _ := http.NewRequest(http.MethodGet, "https://example.com/ws", nil)
	wsReq.Header.Set("Connection", "Upgrade")
	wsReq.Header.Set("Upgrade", "websocket")
	if got := detectRequestProtocol(wsReq); got != domain.InterceptProtocolWebSocket {
		t.Fatalf("detectRequestProtocol websocket = %q, want %q", got, domain.InterceptProtocolWebSocket)
	}

	grpcReq, _ := http.NewRequest(http.MethodPost, "https://example.com/grpc", nil)
	grpcReq.Header.Set("Content-Type", "application/grpc+proto")
	if got := detectRequestProtocol(grpcReq); got != domain.InterceptProtocolGRPC {
		t.Fatalf("detectRequestProtocol grpc = %q, want %q", got, domain.InterceptProtocolGRPC)
	}

	httpReq, _ := http.NewRequest(http.MethodPost, "https://example.com/http", nil)
	if got := detectRequestProtocol(httpReq); got != domain.InterceptProtocolHTTP {
		t.Fatalf("detectRequestProtocol http = %q, want %q", got, domain.InterceptProtocolHTTP)
	}
}
