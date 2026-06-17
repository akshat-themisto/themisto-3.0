package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

type memSource struct {
	data []byte
}

func (m *memSource) ConfigBytes() ([]byte, string, error) {
	return m.data, "test", nil
}

type nopLogger struct{}

func (l *nopLogger) Debug(_ string, _ ...interface{}) {}
func (l *nopLogger) Info(_ string, _ ...interface{})  {}
func (l *nopLogger) Warn(_ string, _ ...interface{})  {}
func (l *nopLogger) Error(_ string, _ ...interface{}) {}
func (l *nopLogger) With(_ ...interface{}) log.Logger { return l }

func validConfig() map[string]interface{} {
	return map[string]interface{}{
		"agent_id":    "test-001",
		"gateway_url": "https://gateway.test:443",
		"listen_addr": "127.0.0.1:8080",
	}
}

func TestNewManager_ValidConfig(t *testing.T) {
	data, _ := json.Marshal(validConfig())
	mgr, err := NewManager(&memSource{data: data}, &nopLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := mgr.Get()
	if cfg.AgentID != "test-001" {
		t.Errorf("AgentID = %q, want test-001", cfg.AgentID)
	}
	if cfg.GatewayURL != "https://gateway.test:443" {
		t.Errorf("GatewayURL = %q", cfg.GatewayURL)
	}
}

func TestNewManager_DefaultsApplied(t *testing.T) {
	data, _ := json.Marshal(validConfig())
	mgr, _ := NewManager(&memSource{data: data}, &nopLogger{})
	cfg := mgr.Get()

	if cfg.PolicySyncInterval != 60*time.Second {
		t.Errorf("PolicySyncInterval = %v, want 60s", cfg.PolicySyncInterval)
	}
	if cfg.MaxConcurrentConns != 1000 {
		t.Errorf("MaxConcurrentConns = %d, want 1000", cfg.MaxConcurrentConns)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", cfg.ShutdownTimeout)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", cfg.BackoffMultiplier)
	}
	if cfg.BlockPageBody == "" {
		t.Error("BlockPageBody should have default")
	}
	if cfg.HTTPSInterceptFailMode != domain.HTTPSInterceptFailOpen {
		t.Errorf("HTTPSInterceptFailMode = %q, want %q", cfg.HTTPSInterceptFailMode, domain.HTTPSInterceptFailOpen)
	}
	if cfg.HTTPSInterceptCaptureMode != domain.InterceptCaptureModeEncryptedFullBody {
		t.Errorf("HTTPSInterceptCaptureMode = %q, want %q", cfg.HTTPSInterceptCaptureMode, domain.InterceptCaptureModeEncryptedFullBody)
	}
	if len(cfg.HTTPSInterceptProtocols) != 1 || cfg.HTTPSInterceptProtocols[0] != domain.InterceptProtocolHTTP {
		t.Errorf("HTTPSInterceptProtocols = %#v, want [http]", cfg.HTTPSInterceptProtocols)
	}
}

func TestNewManager_AllowsBootstrapConfigWithoutAgentID(t *testing.T) {
	cfg := validConfig()
	delete(cfg, "agent_id")
	data, _ := json.Marshal(cfg)
	mgr, err := NewManager(&memSource{data: data}, &nopLogger{})
	if err != nil {
		t.Fatalf("unexpected error for bootstrap config: %v", err)
	}
	if got := mgr.Get().AgentID; got != "" {
		t.Fatalf("AgentID = %q, want empty bootstrap value", got)
	}
}

func TestNewManager_AllowsBootstrapConfigWithoutGatewayURL(t *testing.T) {
	cfg := validConfig()
	delete(cfg, "gateway_url")
	data, _ := json.Marshal(cfg)
	mgr, err := NewManager(&memSource{data: data}, &nopLogger{})
	if err != nil {
		t.Fatalf("unexpected error for bootstrap config: %v", err)
	}
	if got := mgr.Get().GatewayURL; got != "" {
		t.Fatalf("GatewayURL = %q, want empty bootstrap value", got)
	}
}

func TestNewManager_NonHTTPSGateway(t *testing.T) {
	cfg := validConfig()
	cfg["gateway_url"] = "http://insecure.test"
	data, _ := json.Marshal(cfg)
	_, err := NewManager(&memSource{data: data}, &nopLogger{})
	if err == nil {
		t.Fatal("expected error for non-HTTPS gateway")
	}
}

func TestNewManager_NonLoopbackListenAddr(t *testing.T) {
	cfg := validConfig()
	cfg["listen_addr"] = "0.0.0.0:8080"
	data, _ := json.Marshal(cfg)
	_, err := NewManager(&memSource{data: data}, &nopLogger{})
	if err == nil {
		t.Fatal("expected error for non-loopback listen address")
	}
}

func TestNewManager_InvalidJSON(t *testing.T) {
	_, err := NewManager(&memSource{data: []byte("{invalid")}, &nopLogger{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestManager_Watch(t *testing.T) {
	data, _ := json.Marshal(validConfig())
	mgr, _ := NewManager(&memSource{data: data}, &nopLogger{})

	var notified *domain.AgentConfig
	mgr.Watch(func(cfg *domain.AgentConfig) {
		notified = cfg
	})

	// Initial load already happened, watcher not called for it.
	// Trigger reload.
	mgr.Reload()

	if notified == nil {
		t.Fatal("watcher was not notified on reload")
	}
	if notified.AgentID != "test-001" {
		t.Errorf("notified config AgentID = %q", notified.AgentID)
	}
}

func TestValidate_PolicySyncIntervalRange(t *testing.T) {
	cfg := &domain.AgentConfig{
		AgentID:            "x",
		GatewayURL:         "https://gw.test",
		ListenAddr:         "127.0.0.1:8080",
		PolicySyncInterval: 1 * time.Second, // too low
	}
	if err := validate(cfg); err == nil {
		t.Error("expected error for PolicySyncInterval < 10s")
	}
}

func TestValidate_HTTPSInterceptFailMode(t *testing.T) {
	cfg := &domain.AgentConfig{
		AgentID:                   "x",
		GatewayURL:                "https://gw.test",
		ListenAddr:                "127.0.0.1:8080",
		HTTPSInterceptFailMode:    "invalid",
		HTTPSInterceptProtocols:   []string{domain.InterceptProtocolHTTP},
		HTTPSInterceptCaptureMode: domain.InterceptCaptureModeEncryptedFullBody,
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for invalid https_intercept_fail_mode")
	}
}

func TestValidate_HTTPSInterceptProtocols(t *testing.T) {
	cfg := &domain.AgentConfig{
		AgentID:                   "x",
		GatewayURL:                "https://gw.test",
		ListenAddr:                "127.0.0.1:8080",
		HTTPSInterceptFailMode:    domain.HTTPSInterceptFailOpen,
		HTTPSInterceptProtocols:   []string{"http", "grpc", "http"},
		HTTPSInterceptCaptureMode: domain.InterceptCaptureModeEncryptedFullBody,
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if len(cfg.HTTPSInterceptProtocols) != 2 {
		t.Fatalf("expected deduped protocols, got %#v", cfg.HTTPSInterceptProtocols)
	}
}

func TestValidate_AllowsMissingCredentialFilesDuringBootstrap(t *testing.T) {
	cfg := &domain.AgentConfig{
		AgentID:                   "",
		GatewayURL:                "",
		ListenAddr:                "127.0.0.1:8080",
		HTTPSInterceptFailMode:    domain.HTTPSInterceptFailOpen,
		HTTPSInterceptProtocols:   []string{domain.InterceptProtocolHTTP},
		HTTPSInterceptCaptureMode: domain.InterceptCaptureModeEncryptedFullBody,
		CertPath:                  `C:\ProgramData\Themisto\certs\device.crt`,
		KeyPath:                   `C:\ProgramData\Themisto\certs\device.key`,
		CAPath:                    `C:\ProgramData\Themisto\certs\ca-chain.pem`,
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("expected bootstrap config to validate, got %v", err)
	}
}
