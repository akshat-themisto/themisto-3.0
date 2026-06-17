package config

import (
	"time"

	"github.com/themisto/agent/core/domain"
)

// applyDefaults fills zero-valued fields of cfg with production defaults.
func applyDefaults(cfg *domain.AgentConfig) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8080"
	}
	if cfg.HTTPSInterceptFailMode == "" {
		cfg.HTTPSInterceptFailMode = domain.HTTPSInterceptFailOpen
	}
	if len(cfg.HTTPSInterceptProtocols) == 0 {
		cfg.HTTPSInterceptProtocols = []string{domain.InterceptProtocolHTTP}
	}
	if cfg.HTTPSInterceptCaptureMode == "" {
		cfg.HTTPSInterceptCaptureMode = domain.InterceptCaptureModeEncryptedFullBody
	}
	if cfg.PolicySyncInterval == 0 {
		cfg.PolicySyncInterval = 60 * time.Second
	}
	if cfg.TelemetryFlushInterval == 0 {
		cfg.TelemetryFlushInterval = 30 * time.Second
	}
	if cfg.MaxConcurrentConns == 0 {
		cfg.MaxConcurrentConns = 1000
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}
	if cfg.BlockPageBody == "" {
		cfg.BlockPageBody = defaultBlockPage
	}
	if cfg.InitialBackoff == 0 {
		cfg.InitialBackoff = 1 * time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 60 * time.Second
	}
	if cfg.BackoffMultiplier == 0 {
		cfg.BackoffMultiplier = 2.0
	}
	if cfg.JitterFraction == 0 {
		cfg.JitterFraction = 0.2
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 5
	}
	if cfg.CircuitBreakerThreshold == 0 {
		cfg.CircuitBreakerThreshold = 10
	}
	if cfg.CircuitBreakerCooldown == 0 {
		cfg.CircuitBreakerCooldown = 120 * time.Second
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 30 * time.Second
	}
	if cfg.IntegrityCheckInterval == 0 {
		cfg.IntegrityCheckInterval = 30 * time.Second
	}
	if cfg.PromptSemanticsPolicy == "" {
		cfg.PromptSemanticsPolicy = defaultPromptSemanticsPolicy
	}
	if cfg.PromptSemanticsLocalURL == "" || cfg.PromptSemanticsLocalURL == "http://127.0.0.1:17176/v1/classify" {
		cfg.PromptSemanticsLocalURL = "http://127.0.0.1:17177/v1/classify"
	}
	if cfg.PromptSemanticsLocalTimeout == 0 {
		cfg.PromptSemanticsLocalTimeout = 250 * time.Millisecond
	}
	if cfg.PromptSemanticsGatewayTimeout == 0 {
		cfg.PromptSemanticsGatewayTimeout = 900 * time.Millisecond
	}
	if cfg.PromptSemanticsBlockThreshold == 0 {
		cfg.PromptSemanticsBlockThreshold = 0.86
	}
	if cfg.PromptSemanticsAlertThreshold == 0 {
		cfg.PromptSemanticsAlertThreshold = 0.68
	}
	if cfg.PromptSemanticsAmbiguousThreshold == 0 {
		cfg.PromptSemanticsAmbiguousThreshold = 0.58
	}
}

const defaultBlockPage = `<!DOCTYPE html>
<html>
<head><title>Request Blocked</title></head>
<body>
<h1>Request Blocked</h1>
<p>This request has been blocked by your organisation's traffic governance policy.</p>
</body>
</html>`

const defaultPromptSemanticsPolicy = "Block unsafe corporate AI use, including prompts that expose credentials, secrets, source code, customer data, employee data, regulated records, private keys, access tokens, production logs, or attempts to use unsanctioned AI tools for sensitive work. Allow approved business assistance, drafting, summarization, brainstorming, and debugging when sensitive data has been removed."
