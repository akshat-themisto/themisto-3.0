package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// Manager implements Provider. It loads configuration from a Source, validates
// it, applies defaults, and exposes the effective config via an atomic value
// for lock-free reads.
type Manager struct {
	source   Source
	log      log.Logger
	cfg      atomic.Value // *domain.AgentConfig
	mu       sync.Mutex
	watchers []func(*domain.AgentConfig)
}

// NewManager creates a Manager and performs the initial load-validate cycle.
// Returns an error if the initial configuration is invalid.
func NewManager(src Source, logger log.Logger) (*Manager, error) {
	m := &Manager{source: src, log: logger}
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("config: initial load: %w", err)
	}
	return m, nil
}

// Get returns the current effective configuration. The returned pointer must
// not be mutated by callers.
func (m *Manager) Get() *domain.AgentConfig {
	return m.cfg.Load().(*domain.AgentConfig)
}

// Watch registers a callback invoked after every successful config reload.
func (m *Manager) Watch(fn func(*domain.AgentConfig)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers = append(m.watchers, fn)
}

// Validate checks the current stored configuration.
func (m *Manager) Validate() error {
	return validate(m.Get())
}

// SourcePath returns the config file path if the source implements PathSource.
// Returns empty string otherwise.
func (m *Manager) SourcePath() string {
	if ps, ok := m.source.(PathSource); ok {
		return ps.Path()
	}
	return ""
}

// Reload re-reads the source and, on success, swaps the config and notifies
// watchers. On parse/validation failure the old config is retained.
func (m *Manager) Reload() error {
	if err := m.load(); err != nil {
		m.log.Warn("config reload failed, keeping previous config", "error", err)
		return err
	}
	m.log.Info("config reloaded successfully")
	return nil
}

// jsonDuration wraps time.Duration so it unmarshals from JSON strings like "60s".
type jsonDuration time.Duration

func (d *jsonDuration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = jsonDuration(dur)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("duration must be a string or number, got %s", string(b))
	}
	*d = jsonDuration(time.Duration(n))
	return nil
}

// jsonDecision wraps Decision so it unmarshals from JSON strings like "forward".
type jsonDecision domain.Decision

func (d *jsonDecision) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("decision must be a string, got %s", string(b))
	}
	switch strings.ToLower(s) {
	case "forward":
		*d = jsonDecision(domain.DecisionForward)
	case "bypass":
		*d = jsonDecision(domain.DecisionBypass)
	case "block":
		*d = jsonDecision(domain.DecisionBlock)
	default:
		return fmt.Errorf("unknown decision %q", s)
	}
	return nil
}

// jsonAgentConfig is the JSON-friendly mirror of domain.AgentConfig. Duration
// fields arrive as strings ("60s") and Decision as "forward"/"bypass"/"block".
type jsonAgentConfig struct {
	AgentID                 string `json:"agent_id"`
	GatewayURL              string `json:"gateway_url"`
	ListenAddr              string `json:"listen_addr"`
	PromptCaptureListenAddr string `json:"prompt_capture_listen_addr"`

	HTTPSInterceptEnabled     bool     `json:"https_intercept_enabled"`
	HTTPSInterceptDomains     []string `json:"https_intercept_domains"`
	HTTPSInterceptFailMode    string   `json:"https_intercept_fail_mode"`
	HTTPSInterceptProtocols   []string `json:"https_intercept_protocols"`
	HTTPSInterceptCaptureMode string   `json:"https_intercept_capture_mode"`

	PolicySyncInterval     jsonDuration `json:"policy_sync_interval"`
	TelemetryFlushInterval jsonDuration `json:"telemetry_flush_interval"`
	MaxConcurrentConns     int          `json:"max_concurrent_conns"`
	ShutdownTimeout        jsonDuration `json:"shutdown_timeout"`

	DefaultDecision jsonDecision `json:"default_decision"`
	BlockPageBody   string       `json:"block_page_body"`
	LogURLPaths     bool         `json:"log_url_paths"`
	AutoReregister  bool         `json:"auto_reregister"`

	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
	CAPath   string `json:"ca_path"`

	InitialBackoff          jsonDuration `json:"initial_backoff"`
	MaxBackoff              jsonDuration `json:"max_backoff"`
	BackoffMultiplier       float64      `json:"backoff_multiplier"`
	JitterFraction          float64      `json:"jitter_fraction"`
	MaxRetries              int          `json:"max_retries"`
	CircuitBreakerThreshold int          `json:"circuit_breaker_threshold"`
	CircuitBreakerCooldown  jsonDuration `json:"circuit_breaker_cooldown"`

	HealthCheckInterval    jsonDuration `json:"health_check_interval"`
	IntegrityCheckInterval jsonDuration `json:"integrity_check_interval"`

	PromptSemanticsEnabled            bool         `json:"prompt_semantics_enabled"`
	PromptSemanticsPolicy             string       `json:"prompt_semantics_policy"`
	PromptSemanticsLocalURL           string       `json:"prompt_semantics_local_url"`
	PromptSemanticsGatewayEnabled     bool         `json:"prompt_semantics_gateway_enabled"`
	PromptSemanticsLocalTimeout       jsonDuration `json:"prompt_semantics_local_timeout"`
	PromptSemanticsGatewayTimeout     jsonDuration `json:"prompt_semantics_gateway_timeout"`
	PromptSemanticsBlockThreshold     float64      `json:"prompt_semantics_block_threshold"`
	PromptSemanticsAlertThreshold     float64      `json:"prompt_semantics_alert_threshold"`
	PromptSemanticsAmbiguousThreshold float64      `json:"prompt_semantics_ambiguous_threshold"`

	PromptEnforcementMode     string                  `json:"prompt_enforcement_mode"`
	PromptFailClosedSurfaces  []domain.CaptureSurface `json:"prompt_fail_closed_surfaces"`
	PromptEnforcementOverride string                  `json:"prompt_enforcement_override"`
}

func (j *jsonAgentConfig) toDomain() *domain.AgentConfig {
	return &domain.AgentConfig{
		AgentID:                           j.AgentID,
		GatewayURL:                        j.GatewayURL,
		ListenAddr:                        j.ListenAddr,
		PromptCaptureListenAddr:           j.PromptCaptureListenAddr,
		HTTPSInterceptEnabled:             j.HTTPSInterceptEnabled,
		HTTPSInterceptDomains:             j.HTTPSInterceptDomains,
		HTTPSInterceptFailMode:            j.HTTPSInterceptFailMode,
		HTTPSInterceptProtocols:           j.HTTPSInterceptProtocols,
		HTTPSInterceptCaptureMode:         j.HTTPSInterceptCaptureMode,
		PolicySyncInterval:                time.Duration(j.PolicySyncInterval),
		TelemetryFlushInterval:            time.Duration(j.TelemetryFlushInterval),
		MaxConcurrentConns:                j.MaxConcurrentConns,
		ShutdownTimeout:                   time.Duration(j.ShutdownTimeout),
		DefaultDecision:                   domain.Decision(j.DefaultDecision),
		BlockPageBody:                     j.BlockPageBody,
		LogURLPaths:                       j.LogURLPaths,
		AutoReregister:                    j.AutoReregister,
		CertPath:                          j.CertPath,
		KeyPath:                           j.KeyPath,
		CAPath:                            j.CAPath,
		InitialBackoff:                    time.Duration(j.InitialBackoff),
		MaxBackoff:                        time.Duration(j.MaxBackoff),
		BackoffMultiplier:                 j.BackoffMultiplier,
		JitterFraction:                    j.JitterFraction,
		MaxRetries:                        j.MaxRetries,
		CircuitBreakerThreshold:           j.CircuitBreakerThreshold,
		CircuitBreakerCooldown:            time.Duration(j.CircuitBreakerCooldown),
		HealthCheckInterval:               time.Duration(j.HealthCheckInterval),
		IntegrityCheckInterval:            time.Duration(j.IntegrityCheckInterval),
		PromptSemanticsEnabled:            j.PromptSemanticsEnabled,
		PromptSemanticsPolicy:             j.PromptSemanticsPolicy,
		PromptSemanticsLocalURL:           j.PromptSemanticsLocalURL,
		PromptSemanticsGatewayEnabled:     j.PromptSemanticsGatewayEnabled,
		PromptSemanticsLocalTimeout:       time.Duration(j.PromptSemanticsLocalTimeout),
		PromptSemanticsGatewayTimeout:     time.Duration(j.PromptSemanticsGatewayTimeout),
		PromptSemanticsBlockThreshold:     j.PromptSemanticsBlockThreshold,
		PromptSemanticsAlertThreshold:     j.PromptSemanticsAlertThreshold,
		PromptSemanticsAmbiguousThreshold: j.PromptSemanticsAmbiguousThreshold,
		PromptEnforcementMode:             j.PromptEnforcementMode,
		PromptFailClosedSurfaces:          j.PromptFailClosedSurfaces,
		PromptEnforcementOverride:         j.PromptEnforcementOverride,
	}
}

func (m *Manager) load() error {
	raw, origin, err := m.source.ConfigBytes()
	if err != nil {
		return fmt.Errorf("read config from %s: %w", origin, err)
	}

	var jcfg jsonAgentConfig
	if err := json.Unmarshal(raw, &jcfg); err != nil {
		return fmt.Errorf("parse config from %s: %w", origin, err)
	}

	cfg := jcfg.toDomain()
	applyDefaults(cfg)

	if err := validate(cfg); err != nil {
		return fmt.Errorf("validate config from %s: %w", origin, err)
	}

	m.cfg.Store(cfg)
	m.notifyWatchers(cfg)
	return nil
}

func (m *Manager) notifyWatchers(cfg *domain.AgentConfig) {
	m.mu.Lock()
	watchers := make([]func(*domain.AgentConfig), len(m.watchers))
	copy(watchers, m.watchers)
	m.mu.Unlock()

	for _, fn := range watchers {
		fn(cfg)
	}
}
