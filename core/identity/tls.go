package identity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/themisto/agent/core/domain"
)

// BuildTLSConfig constructs a *tls.Config suitable for mTLS connections to
// the gateway from the raw DER material in cfg.
func BuildTLSConfig(cfg *domain.TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("identity/tls: nil TLSConfig")
	}

	cert, err := x509.ParseCertificate(cfg.CertDER)
	if err != nil {
		return nil, fmt.Errorf("identity/tls: parse client certificate: %w", err)
	}

	key, err := x509.ParsePKCS8PrivateKey(cfg.KeyDER)
	if err != nil {
		// Try PKCS1 (RSA) and EC fallbacks.
		key, err = x509.ParsePKCS1PrivateKey(cfg.KeyDER)
		if err != nil {
			key, err = x509.ParseECPrivateKey(cfg.KeyDER)
			if err != nil {
				return nil, fmt.Errorf("identity/tls: parse private key: %w", err)
			}
		}
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{cfg.CertDER},
		PrivateKey:  key,
		Leaf:        cert,
	}

	caPool := x509.NewCertPool()
	if len(cfg.CAChainPEM) > 0 {
		if !caPool.AppendCertsFromPEM(cfg.CAChainPEM) {
			return nil, fmt.Errorf("identity/tls: no valid certificates found in CA chain PEM")
		}
	} else if len(cfg.CADER) > 0 {
		caCert, err := x509.ParseCertificate(cfg.CADER)
		if err != nil {
			return nil, fmt.Errorf("identity/tls: parse CA certificate: %w", err)
		}
		caPool.AddCert(caCert)
	} else {
		return nil, fmt.Errorf("identity/tls: no CA certificate provided")
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{tlsCert},
		RootCAs:            caPool,
		InsecureSkipVerify: false,
	}, nil
}
