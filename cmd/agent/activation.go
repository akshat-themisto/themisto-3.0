package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type activationOptions struct {
	ConfigPath      string
	BackendURL      string
	GatewayURL      string
	DeviceID        string
	EnrollmentToken string
	OrgName         string
	OutputDir       string
	CACertPath      string
	InsecureTLS     bool
	ClearToken      bool
}

type enrollmentResponse struct {
	Certificate string `json:"certificate"`
	CAChain     string `json:"ca_chain"`
}

func maybeActivateFromConfig(configPath string, logger *stdLogger) error {
	cfg, err := loadConfigMap(configPath)
	if err != nil {
		return nil
	}
	if hasUsableBootstrapCerts(cfg) {
		return nil
	}

	deviceID := getString(cfg, "device_id")
	token := getString(cfg, "enrollment_token")
	orgName := getString(cfg, "org_name")
	backendURL := getString(cfg, "backend_url")
	if deviceID == "" || token == "" || orgName == "" || backendURL == "" {
		return nil
	}

	insecureTLS := getBool(cfg, "insecure_enrollment_tls")
	gatewayURL := getString(cfg, "gateway_url")

	logger.Info("bootstrapping certificate identity via enrollment token", "device_id", deviceID, "backend_url", backendURL)
	return runActivation(activationOptions{
		ConfigPath:      configPath,
		BackendURL:      backendURL,
		GatewayURL:      gatewayURL,
		DeviceID:        deviceID,
		EnrollmentToken: token,
		OrgName:         orgName,
		InsecureTLS:     insecureTLS,
		ClearToken:      true,
	}, logger)
}

func runActivation(opts activationOptions, logger *stdLogger) error {
	if opts.ConfigPath == "" {
		opts.ConfigPath = "agent.json"
	}

	cfg, err := loadConfigMap(opts.ConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}

	if opts.BackendURL == "" {
		opts.BackendURL = getString(cfg, "backend_url")
	}
	if opts.DeviceID == "" {
		opts.DeviceID = getString(cfg, "device_id")
	}
	if opts.GatewayURL == "" {
		opts.GatewayURL = getString(cfg, "gateway_url")
	}
	if opts.EnrollmentToken == "" {
		opts.EnrollmentToken = getString(cfg, "enrollment_token")
	}
	if opts.OrgName == "" {
		opts.OrgName = getString(cfg, "org_name")
	}
	if opts.CACertPath == "" {
		opts.CACertPath = getString(cfg, "backend_ca_path")
	}

	if opts.BackendURL == "" || opts.DeviceID == "" || opts.EnrollmentToken == "" || opts.OrgName == "" {
		return fmt.Errorf("activation requires backend_url, device_id, enrollment_token, and org_name")
	}

	certPath, keyPath, caPath, err := resolveCredentialPaths(opts, cfg)
	if err != nil {
		return err
	}

	result, keyPEM, err := enrollDevice(opts)
	if err != nil {
		return err
	}

	if err := writeCredentials(certPath, keyPath, caPath, keyPEM, []byte(result.Certificate), []byte(result.CAChain)); err != nil {
		return err
	}

	cfg["cert_path"] = certPath
	cfg["key_path"] = keyPath
	cfg["ca_path"] = caPath
	if getString(cfg, "agent_id") == "" {
		cfg["agent_id"] = opts.DeviceID
	}
	cfg["device_id"] = opts.DeviceID
	cfg["org_name"] = opts.OrgName
	cfg["backend_url"] = opts.BackendURL
	if opts.GatewayURL != "" {
		cfg["gateway_url"] = opts.GatewayURL
	}
	if opts.ClearToken {
		delete(cfg, "enrollment_token")
	}

	if err := writeConfigMap(opts.ConfigPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	logger.Info("activation complete",
		"device_id", opts.DeviceID,
		"cert_path", certPath,
		"key_path", keyPath,
		"ca_path", caPath,
	)
	return nil
}

func resolveCredentialPaths(opts activationOptions, cfg map[string]interface{}) (string, string, string, error) {
	if cp, kp, cap := getString(cfg, "cert_path"), getString(cfg, "key_path"), getString(cfg, "ca_path"); cp != "" && kp != "" && cap != "" {
		return cp, kp, cap, nil
	}

	outDir := opts.OutputDir
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(opts.ConfigPath), "certs")
	}
	if outDir == "" {
		return "", "", "", fmt.Errorf("unable to determine output directory for credentials")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("create output directory %s: %w", outDir, err)
	}

	return filepath.Join(outDir, "device.crt"),
		filepath.Join(outDir, "device.key"),
		filepath.Join(outDir, "ca-chain.pem"), nil
}

func writeCredentials(certPath, keyPath, caPath string, keyPEM, certPEM, caPEM []byte) error {
	for _, p := range []string{certPath, keyPath, caPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return fmt.Errorf("create directory for %s: %w", p, err)
		}
	}

	if err := os.WriteFile(keyPath, keyPEM, 0400); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0444); err != nil {
		return fmt.Errorf("write certificate file: %w", err)
	}
	if err := os.WriteFile(caPath, caPEM, 0444); err != nil {
		return fmt.Errorf("write CA chain file: %w", err)
	}
	return nil
}

func enrollDevice(opts activationOptions) (*enrollmentResponse, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate private key: %w", err)
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   opts.DeviceID,
			Organization: []string{opts.OrgName},
		},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	client, err := enrollmentHTTPClient(opts.CACertPath, opts.InsecureTLS)
	if err != nil {
		return nil, nil, err
	}

	url := strings.TrimRight(opts.BackendURL, "/") + "/api/v1/devices/" + opts.DeviceID + "/csr"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(csrPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("build enrollment request: %w", err)
	}
	req.Header.Set("X-Enrollment-Token", opts.EnrollmentToken)
	req.Header.Set("Content-Type", "application/pkcs10")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("enrollment request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read enrollment response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("enrollment failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out enrollmentResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, nil, fmt.Errorf("decode enrollment response: %w", err)
	}
	if strings.TrimSpace(out.Certificate) == "" || strings.TrimSpace(out.CAChain) == "" {
		return nil, nil, fmt.Errorf("enrollment response missing certificate or ca_chain")
	}

	return &out, keyPEM, nil
}

func enrollmentHTTPClient(caPath string, insecure bool) (*http.Client, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	}
	if caPath != "" {
		pemBytes, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read backend CA file %s: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("parse backend CA file %s: no valid certificates found", caPath)
		}
		tlsConfig.RootCAs = pool
	}

	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}, nil
}

func hasUsableBootstrapCerts(cfg map[string]interface{}) bool {
	certPath := getString(cfg, "cert_path")
	keyPath := getString(cfg, "key_path")
	caPath := getString(cfg, "ca_path")
	if certPath == "" || keyPath == "" || caPath == "" {
		return false
	}
	for _, p := range []string{certPath, keyPath, caPath} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func loadConfigMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config JSON: %w", err)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg, nil
}

func writeConfigMap(path string, cfg map[string]interface{}) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func getString(cfg map[string]interface{}, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func getBool(cfg map[string]interface{}, key string) bool {
	v, ok := cfg[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}
