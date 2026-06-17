package testutil

import (
	"encoding/json"
	"net"

	"github.com/themisto/agent/core/domain"
)

// TestConfig returns a minimal AgentConfig suitable for integration tests.
func TestConfig(gatewayURL, listenAddr string) *domain.AgentConfig {
	return &domain.AgentConfig{
		AgentID:         "test-agent-001",
		GatewayURL:      gatewayURL,
		ListenAddr:      listenAddr,
		DefaultDecision: domain.DecisionForward,
		AutoReregister:  true,
		LogURLPaths:     true,
	}
}

// TestConfigJSON returns a JSON-encoded config for use with config.Source.
func TestConfigJSON(gatewayURL, listenAddr string) []byte {
	cfg := map[string]interface{}{
		"agent_id":    "test-agent-001",
		"gateway_url": gatewayURL,
		"listen_addr": listenAddr,
	}
	data, _ := json.Marshal(cfg)
	return data
}

// TestPolicy returns the standard three-rule test policy.
func TestPolicy(version string) *domain.PolicyPayload {
	return &domain.PolicyPayload{
		Version: version,
		Rules: []domain.PolicyRule{
			{
				ID:       "rule-allow",
				Priority: 10,
				Decision: domain.DecisionForward,
				Conditions: []domain.RuleCondition{
					{Field: "host", Operator: "suffix", Value: ".example.com"},
				},
			},
			{
				ID:       "rule-block",
				Priority: 20,
				Decision: domain.DecisionBlock,
				Conditions: []domain.RuleCondition{
					{Field: "host", Operator: "suffix", Value: ".blocked.test"},
				},
			},
			{
				ID:       "rule-bypass",
				Priority: 30,
				Decision: domain.DecisionBypass,
				Conditions: []domain.RuleCondition{
					{Field: "host", Operator: "suffix", Value: ".bypass.test"},
				},
			},
		},
	}
}

// FreePort returns an available TCP port on loopback.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// MemSource implements config.Source for in-memory test configs.
type MemSource struct {
	Data []byte
}

func (m *MemSource) ConfigBytes() ([]byte, string, error) {
	return m.Data, "memory", nil
}
