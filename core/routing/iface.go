// Package routing defines request classification and forwarding decisions.
package routing

import "github.com/themisto/agent/core/domain"

// Router decides how to handle each request (forward via gateway, bypass, or block).
type Router interface {
	Route(ctx *domain.RequestContext) (domain.Decision, string, error)
}
