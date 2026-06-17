// Package identity defines how the agent obtains and uses mTLS identity.
package identity

import "github.com/themisto/agent/core/domain"

// Store is implemented by core; it uses iface.CertStore for persistence.
type Store interface {
	GetGatewayTLSConfig() (*domain.TLSConfig, error)
	RotateCredentials(caID, clientID string, certDER, keyDER, caDER []byte) error
	Clear() error
}
