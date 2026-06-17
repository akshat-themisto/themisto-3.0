package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func NewTLSConfig(certPath, keyPath, caChainPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(caChainPath)
	if err != nil {
		return nil, fmt.Errorf("read CA chain: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid certificates in CA chain file")
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}, nil
}
