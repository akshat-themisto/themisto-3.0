// Package policy defines policy sync and rule application.
package policy

import (
	"time"

	"github.com/themisto/agent/core/domain"
)

// Fetcher retrieves policy from the gateway.
type Fetcher interface {
	Fetch() (*domain.PolicyPayload, string, error)
	PollInterval() time.Duration
}

// Engine applies policy to requests (used by Routing).
type Engine interface {
	Apply(ctx *domain.RequestContext) (domain.Decision, string, error)
	Update(payload *domain.PolicyPayload) error
}
