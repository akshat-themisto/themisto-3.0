package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
)

// validate checks cfg for invalid or missing values and returns an error
// describing every violation found.
func validate(cfg *domain.AgentConfig) error {
	var errs []error

	if cfg.GatewayURL != "" {
		u, err := url.Parse(cfg.GatewayURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("gateway_url: %w", err))
		} else if u.Scheme != "https" {
			errs = append(errs, errors.New("gateway_url must use https scheme"))
		}
	}

	if cfg.ListenAddr == "" {
		errs = append(errs, errors.New("listen_addr is required"))
	} else {
		host, _, err := net.SplitHostPort(cfg.ListenAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("listen_addr: %w", err))
		} else {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				errs = append(errs, errors.New("listen_addr host must be a loopback address"))
			}
		}
	}
	if cfg.PromptCaptureListenAddr != "" {
		host, _, err := net.SplitHostPort(cfg.PromptCaptureListenAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("prompt_capture_listen_addr: %w", err))
		} else {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				errs = append(errs, errors.New("prompt_capture_listen_addr host must be a loopback address"))
			}
		}
	}

	cfg.HTTPSInterceptFailMode = strings.ToLower(strings.TrimSpace(cfg.HTTPSInterceptFailMode))
	switch cfg.HTTPSInterceptFailMode {
	case domain.HTTPSInterceptFailOpen, domain.HTTPSInterceptFailClosed:
	default:
		errs = append(errs, errors.New("https_intercept_fail_mode must be fail_open or fail_closed"))
	}

	cfg.HTTPSInterceptCaptureMode = strings.ToLower(strings.TrimSpace(cfg.HTTPSInterceptCaptureMode))
	if cfg.HTTPSInterceptCaptureMode != domain.InterceptCaptureModeEncryptedFullBody {
		errs = append(errs, errors.New("https_intercept_capture_mode must be encrypted_full_body"))
	}

	seenProtocols := make(map[string]bool)
	normalizedProtocols := make([]string, 0, len(cfg.HTTPSInterceptProtocols))
	for _, raw := range cfg.HTTPSInterceptProtocols {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		switch p {
		case domain.InterceptProtocolHTTP, domain.InterceptProtocolWebSocket, domain.InterceptProtocolGRPC:
		default:
			errs = append(errs, fmt.Errorf("https_intercept_protocols contains invalid value %q", raw))
			continue
		}
		if seenProtocols[p] {
			continue
		}
		seenProtocols[p] = true
		normalizedProtocols = append(normalizedProtocols, p)
	}
	if len(normalizedProtocols) == 0 {
		errs = append(errs, errors.New("https_intercept_protocols must include at least one protocol"))
	}
	cfg.HTTPSInterceptProtocols = normalizedProtocols

	normalizedDomains := make([]string, 0, len(cfg.HTTPSInterceptDomains))
	seenDomains := make(map[string]bool)
	for _, raw := range cfg.HTTPSInterceptDomains {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" {
			continue
		}
		if strings.Contains(host, "://") {
			errs = append(errs, fmt.Errorf("https_intercept_domains entry %q must be host only", raw))
			continue
		}
		if strings.Contains(host, "/") {
			errs = append(errs, fmt.Errorf("https_intercept_domains entry %q must not include path", raw))
			continue
		}
		parsedHost := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			parsedHost = h
		}
		if parsedHost == "" {
			errs = append(errs, fmt.Errorf("https_intercept_domains entry %q is invalid", raw))
			continue
		}
		if net.ParseIP(parsedHost) == nil {
			for _, label := range strings.Split(parsedHost, ".") {
				if label == "" {
					errs = append(errs, fmt.Errorf("https_intercept_domains entry %q is invalid", raw))
					parsedHost = ""
					break
				}
			}
			if parsedHost == "" {
				continue
			}
		}
		if seenDomains[host] {
			continue
		}
		seenDomains[host] = true
		normalizedDomains = append(normalizedDomains, host)
	}
	cfg.HTTPSInterceptDomains = normalizedDomains

	if cfg.PolicySyncInterval != 0 {
		if cfg.PolicySyncInterval < 10*time.Second || cfg.PolicySyncInterval > time.Hour {
			errs = append(errs, errors.New("policy_sync_interval must be between 10s and 1h"))
		}
	}

	if cfg.TelemetryFlushInterval != 0 {
		if cfg.TelemetryFlushInterval < 5*time.Second || cfg.TelemetryFlushInterval > 5*time.Minute {
			errs = append(errs, errors.New("telemetry_flush_interval must be between 5s and 5m"))
		}
	}

	if cfg.PromptSemanticsEnabled {
		if strings.TrimSpace(cfg.PromptSemanticsPolicy) == "" {
			errs = append(errs, errors.New("prompt_semantics_policy is required when prompt semantics is enabled"))
		}
		if cfg.PromptSemanticsLocalTimeout < 50*time.Millisecond || cfg.PromptSemanticsLocalTimeout > 2*time.Second {
			errs = append(errs, errors.New("prompt_semantics_local_timeout must be between 50ms and 2s"))
		}
		if cfg.PromptSemanticsGatewayTimeout < 100*time.Millisecond || cfg.PromptSemanticsGatewayTimeout > 30*time.Second {
			errs = append(errs, errors.New("prompt_semantics_gateway_timeout must be between 100ms and 30s"))
		}
		if cfg.PromptSemanticsBlockThreshold <= 0 || cfg.PromptSemanticsBlockThreshold > 1 {
			errs = append(errs, errors.New("prompt_semantics_block_threshold must be between 0 and 1"))
		}
		if cfg.PromptSemanticsAlertThreshold <= 0 || cfg.PromptSemanticsAlertThreshold > 1 {
			errs = append(errs, errors.New("prompt_semantics_alert_threshold must be between 0 and 1"))
		}
		if cfg.PromptSemanticsAmbiguousThreshold <= 0 || cfg.PromptSemanticsAmbiguousThreshold > 1 {
			errs = append(errs, errors.New("prompt_semantics_ambiguous_threshold must be between 0 and 1"))
		}
		if cfg.PromptSemanticsAlertThreshold > cfg.PromptSemanticsBlockThreshold {
			errs = append(errs, errors.New("prompt_semantics_alert_threshold must be <= prompt_semantics_block_threshold"))
		}
		if cfg.PromptSemanticsAmbiguousThreshold > cfg.PromptSemanticsAlertThreshold {
			errs = append(errs, errors.New("prompt_semantics_ambiguous_threshold must be <= prompt_semantics_alert_threshold"))
		}
		if raw := strings.TrimSpace(cfg.PromptSemanticsLocalURL); raw != "" {
			u, err := url.Parse(raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("prompt_semantics_local_url: %w", err))
			} else if u.Scheme != "http" {
				errs = append(errs, errors.New("prompt_semantics_local_url must use http loopback"))
			} else {
				host := u.Hostname()
				ip := net.ParseIP(host)
				if ip == nil || !ip.IsLoopback() {
					errs = append(errs, errors.New("prompt_semantics_local_url host must be loopback"))
				}
			}
		}
	}

	cfg.PromptEnforcementMode = strings.ToLower(strings.TrimSpace(cfg.PromptEnforcementMode))
	switch cfg.PromptEnforcementMode {
	case domain.PromptEnforcementModeMonitor, domain.PromptEnforcementModeAlert, domain.PromptEnforcementModeEnforce:
	case "":
	default:
		errs = append(errs, errors.New("prompt_enforcement_mode must be monitor, alert, or enforce"))
	}
	cfg.PromptEnforcementOverride = strings.ToLower(strings.TrimSpace(cfg.PromptEnforcementOverride))
	switch cfg.PromptEnforcementOverride {
	case "", domain.PromptEnforcementModeMonitor, domain.PromptEnforcementModeAlert, domain.PromptEnforcementModeEnforce:
	default:
		errs = append(errs, errors.New("prompt_enforcement_override must be empty, monitor, alert, or enforce"))
	}
	seenSurfaces := map[domain.CaptureSurface]bool{}
	surfaces := make([]domain.CaptureSurface, 0, len(cfg.PromptFailClosedSurfaces))
	for _, surface := range cfg.PromptFailClosedSurfaces {
		if !surface.Valid() {
			errs = append(errs, fmt.Errorf("prompt_fail_closed_surfaces contains invalid surface %q", surface))
			continue
		}
		if seenSurfaces[surface] {
			continue
		}
		seenSurfaces[surface] = true
		surfaces = append(surfaces, surface)
	}
	cfg.PromptFailClosedSurfaces = surfaces

	if cfg.MaxConcurrentConns != 0 {
		if cfg.MaxConcurrentConns < 1 || cfg.MaxConcurrentConns > 65535 {
			errs = append(errs, errors.New("max_concurrent_conns must be between 1 and 65535"))
		}
	}

	// Bootstrap configs are allowed to start without credentials on disk so the
	// local status API can accept enrollment and finish setup later. If any
	// credential path is configured, require the full set of paths, but do not
	// require the files to already exist at config-validation time.
	if cfg.CertPath != "" || cfg.KeyPath != "" || cfg.CAPath != "" {
		for _, p := range []struct{ name, path string }{
			{"cert_path", cfg.CertPath},
			{"key_path", cfg.KeyPath},
			{"ca_path", cfg.CAPath},
		} {
			if p.path == "" {
				errs = append(errs, fmt.Errorf("%s is required when bootstrap cert paths are configured", p.name))
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
