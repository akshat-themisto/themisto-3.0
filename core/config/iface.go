// Package config defines configuration contract for the agent.
package config

import "github.com/themisto/agent/core/domain"

// Source provides raw configuration bytes and origin. Implemented in cmd or a small loader.
// ConfigBytes returns (data, origin-label, error). The origin label is typically
// the file path but may be any human-readable identifier.
type Source interface {
	ConfigBytes() ([]byte, string, error)
}

// PathSource is an optional interface that Sources can implement to expose
// the underlying file path. Used by the status API to write config updates.
type PathSource interface {
	Path() string
}

// Provider exposes validated configuration to the rest of the core.
type Provider interface {
	Get() *domain.AgentConfig
	Watch(OnChange func(*domain.AgentConfig))
	Validate() error
}
