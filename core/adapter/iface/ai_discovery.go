package iface

import (
	"context"

	"github.com/themisto/agent/core/domain"
)

// EndpointAIDiscoverer is an optional OS capability. Its observations must be
// aggregate-only and must never contain paths, command arguments, MCP payloads,
// credentials, prompt content, or inferred employee identity.
type EndpointAIDiscoverer interface {
	DiscoverEndpointAI(context.Context, []domain.AIProductCatalogEntry) ([]domain.EndpointAIObservation, error)
}
