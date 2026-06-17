package identity

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// IdentityStore implements the Store interface. It keeps mTLS credentials in
// memory and delegates persistence to the OS adapter's CertStore.
type IdentityStore struct {
	certStore iface.CertStore
	log       log.Logger

	mu   sync.RWMutex
	creds *credentials
}

type credentials struct {
	caID       string
	clientID   string
	certDER    []byte
	keyDER     []byte
	caDER      []byte // single DER cert (legacy)
	caChainPEM []byte // full PEM chain (preferred, may contain multiple certs)
}

// NewIdentityStore creates a store that persists certificates via cs.
func NewIdentityStore(cs iface.CertStore, logger log.Logger) *IdentityStore {
	return &IdentityStore{certStore: cs, log: logger}
}

// GetGatewayTLSConfig returns the current TLS material for mTLS. Returns an
// error if no credentials have been loaded.
func (s *IdentityStore) GetGatewayTLSConfig() (*domain.TLSConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.creds == nil {
		return nil, fmt.Errorf("identity: no credentials available")
	}
	return &domain.TLSConfig{
		CertDER:    s.creds.certDER,
		KeyDER:     s.creds.keyDER,
		CADER:      s.creds.caDER,
		CAChainPEM: s.creds.caChainPEM,
	}, nil
}

// RotateCredentials replaces the stored mTLS credentials. It installs the new
// CA into the OS trust store, then updates the in-memory state atomically.
func (s *IdentityStore) RotateCredentials(caID, clientID string, certDER, keyDER, caDER []byte) error {
	ctx := context.Background()

	if err := s.certStore.InstallCA(ctx, caID, caDER); err != nil {
		return fmt.Errorf("identity: install CA: %w", err)
	}

	// Convert the single CA DER to PEM so BuildTLSConfig can use AppendCertsFromPEM.
	caChainPEM := derCertToPEM(caDER)

	s.mu.Lock()
	old := s.creds
	s.creds = &credentials{
		caID:       caID,
		clientID:   clientID,
		certDER:    certDER,
		keyDER:     keyDER,
		caDER:      caDER,
		caChainPEM: caChainPEM,
	}
	s.mu.Unlock()

	if old != nil && old.caID != caID {
		if err := s.certStore.RemoveCA(ctx, old.caID); err != nil {
			s.log.Warn("identity: failed to remove old CA", "id", old.caID, "error", err)
		}
	}

	s.log.Info("credentials rotated", "ca_id", caID, "client_id", clientID)
	return nil
}

// Clear removes all credentials from the OS trust store and clears in-memory
// state.
func (s *IdentityStore) Clear() error {
	s.mu.Lock()
	creds := s.creds
	s.creds = nil
	s.mu.Unlock()

	if creds == nil {
		return nil
	}

	ctx := context.Background()
	if err := s.certStore.RemoveCA(ctx, creds.caID); err != nil {
		s.log.Warn("identity: failed to remove CA during clear", "id", creds.caID, "error", err)
	}

	s.log.Info("credentials cleared")
	return nil
}

// Bootstrap loads initial credentials into the store without touching the OS
// trust store (assumes the CA is already installed externally, e.g. by the
// installer). Used during first startup.
//
// Bootstrap loads initial credentials into the store without touching the OS
// trust store (assumes the CA is already installed externally, e.g. by the
// installer). Used during first startup.
//
// caChainPEM may contain multiple PEM-encoded certificates (e.g. issuing CA +
// root CA). Pass the full chain file contents here to ensure proper TLS
// verification against the gateway's server certificate.
func (s *IdentityStore) Bootstrap(caID, clientID string, certDER, keyDER, caChainPEM []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds = &credentials{
		caID:       caID,
		clientID:   clientID,
		certDER:    certDER,
		keyDER:     keyDER,
		caChainPEM: caChainPEM,
	}
}

// derCertToPEM encodes a single DER certificate as a PEM block.
func derCertToPEM(der []byte) []byte {
	// Validate it's a real certificate before encoding.
	if _, err := x509.ParseCertificate(der); err != nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
