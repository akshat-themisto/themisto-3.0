package signing

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

type Signer struct {
	CACert       *x509.Certificate
	CAKey        *ecdsa.PrivateKey
	CAChainPEM   []byte
	ValidityDays int
}

func NewSigner(certPath, keyPath, chainPath string, validityDays int) (*Signer, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in CA cert")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read CA key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("no PEM block in CA key")
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}

	chainPEM, err := os.ReadFile(chainPath)
	if err != nil {
		return nil, fmt.Errorf("read CA chain: %w", err)
	}

	return &Signer{
		CACert:       caCert,
		CAKey:        caKey,
		CAChainPEM:   chainPEM,
		ValidityDays: validityDays,
	}, nil
}

type SignResult struct {
	CertPEM   string
	ChainPEM  string
	Serial    string
	NotBefore time.Time
	NotAfter  time.Time
}

func (s *Signer) SignCSR(csrDER []byte) (*SignResult, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}

	serialBytes := make([]byte, 16)
	if _, err := rand.Read(serialBytes); err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	serialNumber := new(big.Int).SetBytes(serialBytes)

	now := time.Now().UTC()
	notAfter := now.Add(time.Duration(s.ValidityDays) * 24 * time.Hour)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   csr.Subject.CommonName,
			Organization: csr.Subject.Organization,
		},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, s.CACert, csr.PublicKey, s.CAKey)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return &SignResult{
		CertPEM:   string(certPEM),
		ChainPEM:  string(s.CAChainPEM),
		Serial:    hex.EncodeToString(serialBytes),
		NotBefore: now,
		NotAfter:  notAfter,
	}, nil
}
