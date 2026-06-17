// Package transport defines the local proxy and gateway relay.
package transport

import (
	"context"

	"github.com/themisto/agent/core/domain"
)

// LocalProxy is the HTTP/HTTPS proxy server that receives traffic from the OS.
type LocalProxy interface {
	Listen(ctx context.Context, addr string) error
	Stop() error
}

// GatewayClient performs mTLS requests to the remote gateway.
type GatewayClient interface {
	Relay(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error)
	Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error)
	Close() error
}
