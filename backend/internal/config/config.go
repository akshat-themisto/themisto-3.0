package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Signing      SigningConfig      `yaml:"signing"`
	Token        TokenConfig        `yaml:"token"`
	Logging      LogConfig          `yaml:"logging"`
	ControlPlane ControlPlaneConfig `yaml:"control_plane"`

	Public PublicConfig `yaml:"public"`

	DBDSN       string `yaml:"-"`
	AdminAPIKey string `yaml:"-"`

	DLPBodyRetentionDays int      `yaml:"-"`
	CORSAllowedOrigins   []string `yaml:"-"`
}

type ServerConfig struct {
	ListenAddr   string        `yaml:"listen_addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type SigningConfig struct {
	CACertPath       string `yaml:"ca_cert_path"`
	CAKeyPath        string `yaml:"ca_key_path"`
	CAChainPath      string `yaml:"ca_chain_path"`
	CertValidityDays int    `yaml:"cert_validity_days"`
}

type TokenConfig struct {
	ExpiryHours int `yaml:"expiry_hours"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type PublicConfig struct {
	BackendURL string `yaml:"backend_url"`
	GatewayURL string `yaml:"gateway_url"`
}

type ControlPlaneConfig struct {
	URL      string        `yaml:"url"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
	FailMode string        `yaml:"fail_mode"`
	Token    string        `yaml:"-"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			ListenAddr:   ":8443",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		Signing: SigningConfig{
			CertValidityDays: 90,
		},
		Token: TokenConfig{
			ExpiryHours: 24,
		},
		Logging: LogConfig{
			Level:  "info",
			Format: "json",
		},
		ControlPlane: ControlPlaneConfig{
			CacheTTL: 30 * time.Second,
			FailMode: "closed",
		},
		Public: PublicConfig{
			BackendURL: "https://localhost:8443",
			GatewayURL: "https://localhost:443",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.DBDSN = os.Getenv("DB_DSN")
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}

	cfg.AdminAPIKey = os.Getenv("ADMIN_API_KEY")
	if cfg.AdminAPIKey == "" {
		return nil, fmt.Errorf("ADMIN_API_KEY environment variable is required")
	}

	if v := os.Getenv("PUBLIC_BACKEND_URL"); v != "" {
		cfg.Public.BackendURL = v
	}
	if v := os.Getenv("PUBLIC_GATEWAY_URL"); v != "" {
		cfg.Public.GatewayURL = v
	}
	if v := os.Getenv("CONTROL_PLANE_URL"); v != "" {
		cfg.ControlPlane.URL = v
	}
	if v := os.Getenv("CONTROL_PLANE_TOKEN"); v != "" {
		cfg.ControlPlane.Token = v
	}
	if v := os.Getenv("CONTROL_PLANE_FAIL_MODE"); v != "" {
		cfg.ControlPlane.FailMode = strings.ToLower(strings.TrimSpace(v))
	}
	if raw := os.Getenv("CONTROL_PLANE_CACHE_TTL"); raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid CONTROL_PLANE_CACHE_TTL value: %q", raw)
		}
		cfg.ControlPlane.CacheTTL = dur
	}

	cfg.DLPBodyRetentionDays = 30
	if raw := os.Getenv("DLP_BODY_RETENTION_DAYS"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days <= 0 {
			return nil, fmt.Errorf("invalid DLP_BODY_RETENTION_DAYS value: %q", raw)
		}
		cfg.DLPBodyRetentionDays = days
	}

	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
			}
		}
	}

	return cfg, nil
}
