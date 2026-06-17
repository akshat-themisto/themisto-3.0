package statusapi

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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	proxyStartupGraceWindow = 30 * time.Second
	gatewayRetryGraceWindow = 3 * time.Minute
)

// enrollRequest includes an optional CACert field so the caller can provide
// the backend's CA certificate for TLS verification of self-signed backends.
// The local auth token is sent via the Authorization header, not in the body.

// --- GET /v1/status ---

type statusResponse struct {
	Running            bool   `json:"running"`
	AgentID            string `json:"agent_id"`
	GatewayConnected   bool   `json:"gateway_connected"`
	GatewayState       string `json:"gateway_state"`
	Uptime             string `json:"uptime"`
	UptimeSeconds      int64  `json:"uptime_seconds"`
	ProxyState         string `json:"proxy_state"`
	ProxyListenerReady bool   `json:"proxy_listener_ready"`
	RuntimePhase       string `json:"runtime_phase"`
	Version            string `json:"version"`
	BufferSize         int    `json:"buffer_size"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config.Get()
	enrolled := false
	if _, err := s.deps.Identity.GetGatewayTLSConfig(); err == nil {
		enrolled = true
	}

	gatewayConnected := false
	if gateway := s.currentGateway(); gateway != nil {
		gatewayConnected = gateway.Healthy()
	}

	uptime := time.Since(s.deps.StartTime)
	proxyListenerReady := probeLoopbackListener(cfg.ListenAddr, 200*time.Millisecond)
	gatewayState, proxyState, runtimePhase := deriveRuntimePhase(uptime, gatewayConnected, proxyListenerReady, enrolled)

	resp := statusResponse{
		Running:            true,
		AgentID:            cfg.AgentID,
		GatewayConnected:   gatewayConnected,
		GatewayState:       gatewayState,
		Uptime:             uptime.Truncate(time.Second).String(),
		UptimeSeconds:      int64(uptime.Seconds()),
		ProxyState:         proxyState,
		ProxyListenerReady: proxyListenerReady,
		RuntimePhase:       runtimePhase,
		Version:            s.deps.Version,
		BufferSize:         s.deps.Collector.Size(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func deriveRuntimePhase(uptime time.Duration, gatewayConnected, proxyListenerReady, enrolled bool) (gatewayState, proxyState, runtimePhase string) {
	if !enrolled {
		return "not_enrolled", "waiting_for_enrollment", "needs_enrollment"
	}

	switch {
	case proxyListenerReady:
		proxyState = "running"
	case uptime < proxyStartupGraceWindow:
		proxyState = "starting"
	default:
		proxyState = "degraded"
	}

	switch {
	case gatewayConnected:
		gatewayState = "connected"
	case !proxyListenerReady && uptime < proxyStartupGraceWindow:
		gatewayState = "starting"
	case uptime < gatewayRetryGraceWindow:
		gatewayState = "retrying"
	default:
		gatewayState = "offline"
	}

	switch {
	case proxyState == "starting":
		runtimePhase = "starting"
	case proxyState == "degraded":
		runtimePhase = "degraded"
	case gatewayState == "retrying":
		runtimePhase = "retrying_gateway"
	case gatewayState == "connected":
		runtimePhase = "ready"
	default:
		runtimePhase = "degraded"
	}

	return gatewayState, proxyState, runtimePhase
}

func probeLoopbackListener(listenAddr string, timeout time.Duration) bool {
	if listenAddr == "" {
		return false
	}

	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	if host == "" {
		host = "127.0.0.1"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}

	target := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// --- GET /v1/config ---

type configResponse struct {
	ListenAddr       string   `json:"listen_addr"`
	GatewayURL       string   `json:"gateway_url"`
	Enrolled         bool     `json:"enrolled"`
	OrgName          string   `json:"org_name"`
	AgentID          string   `json:"agent_id"`
	EnrollmentHealth string   `json:"enrollment_health"`
	CertExpiry       string   `json:"cert_expiry,omitempty"`
	ConfigIssues     []string `json:"config_issues,omitempty"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config.Get()

	enrolled := false
	if _, err := s.deps.Identity.GetGatewayTLSConfig(); err == nil {
		enrolled = true
	}

	orgName := readOrgNameFromConfig(s.deps.ConfigPath)
	health, expiry, issues := s.checkEnrollmentHealth()

	resp := configResponse{
		ListenAddr:       cfg.ListenAddr,
		GatewayURL:       cfg.GatewayURL,
		Enrolled:         enrolled,
		OrgName:          orgName,
		AgentID:          cfg.AgentID,
		EnrollmentHealth: health,
		CertExpiry:       expiry,
		ConfigIssues:     issues,
	}
	writeJSON(w, http.StatusOK, resp)
}

// checkEnrollmentHealth inspects config and cert files on disk to return a
// specific enrollment health status instead of just enrolled/not-enrolled.
func (s *Server) checkEnrollmentHealth() (health string, certExpiry string, issues []string) {
	cfg := s.deps.Config.Get()

	if cfg.AgentID == "" {
		issues = append(issues, "agent_id not set")
	}
	if cfg.GatewayURL == "" {
		issues = append(issues, "gateway_url not set")
	}

	if cfg.CertPath == "" || cfg.KeyPath == "" || cfg.CAPath == "" {
		return "not_enrolled", "", issues
	}

	for _, p := range []struct{ name, path string }{
		{"cert", cfg.CertPath}, {"key", cfg.KeyPath}, {"ca", cfg.CAPath},
	} {
		if _, err := os.Stat(p.path); err != nil {
			issues = append(issues, "missing: "+p.name+" ("+p.path+")")
			return "cert_missing", "", issues
		}
	}

	certPEM, err := os.ReadFile(cfg.CertPath)
	if err != nil {
		issues = append(issues, "cannot read cert: "+err.Error())
		return "cert_invalid", "", issues
	}
	keyPEM, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		issues = append(issues, "cannot read key: "+err.Error())
		return "cert_invalid", "", issues
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		issues = append(issues, "cert/key pair invalid: "+err.Error())
		return "cert_invalid", "", issues
	}
	caPEM, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		issues = append(issues, "cannot read ca chain: "+err.Error())
		return "cert_invalid", "", issues
	}
	if err := validateCAChainPEM(caPEM); err != nil {
		issues = append(issues, "ca chain invalid: "+err.Error())
		return "cert_invalid", "", issues
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		issues = append(issues, "cert PEM decode failed")
		return "cert_invalid", "", issues
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		issues = append(issues, "cert parse failed: "+err.Error())
		return "cert_invalid", "", issues
	}

	certExpiry = cert.NotAfter.Format(time.RFC3339)
	if time.Now().After(cert.NotAfter) {
		issues = append(issues, "certificate expired")
		return "cert_expired", certExpiry, issues
	}

	if len(issues) > 0 {
		return "config_incomplete", certExpiry, issues
	}
	return "healthy", certExpiry, nil
}

func validateCAChainPEM(caPEM []byte) error {
	rest := caPEM
	parsed := 0
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return err
		}
		parsed++
	}
	if parsed == 0 {
		return fmt.Errorf("no certificate PEM blocks found")
	}
	return nil
}

// --- POST /v1/enroll ---

type enrollRequest struct {
	Token      string `json:"token"`
	BackendURL string `json:"backend_url"`
	GatewayURL string `json:"gateway_url"`
	OrgName    string `json:"org_name"`
	DeviceID   string `json:"device_id"`
	CACert     string `json:"ca_cert,omitempty"` // optional PEM-encoded CA cert for self-signed backends
}

type enrollResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	DeviceID string `json:"device_id,omitempty"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	// Validate local auth token (shared-secret written to a temp file on startup).
	if !s.validateLocalAuth(r) {
		writeJSON(w, http.StatusUnauthorized, enrollResponse{Message: "unauthorized: invalid or missing local auth token"})
		return
	}

	var req enrollRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, enrollResponse{Message: "read body: " + err.Error()})
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, enrollResponse{Message: "invalid json"})
		return
	}

	if req.Token == "" || req.BackendURL == "" || req.DeviceID == "" || req.OrgName == "" {
		writeJSON(w, http.StatusBadRequest, enrollResponse{Message: "token, backend_url, device_id, and org_name are required"})
		return
	}

	configPath := s.deps.ConfigPath

	// Generate key pair and CSR
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "generate key: " + err.Error()})
		return
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   req.DeviceID,
			Organization: []string{req.OrgName},
		},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}, priv)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "create CSR: " + err.Error()})
		return
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "marshal key: " + err.Error()})
		return
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Build TLS config using system CA pool; optionally append a custom CA cert.
	tlsCfg, err := buildEnrollmentTLSConfig(req.CACert)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, enrollResponse{Message: "invalid ca_cert: " + err.Error()})
		return
	}

	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}

	enrollURL := strings.TrimRight(req.BackendURL, "/") + "/api/v1/devices/" + req.DeviceID + "/csr"
	httpReq, err := http.NewRequest(http.MethodPost, enrollURL, bytes.NewReader(csrPEM))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "build request: " + err.Error()})
		return
	}
	httpReq.Header.Set("X-Enrollment-Token", req.Token)
	httpReq.Header.Set("Content-Type", "application/pkcs10")

	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, enrollResponse{Message: "enrollment request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, enrollResponse{Message: "read response: " + err.Error()})
		return
	}
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, enrollResponse{Message: fmt.Sprintf("enrollment failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))})
		return
	}

	var enrollResp struct {
		Certificate string `json:"certificate"`
		CAChain     string `json:"ca_chain"`
	}
	if err := json.Unmarshal(respBody, &enrollResp); err != nil {
		writeJSON(w, http.StatusBadGateway, enrollResponse{Message: "decode response: " + err.Error()})
		return
	}
	if enrollResp.Certificate == "" || enrollResp.CAChain == "" {
		writeJSON(w, http.StatusBadGateway, enrollResponse{Message: "response missing certificate or ca_chain"})
		return
	}

	// Write credentials to the directory configured in the existing config
	// (matches installer defaults), falling back to <configDir>/certs/.
	cfgMap := loadConfigMapSafe(configPath)
	certDir := resolveCertDir(cfgMap, configPath)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "create cert dir: " + err.Error()})
		return
	}

	certPath := filepath.Join(certDir, "device.crt")
	keyPath := filepath.Join(certDir, "device.key")
	caPath := filepath.Join(certDir, "ca-chain.pem")

	if err := os.WriteFile(keyPath, keyPEM, 0400); err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "write key: " + err.Error()})
		return
	}
	if err := os.WriteFile(certPath, []byte(enrollResp.Certificate), 0444); err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "write cert: " + err.Error()})
		return
	}
	if err := os.WriteFile(caPath, []byte(enrollResp.CAChain), 0444); err != nil {
		writeJSON(w, http.StatusInternalServerError, enrollResponse{Message: "write ca: " + err.Error()})
		return
	}

	// Update config file (cfgMap already loaded above)
	cfgMap["cert_path"] = certPath
	cfgMap["key_path"] = keyPath
	cfgMap["ca_path"] = caPath
	cfgMap["device_id"] = req.DeviceID
	cfgMap["org_name"] = req.OrgName
	cfgMap["backend_url"] = req.BackendURL
	if req.GatewayURL != "" {
		cfgMap["gateway_url"] = req.GatewayURL
	}
	if _, ok := cfgMap["agent_id"]; !ok || cfgMap["agent_id"] == "" {
		cfgMap["agent_id"] = req.DeviceID
	}
	delete(cfgMap, "enrollment_token")

	data, _ := json.MarshalIndent(cfgMap, "", "  ")
	data = append(data, '\n')
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		s.deps.Logger.Warn("failed to write updated config after enrollment", "error", err)
	}

	message := "Device enrolled successfully. Themisto is finalizing local startup."
	if s.deps.OnEnrollSuccess != nil {
		if err := s.deps.OnEnrollSuccess(); err != nil {
			s.deps.Logger.Warn("post-enrollment activation failed", "error", err)
			message = "Device enrolled successfully, but Themisto still needs to finish local startup: " + err.Error()
		}
	}

	writeJSON(w, http.StatusOK, enrollResponse{
		Success:  true,
		Message:  message,
		DeviceID: req.DeviceID,
	})
}

// validateLocalAuth checks the Authorization: Bearer <token> header against the
// shared-secret auth token generated at startup.
func (s *Server) validateLocalAuth(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.authToken
}

// buildEnrollmentTLSConfig creates a TLS config using the system CA pool.
// If caCertPEM is non-empty, it is appended to the pool (for self-signed backends).
func buildEnrollmentTLSConfig(caCertPEM string) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if caCertPEM = strings.TrimSpace(caCertPEM); caCertPEM != "" {
		if !pool.AppendCertsFromPEM([]byte(caCertPEM)) {
			return nil, fmt.Errorf("failed to parse provided CA certificate PEM")
		}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}, nil
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func loadConfigMapSafe(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return map[string]interface{}{}
	}
	return cfg
}

// resolveCertDir returns the directory where device certificates should be
// written. If the config already specifies a cert_path whose parent directory
// exists, that directory is reused (avoids orphaning certs on re-enrollment).
// Otherwise it falls back to <configDir>/certs/ which matches the installer
// defaults.
func resolveCertDir(cfgMap map[string]interface{}, configPath string) string {
	if cp, ok := cfgMap["cert_path"].(string); ok && cp != "" {
		if !filepath.IsAbs(cp) {
			cp = filepath.Join(filepath.Dir(configPath), cp)
		}
		dir := filepath.Dir(cp)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return filepath.Join(filepath.Dir(configPath), "certs")
}

func readOrgNameFromConfig(configPath string) string {
	cfg := loadConfigMapSafe(configPath)
	if v, ok := cfg["org_name"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
