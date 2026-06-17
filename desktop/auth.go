package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	sessionTTL     = 30 * time.Minute
	defaultBackend = "https://localhost:8443"
)

// AuthManager handles admin authentication against the backend API.
type AuthManager struct {
	mu         sync.RWMutex
	sessions   map[string]sessionEntry
	client     *http.Client
}

type sessionEntry struct {
	token     string
	expiresAt time.Time
}

// NewAuthManager creates an auth manager.
func NewAuthManager() *AuthManager {
	return &AuthManager{
		sessions: make(map[string]sessionEntry),
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: buildAuthTLSConfig()},
		},
	}
}

// buildAuthTLSConfig creates a TLS config using the system CA pool, optionally
// appending the agent's CA cert (from enrollment) for self-signed backends.
func buildAuthTLSConfig() *tls.Config {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	// Try to load the CA cert from agent config (written during enrollment).
	cfg := loadAgentConfig()
	if caPath, ok := cfg["ca_path"].(string); ok && caPath != "" {
		if caPEM, err := os.ReadFile(caPath); err == nil {
			pool.AppendCertsFromPEM(caPEM)
		}
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}
}

// ValidateAdmin authenticates against POST /api/v1/auth/login on the backend.
// Returns the session token on success.
func (am *AuthManager) ValidateAdmin(email, password string) (string, error) {
	payload := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	backendURL := am.resolveBackendURL()

	resp, err := am.postAuthLogin(backendURL, payload)
	if err != nil && shouldRetryOverHTTP(err, backendURL) {
		if alt := httpsToHTTP(backendURL); alt != "" {
			resp, err = am.postAuthLogin(alt, payload)
		}
	}
	if err != nil {
		return "", fmt.Errorf("could not reach backend: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid email or password")
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("authentication failed (%d)", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("invalid response from backend")
	}

	am.mu.Lock()
	am.sessions[result.Token] = sessionEntry{
		token:     result.Token,
		expiresAt: time.Now().Add(sessionTTL),
	}
	am.mu.Unlock()

	return result.Token, nil
}

// IsAuthorized checks if a token is valid and not expired.
func (am *AuthManager) IsAuthorized(token string) bool {
	if token == "" {
		return false
	}
	am.mu.RLock()
	defer am.mu.RUnlock()
	entry, ok := am.sessions[token]
	if !ok {
		return false
	}
	return time.Now().Before(entry.expiresAt)
}

func (am *AuthManager) resolveBackendURL() string {
	cfg := loadAgentConfig()
	if url, ok := cfg["backend_url"].(string); ok && url != "" {
		return url
	}
	return defaultBackend
}

func (am *AuthManager) postAuthLogin(backendURL, payload string) (*http.Response, error) {
	return am.client.Post(
		strings.TrimRight(backendURL, "/")+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(payload),
	)
}

func shouldRetryOverHTTP(err error, backendURL string) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(backendURL)), "https://") {
		return false
	}
	if !isLocalhostURL(backendURL) {
		return false
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "http response to https client") {
		return true
	}

	var uerr *url.Error
	if errors.As(err, &uerr) {
		if opErr, ok := uerr.Err.(*net.OpError); ok && opErr != nil {
			return strings.Contains(strings.ToLower(opErr.Error()), "http response to https client")
		}
	}
	return false
}

func httpsToHTTP(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if strings.ToLower(u.Scheme) != "https" {
		return ""
	}
	u.Scheme = "http"
	return u.String()
}

func isLocalhostURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func loadAgentConfig() map[string]interface{} {
	paths := []string{agentConfigPath}

	if runtime.GOOS != "windows" {
		paths = append(paths, "/usr/local/etc/themisto/agent.json")
	}

	// Allow CWD fallback only when running from a dev/repo location (not
	// installed under Program Files). This prevents the installed desktop
	// app from accidentally picking up a repo-local agent.json.
	if !isInstalledBinary() {
		paths = append(paths, "agent.json")
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg map[string]interface{}
		if json.Unmarshal(data, &cfg) == nil {
			return cfg
		}
	}
	return map[string]interface{}{}
}

// isInstalledBinary returns true if the running executable is located under
// the canonical install directory (C:\Program Files\Themisto).
func isInstalledBinary() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(abs), strings.ToLower(`C:\Program Files\Themisto`))
	}
	return strings.HasPrefix(abs, "/Applications/Themisto.app/")
}
