package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

type staticSource struct {
	data   []byte
	origin string
}

func (s *staticSource) ConfigBytes() ([]byte, string, error) {
	return s.data, s.origin, nil
}

func TestJsonDuration_UnmarshalString(t *testing.T) {
	var d jsonDuration
	if err := json.Unmarshal([]byte(`"30s"`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if time.Duration(d) != 30*time.Second {
		t.Errorf("got %v, want 30s", time.Duration(d))
	}
}

func TestJsonDuration_UnmarshalNumber(t *testing.T) {
	var d jsonDuration
	if err := json.Unmarshal([]byte(`1000000000`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if time.Duration(d) != time.Second {
		t.Errorf("got %v, want 1s", time.Duration(d))
	}
}

func TestJsonDuration_UnmarshalInvalid(t *testing.T) {
	var d jsonDuration
	if err := json.Unmarshal([]byte(`"not-a-dur"`), &d); err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestJsonDecision_Unmarshal(t *testing.T) {
	tests := []struct {
		input string
		want  domain.Decision
	}{
		{`"forward"`, domain.DecisionForward},
		{`"bypass"`, domain.DecisionBypass},
		{`"block"`, domain.DecisionBlock},
	}
	for _, tt := range tests {
		var d jsonDecision
		if err := json.Unmarshal([]byte(tt.input), &d); err != nil {
			t.Errorf("unmarshal %s: %v", tt.input, err)
			continue
		}
		if domain.Decision(d) != tt.want {
			t.Errorf("got %v, want %v", domain.Decision(d), tt.want)
		}
	}
}

func TestJsonDecision_UnmarshalInvalid(t *testing.T) {
	var d jsonDecision
	if err := json.Unmarshal([]byte(`"unknown"`), &d); err == nil {
		t.Error("expected error for unknown decision")
	}
}

func TestManager_LoadWithDurations(t *testing.T) {
	cfg := map[string]interface{}{
		"agent_id":              "test-agent",
		"gateway_url":           "https://gw.example.com",
		"listen_addr":           "127.0.0.1:9090",
		"policy_sync_interval":  "60s",
		"telemetry_flush_interval": "30s",
		"max_concurrent_conns":  100,
		"shutdown_timeout":      "10s",
		"default_decision":      "forward",
		"initial_backoff":       "1s",
		"max_backoff":           "60s",
		"backoff_multiplier":    2.0,
		"jitter_fraction":       0.2,
		"max_retries":           5,
		"circuit_breaker_threshold": 10,
		"circuit_breaker_cooldown":  "120s",
		"health_check_interval":     "30s",
		"integrity_check_interval":  "30s",
	}
	data, _ := json.Marshal(cfg)

	src := &staticSource{data: data, origin: "test"}
	mgr, err := NewManager(src, &noopLogger{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	got := mgr.Get()
	if got.PolicySyncInterval != 60*time.Second {
		t.Errorf("PolicySyncInterval = %v, want 60s", got.PolicySyncInterval)
	}
	if got.DefaultDecision != domain.DecisionForward {
		t.Errorf("DefaultDecision = %v, want forward", got.DefaultDecision)
	}
	if got.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9090", got.ListenAddr)
	}
}

type noopLogger struct{}

func (l *noopLogger) Debug(_ string, _ ...interface{}) {}
func (l *noopLogger) Info(_ string, _ ...interface{})  {}
func (l *noopLogger) Warn(_ string, _ ...interface{})  {}
func (l *noopLogger) Error(_ string, _ ...interface{}) {}
func (l *noopLogger) With(_ ...interface{}) log.Logger  { return l }
