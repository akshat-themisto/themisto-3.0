package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/themisto/gateway/internal/config"
	"github.com/themisto/gateway/internal/handler"
	"github.com/themisto/gateway/internal/metrics"
	"github.com/themisto/gateway/internal/mtls"
	"github.com/themisto/gateway/internal/policy"
	"github.com/themisto/gateway/internal/store"
	"github.com/themisto/gateway/internal/telemetry"
	"github.com/themisto/gateway/internal/upstream"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/themisto/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := store.New(cfg.DBDSN)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("connected to database")

	if err := db.EnsureDLPSchema(context.Background()); err != nil {
		logger.Error("failed to ensure DLP event schema", "error", err)
		os.Exit(1)
	}
	if err := db.EnsureAgentStatusSchema(context.Background()); err != nil {
		logger.Error("failed to ensure agent status schema", "error", err)
		os.Exit(1)
	}
	if err := db.EnsurePolicyControlsSchema(context.Background()); err != nil {
		logger.Error("failed to ensure policy controls schema", "error", err)
		os.Exit(1)
	}

	tlsConfig, err := mtls.NewTLSConfig(cfg.TLS.CertPath, cfg.TLS.KeyPath, cfg.TLS.CAChainPath)
	if err != nil {
		logger.Error("failed to configure TLS", "error", err)
		os.Exit(1)
	}

	verifier := mtls.NewCertVerifier(
		cfg.Revocation.BackendURL,
		cfg.Revocation.FailMode,
		cfg.InternalToken,
		cfg.ControlPlane.URL,
		cfg.ControlPlane.Token,
		cfg.ControlPlane.FailMode,
		cfg.ControlPlane.CacheTTL,
		cfg.Revocation.CacheTTL,
		cfg.Revocation.CacheMaxEntries,
		logger,
	)

	telBuf := telemetry.NewBuffer(
		db,
		cfg.Telemetry.BufferSize,
		cfg.Telemetry.FlushBatchSize,
		cfg.Telemetry.FlushInterval,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	telBuf.Start(ctx)

	fwd := upstream.NewForwarder(cfg.Upstream.RequestTimeout)
	defer fwd.Close()

	pol := policy.NewEngine(db)
	pol.StartReloader(ctx, 10*time.Second)

	var connCount atomic.Int64

	proxyHandler := handler.NewProxyHandler(fwd, pol, telBuf, verifier, logger)
	policyHandler := handler.NewPolicyHandler(pol, db, verifier, logger)
	semanticHandler := handler.NewSemanticHandler(cfg.PromptSemantics, verifier, logger)

	// Mux: /policy and /healthz are handled by dedicated handlers.
	// /telemetry and all other paths go through proxyHandler (mTLS cert verification + real processing).
	proxyMux := http.NewServeMux()

	proxyMux.Handle("GET /policy", policyHandler)
	proxyMux.Handle("POST /v1/prompt/semantic-evaluate", semanticHandler)
	proxyMux.Handle("GET /healthz", handler.NewHealthHandler(db, &connCount))
	proxyMux.Handle("/", proxyHandler)

	ln, err := net.Listen("tcp", cfg.Server.ListenAddr)
	if err != nil {
		logger.Error("failed to listen", "addr", cfg.Server.ListenAddr, "error", err)
		os.Exit(1)
	}
	tlsLn := tls.NewListener(ln, tlsConfig)

	proxySrv := &http.Server{
		Handler:      proxyMux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				connCount.Add(1)
				metrics.ActiveConnections.Inc()
			case http.StateClosed, http.StateHijacked:
				connCount.Add(-1)
				metrics.ActiveConnections.Dec()
			}
		},
	}

	healthHandler := handler.NewHealthHandler(db, &connCount)
	healthMux := http.NewServeMux()
	healthMux.Handle("GET /healthz", healthHandler)
	healthMux.Handle("GET /metrics", withInternalMetricsAuth(cfg.InternalToken, promhttp.Handler()))
	healthSrv := &http.Server{
		Addr:    cfg.Server.HealthAddr,
		Handler: healthMux,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("gateway mTLS listening", "addr", cfg.Server.ListenAddr)
		errCh <- proxySrv.Serve(tlsLn)
	}()
	go func() {
		logger.Info("health endpoint listening", "addr", cfg.Server.HealthAddr)
		errCh <- healthSrv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		logger.Error("server error", "error", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	proxySrv.Shutdown(shutdownCtx)
	healthSrv.Shutdown(shutdownCtx)
	cancel()
	telBuf.Stop()

	logger.Info("gateway stopped")
}

func withInternalMetricsAuth(expectedToken string, next http.Handler) http.Handler {
	if expectedToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
			http.Error(w, "invalid internal token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
