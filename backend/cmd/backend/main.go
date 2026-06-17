package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/themisto/backend/internal/api"
	"github.com/themisto/backend/internal/config"
	"github.com/themisto/backend/internal/controlplane"
	"github.com/themisto/backend/internal/metrics"
	"github.com/themisto/backend/internal/signing"
	"github.com/themisto/backend/internal/store"
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

	if err := db.EnsureOperatorControlSchema(context.Background()); err != nil {
		logger.Error("failed to ensure operator control schema", "error", err)
		os.Exit(1)
	}
	if err := db.EnsureDLPSchema(context.Background()); err != nil {
		logger.Error("failed to ensure DLP event schema", "error", err)
		os.Exit(1)
	}

	signer, err := signing.NewSigner(
		cfg.Signing.CACertPath,
		cfg.Signing.CAKeyPath,
		cfg.Signing.CAChainPath,
		cfg.Signing.CertValidityDays,
	)
	if err != nil {
		logger.Error("failed to initialize signer", "error", err)
		os.Exit(1)
	}
	logger.Info("CA signer initialized", "issuer", signer.CACert.Subject.CommonName)

	controlPlane := controlplane.NewClient(
		cfg.ControlPlane.URL,
		cfg.ControlPlane.Token,
		cfg.ControlPlane.FailMode,
		cfg.ControlPlane.CacheTTL,
	)

	srv := &http.Server{
		Addr: cfg.Server.ListenAddr,
		Handler: api.NewServer(
			db,
			signer,
			cfg.AdminAPIKey,
			time.Duration(cfg.Token.ExpiryHours)*time.Hour,
			cfg.Public.BackendURL,
			cfg.Public.GatewayURL,
			controlPlane,
			logger,
			cfg.CORSAllowedOrigins,
		),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	defer backgroundCancel()
	go runDLPBodyRetentionScrubber(backgroundCtx, db, cfg.DLPBodyRetentionDays, logger)
	go runManagedPolicyMaintenance(backgroundCtx, db, logger)
	go runMetricsRefresh(backgroundCtx, db, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("backend listening", "addr", cfg.Server.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig)
	case err := <-errCh:
		logger.Error("server error", "error", err)
	}
	backgroundCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("backend stopped")
}

func runDLPBodyRetentionScrubber(ctx context.Context, db *store.Store, retentionDays int, logger *slog.Logger) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	run := func() {
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		rows, err := db.ScrubExpiredDLPBodies(c, retentionDays)
		if err != nil {
			logger.Error("dlp body retention scrub failed", "retention_days", retentionDays, "error", err)
			return
		}
		if rows > 0 {
			logger.Info("dlp body retention scrub completed", "retention_days", retentionDays, "scrubbed_rows", rows)
		}
	}

	run()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func runManagedPolicyMaintenance(ctx context.Context, db *store.Store, logger *slog.Logger) {
	run := func() {
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		if err := db.EnsureManagedAIDLPPoliciesForAllOrgs(c); err != nil {
			logger.Error("managed ai dlp policy maintenance failed", "error", err)
		}
		if err := db.EnsureDefaultAIInterceptDomainsForAllOrgs(c); err != nil {
			logger.Error("managed ai intercept domain maintenance failed", "error", err)
		}
	}

	run()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func runMetricsRefresh(ctx context.Context, db *store.Store, logger *slog.Logger) {
	run := func() {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Active devices gauge
		deviceCounts, err := db.ActiveDevicesByOrg(c)
		if err != nil {
			logger.Error("metrics: active devices query failed", "error", err)
		} else {
			metrics.ActiveDevicesTotal.Reset()
			for org, count := range deviceCounts {
				metrics.ActiveDevicesTotal.WithLabelValues(org).Set(float64(count))
			}
		}

		// Certs expiring within 14 days
		certCounts, err := db.CertsExpiringSoonByOrg(c, 14)
		if err != nil {
			logger.Error("metrics: expiring certs query failed", "error", err)
		} else {
			metrics.CertsExpiringSoon.Reset()
			for org, count := range certCounts {
				metrics.CertsExpiringSoon.WithLabelValues(org).Set(float64(count))
			}
		}
	}

	run()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
