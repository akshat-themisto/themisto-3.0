// Package unified implements a merged agent+gateway for development.
// All enforcement logic runs in-process with no network separation.
package unified

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/themisto/agent/core/domain"
)

// DevConfig holds settings for the unified development service.
// It is intentionally simpler than domain.AgentConfig — no mTLS, no gateway URL.
type DevConfig struct {
	ListenAddr string `json:"listen_addr"` // default "127.0.0.1:8080"
	LogOutput  string `json:"log_output"`  // "stdout" or file path
	LogJSON    bool   `json:"log_json"`    // structured JSON logs
	PolicyFile string `json:"policy_file"` // path to policy JSON (optional)

	BlockPageBody string `json:"block_page_body"`

	// TODO(mTLS): Add GatewayURL, AgentID, TLS cert paths when splitting
	// back to agent+gateway architecture.

	// TODO(identity): Add client cert, CA cert, key paths for mTLS
	// enrollment and rotation.
}

// DefaultDevConfig returns sane defaults for local development.
func DefaultDevConfig() *DevConfig {
	return &DevConfig{
		ListenAddr: "127.0.0.1:8080",
		LogOutput:  "stdout",
		LogJSON:    true,
		BlockPageBody: `<!DOCTYPE html>
<html><head><title>Blocked</title></head>
<body><h1>Request Blocked</h1>
<p>This request was blocked by the traffic governance policy.</p>
</body></html>`,
	}
}

// LoadDevConfig reads config from a JSON file, falling back to defaults.
func LoadDevConfig(path string) (*DevConfig, error) {
	cfg := DefaultDevConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8080"
	}
	return cfg, nil
}

// LoadPolicyFromFile loads a PolicyPayload from a JSON file.
// Returns a default allow-all policy if no file is specified.
func LoadPolicyFromFile(path string) (*domain.PolicyPayload, error) {
	if path == "" {
		return defaultDevPolicy(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	var p domain.PolicyPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return &p, nil
}

func defaultDevPolicy() *domain.PolicyPayload {
	return &domain.PolicyPayload{
		Version: "dev-" + time.Now().Format("20060102"),
		Rules:   nil, // no rules → default decision (ALLOW) applies to everything
	}
}
