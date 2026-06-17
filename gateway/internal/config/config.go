package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server          ServerConfig          `yaml:"server"`
	TLS             TLSConfig             `yaml:"tls"`
	Revocation      RevocationConfig      `yaml:"revocation"`
	ControlPlane    ControlPlaneConfig    `yaml:"control_plane"`
	Upstream        UpstreamConfig        `yaml:"upstream"`
	PromptSemantics PromptSemanticsConfig `yaml:"prompt_semantics"`
	Telemetry       TelemetryConfig       `yaml:"telemetry"`
	Logging         LogConfig             `yaml:"logging"`

	DBDSN         string `yaml:"-"`
	InternalToken string `yaml:"-"`
}

type ServerConfig struct {
	ListenAddr   string        `yaml:"listen_addr"`
	HealthAddr   string        `yaml:"health_addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

type TLSConfig struct {
	CertPath    string `yaml:"cert_path"`
	KeyPath     string `yaml:"key_path"`
	CAChainPath string `yaml:"ca_chain_path"`
}

type RevocationConfig struct {
	BackendURL      string        `yaml:"backend_url"`
	CacheTTL        time.Duration `yaml:"cache_ttl"`
	CacheMaxEntries int           `yaml:"cache_max_entries"`
	FailMode        string        `yaml:"fail_mode"`
}

type ControlPlaneConfig struct {
	URL      string        `yaml:"url"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
	FailMode string        `yaml:"fail_mode"`
	Token    string        `yaml:"-"`
}

type UpstreamConfig struct {
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

type PromptSemanticsConfig struct {
	Enabled    bool          `yaml:"enabled"`
	QwenURL    string        `yaml:"qwen_url"`
	QwenAPIKey string        `yaml:"-"`
	Timeout    time.Duration `yaml:"timeout"`
}

type TelemetryConfig struct {
	BufferSize     int           `yaml:"buffer_size"`
	FlushInterval  time.Duration `yaml:"flush_interval"`
	FlushBatchSize int           `yaml:"flush_batch_size"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			ListenAddr:   ":8443",
			HealthAddr:   ":8080",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 120 * time.Second,
			IdleTimeout:  300 * time.Second,
		},
		Revocation: RevocationConfig{
			CacheTTL:        60 * time.Second,
			CacheMaxEntries: 10000,
			FailMode:        "closed",
		},
		ControlPlane: ControlPlaneConfig{
			CacheTTL: 30 * time.Second,
			FailMode: "closed",
		},
		Upstream: UpstreamConfig{
			RequestTimeout: 120 * time.Second,
		},
		PromptSemantics: PromptSemanticsConfig{
			Timeout: 900 * time.Millisecond,
		},
		Telemetry: TelemetryConfig{
			BufferSize:     10000,
			FlushInterval:  time.Second,
			FlushBatchSize: 500,
		},
		Logging: LogConfig{Level: "info", Format: "json"},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.DBDSN = os.Getenv("DB_DSN")
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}

	cfg.InternalToken = os.Getenv("INTERNAL_TOKEN")
	if v := os.Getenv("CONTROL_PLANE_URL"); v != "" {
		cfg.ControlPlane.URL = v
	}
	if v := os.Getenv("CONTROL_PLANE_TOKEN"); v != "" {
		cfg.ControlPlane.Token = v
	}
	if v := os.Getenv("CONTROL_PLANE_FAIL_MODE"); v != "" {
		cfg.ControlPlane.FailMode = v
	}
	if raw := os.Getenv("CONTROL_PLANE_CACHE_TTL"); raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid CONTROL_PLANE_CACHE_TTL value: %q", raw)
		}
		cfg.ControlPlane.CacheTTL = dur
	}
	if v := os.Getenv("PROMPT_SEMANTICS_ENABLED"); v != "" {
		cfg.PromptSemantics.Enabled = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("QWEN_CLASSIFIER_URL"); v != "" {
		cfg.PromptSemantics.QwenURL = v
	}
	if v := os.Getenv("QWEN_CLASSIFIER_API_KEY"); v != "" {
		cfg.PromptSemantics.QwenAPIKey = v
	}
	if raw := os.Getenv("QWEN_CLASSIFIER_TIMEOUT"); raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid QWEN_CLASSIFIER_TIMEOUT value: %q", raw)
		}
		cfg.PromptSemantics.Timeout = dur
	}

	return cfg, nil
}
