package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

const (
	interceptCAID       = "themisto-https-intercept-ca-v1"
	interceptCACertFile = "intercept_ca_cert.pem"
	interceptCAKeyFile  = "intercept_ca_key.pem"
)

type cachedLeafCert struct {
	cert    tls.Certificate
	expires time.Time
}

type interceptCertManager struct {
	certStore iface.CertStore
	log       log.Logger
	storage   string

	mu      sync.Mutex
	caCert  tls.Certificate
	caLeaf  *x509.Certificate
	leafMap map[string]cachedLeafCert
	ready   bool
}

func newInterceptCertManager(certStore iface.CertStore, logger log.Logger) *interceptCertManager {
	return &interceptCertManager{
		certStore: certStore,
		log:       logger,
		storage:   defaultInterceptStorageDir(),
		leafMap:   make(map[string]cachedLeafCert),
	}
}

func defaultInterceptStorageDir() string {
	if cfg, err := os.UserConfigDir(); err == nil && strings.TrimSpace(cfg) != "" {
		return filepath.Join(cfg, "Themisto", "intercept")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".themisto", "intercept")
	}
	return filepath.Join(os.TempDir(), "themisto-intercept")
}

func (m *interceptCertManager) EnsureReady(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ready {
		return nil
	}

	if err := m.loadOrCreateCA(); err != nil {
		return err
	}
	if err := m.installCATrust(ctx); err != nil {
		return err
	}
	m.ready = true
	return nil
}

func (m *interceptCertManager) Rotate(ctx context.Context) error {
	m.mu.Lock()
	m.ready = false
	m.leafMap = make(map[string]cachedLeafCert)
	_ = os.Remove(filepath.Join(m.storage, interceptCACertFile))
	_ = os.Remove(filepath.Join(m.storage, interceptCAKeyFile))
	m.mu.Unlock()
	return m.EnsureReady(ctx)
}

func (m *interceptCertManager) Remove(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.certStore != nil {
		if err := m.certStore.RemoveCA(ctx, interceptCAID); err != nil {
			return fmt.Errorf("remove intercept CA from trust store: %w", err)
		}
	}
	_ = os.Remove(filepath.Join(m.storage, interceptCACertFile))
	_ = os.Remove(filepath.Join(m.storage, interceptCAKeyFile))
	m.caCert = tls.Certificate{}
	m.caLeaf = nil
	m.leafMap = make(map[string]cachedLeafCert)
	m.ready = false
	return nil
}

func (m *interceptCertManager) LeafCertificate(host string) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.ready || m.caLeaf == nil || len(m.caCert.Certificate) == 0 || m.caCert.PrivateKey == nil {
		return nil, fmt.Errorf("intercept CA is not ready")
	}

	host = normalizeLeafHost(host)
	if host == "" {
		return nil, fmt.Errorf("invalid host for forged leaf certificate")
	}

	if cached, ok := m.leafMap[host]; ok && time.Until(cached.expires) > 5*time.Minute {
		cert := cached.cert
		return &cert, nil
	}

	cert, notAfter, err := m.generateLeaf(host)
	if err != nil {
		return nil, err
	}
	m.leafMap[host] = cachedLeafCert{cert: cert, expires: notAfter}
	return &cert, nil
}

func normalizeLeafHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

func (m *interceptCertManager) loadOrCreateCA() error {
	if err := os.MkdirAll(m.storage, 0700); err != nil {
		return fmt.Errorf("create interception CA directory: %w", err)
	}

	certPath := filepath.Join(m.storage, interceptCACertFile)
	keyPath := filepath.Join(m.storage, interceptCAKeyFile)
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		loaded, leaf, err := parseCAPair(certPEM, keyPEM)
		if err == nil && leaf.IsCA && time.Until(leaf.NotAfter) > 24*time.Hour {
			m.caCert = loaded
			m.caLeaf = leaf
			return nil
		}
		m.log.Warn("existing interception CA invalid, regenerating", "error", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate interception CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("generate interception CA serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Themisto HTTPS Interception CA", Organization: []string{"Themisto"}},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(5 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        false,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("create interception CA certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshal interception CA key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return fmt.Errorf("write interception CA certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write interception CA private key: %w", err)
	}

	loaded, leaf, err := parseCAPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load generated interception CA: %w", err)
	}
	m.caCert = loaded
	m.caLeaf = leaf
	return nil
}

func (m *interceptCertManager) installCATrust(ctx context.Context) error {
	if m.certStore == nil {
		return fmt.Errorf("intercept trust store unavailable")
	}

	has, err := m.certStore.HasCA(interceptCAID)
	if err == nil && has {
		return nil
	}

	if len(m.caCert.Certificate) == 0 {
		return fmt.Errorf("intercept CA certificate is empty")
	}
	if err := m.certStore.InstallCA(ctx, interceptCAID, m.caCert.Certificate[0]); err != nil {
		return fmt.Errorf("install interception CA: %w", err)
	}
	return nil
}

func (m *interceptCertManager) generateLeaf(host string) (tls.Certificate, time.Time, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf("generate leaf key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf("generate leaf serial: %w", err)
	}

	now := time.Now()
	notAfter := now.Add(24 * time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Themisto"}},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, m.caLeaf, &priv.PublicKey, m.caCert.PrivateKey)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf("sign forged leaf certificate: %w", err)
	}

	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if len(m.caCert.Certificate) > 0 {
		leafPEM = append(leafPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: m.caCert.Certificate[0]})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf("marshal leaf key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(leafPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, time.Time{}, fmt.Errorf("build leaf key pair: %w", err)
	}
	return tlsCert, notAfter, nil
}

func parseCAPair(certPEM, keyPEM []byte) (tls.Certificate, *x509.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if len(pair.Certificate) == 0 {
		return tls.Certificate{}, nil, fmt.Errorf("CA certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return pair, leaf, nil
}

func randomSerial() (*big.Int, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, max)
}
