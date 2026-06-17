// Package statusapi provides a read-only local HTTP API for the desktop app
// to query agent state. Binds only to loopback (127.0.0.1:17176).
package statusapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/themisto/agent/core/config"
	"github.com/themisto/agent/core/identity"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/core/transport"
	"github.com/themisto/agent/pkg/log"
)

const DefaultListenAddr = "127.0.0.1:17176"

// Deps groups dependencies for the status API server.
type Deps struct {
	Config          config.Provider
	Collector       *telemetry.Collector
	Identity        *identity.IdentityStore
	Gateway         *transport.MtlsGatewayClient
	Logger          log.Logger
	StartTime       time.Time
	Version         string
	ConfigPath      string // actual path to agent config file
	OnEnrollSuccess func() error
}

// Server is the local status API HTTP server.
type Server struct {
	deps      Deps
	server    *http.Server
	addr      string
	authToken string // shared-secret for local auth
	tokenPath string // path to the auth token file
	gatewayMu sync.RWMutex
	gateway   *transport.MtlsGatewayClient
}

// New creates a status API server bound to the loopback address.
func New(deps Deps) *Server {
	return &Server{
		deps:    deps,
		addr:    DefaultListenAddr,
		gateway: deps.Gateway,
	}
}

// TokenPath returns the path to the auth token file, so the desktop app can read it.
func (s *Server) TokenPath() string {
	return s.tokenPath
}

// Start begins listening. It returns immediately; the server runs in a goroutine.
func (s *Server) Start(ctx context.Context) error {
	// Generate a shared-secret auth token for local IPC.
	token, tokenPath, err := generateAuthToken()
	if err != nil {
		return fmt.Errorf("status api auth token: %w", err)
	}
	s.authToken = token
	s.tokenPath = tokenPath

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("POST /v1/enroll", s.handleEnroll)

	// No CORS middleware — the status API is only called by the local
	// desktop app via Go HTTP, not from a browser origin.
	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("status api listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()
	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.deps.Logger.Error("status api server exited", "error", err)
		}
	}()

	s.deps.Logger.Info("status api listening", "addr", s.addr, "token_path", s.tokenPath)
	return nil
}

// generateAuthToken creates a 32-byte random token and writes it to a stable
// local IPC file. On non-Windows platforms the token must be readable by the
// desktop app even when the agent runs as root under launchd.
func generateAuthToken() (string, string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", fmt.Errorf("generate random token: %w", err)
	}
	token := hex.EncodeToString(buf[:])

	dir := "/tmp"
	mode := os.FileMode(0644)
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			dir = filepath.Join(pd, "Themisto")
			_ = os.MkdirAll(dir, 0700)
		}
		mode = 0600
	}
	tokenPath := filepath.Join(dir, "themisto-status-api.token")
	if err := os.WriteFile(tokenPath, []byte(token), mode); err != nil {
		return "", "", fmt.Errorf("write token file: %w", err)
	}
	return token, tokenPath, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// Addr returns the listen address.
func (s *Server) Addr() string {
	return s.addr
}

// SetGateway updates the gateway reference used by status handlers once the
// runtime has progressed past bootstrap.
func (s *Server) SetGateway(gw *transport.MtlsGatewayClient) {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()
	s.gateway = gw
}

func (s *Server) currentGateway() *transport.MtlsGatewayClient {
	s.gatewayMu.RLock()
	defer s.gatewayMu.RUnlock()
	return s.gateway
}
