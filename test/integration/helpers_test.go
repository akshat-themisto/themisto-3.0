package integration

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

// nopLogger satisfies pkg/log.Logger with no output, for test use.
type nopLogger struct{}

func (l *nopLogger) Debug(_ string, _ ...interface{}) {}
func (l *nopLogger) Info(_ string, _ ...interface{})  {}
func (l *nopLogger) Warn(_ string, _ ...interface{})  {}
func (l *nopLogger) Error(_ string, _ ...interface{}) {}
func (l *nopLogger) With(_ ...interface{}) log.Logger  { return l }

// marshalECKey serializes an ECDSA private key to DER.
func marshalECKey(key *ecdsa.PrivateKey) ([]byte, error) {
	return x509.MarshalECPrivateKey(key)
}

// domainTLS creates a domain.TLSConfig from raw DER fields.
func domainTLS(certDER, keyDER, caDER []byte) *domain.TLSConfig {
	caChainPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	return &domain.TLSConfig{
		CertDER:    certDER,
		KeyDER:     keyDER,
		CADER:      caDER,
		CAChainPEM: caChainPEM,
	}
}
