package core

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/config"
	"github.com/themisto/agent/core/cursorcleanup"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/identity"
	"github.com/themisto/agent/core/policy"
	"github.com/themisto/agent/core/promptcapture"
	"github.com/themisto/agent/core/promptsemantics"
	"github.com/themisto/agent/core/routing"
	"github.com/themisto/agent/core/statusapi"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/core/transport"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/retry"
)

// Agent is the top-level type that composes all core modules. It owns the
// startup/shutdown lifecycle and wires dependencies.
type Agent struct {
	adapter   iface.OSAdapter
	configMgr *config.Manager
	identity  *identity.IdentityStore
	engine    *policy.DefaultEngine
	fetcher   *policy.DefaultFetcher
	syncLoop  *policy.SyncLoop
	router    *routing.DefaultRouter
	prompt    *promptcapture.Service
	statusAPI *statusapi.Server
	gateway   *transport.MtlsGatewayClient
	proxy     *transport.HTTPProxy
	collector *telemetry.Collector
	emitter   *telemetry.Emitter
	log       log.Logger
	version   string

	startTime time.Time
	cancel    context.CancelFunc
	gt        *goroutineTracker
}

// Deps groups the external dependencies injected from cmd/.
type Deps struct {
	Adapter iface.OSAdapter
	Source  config.Source
	Logger  log.Logger
	Version string
}

// NewAgent creates and initialises the agent. It loads configuration and sets
// up the identity store but does NOT start the proxy or background loops.
func NewAgent(deps Deps) (*Agent, error) {
	logger := deps.Logger

	cfgMgr, err := config.NewManager(deps.Source, logger)
	if err != nil {
		return nil, err
	}
	cfg := cfgMgr.Get()

	idStore := identity.NewIdentityStore(deps.Adapter, logger)

	// Bootstrap identity from PEM files if configured.
	if cfg.CertPath != "" {
		if err := bootstrapFromPEM(idStore, cfg, logger); err != nil {
			logger.Warn("identity bootstrap from PEM unavailable; agent will wait for enrollment",
				"error", err,
				"cert_path", cfg.CertPath,
				"key_path", cfg.KeyPath,
				"ca_path", cfg.CAPath,
			)
		} else {
			logger.Info("identity bootstrapped from PEM files",
				"cert_path", cfg.CertPath,
				"key_path", cfg.KeyPath,
				"ca_path", cfg.CAPath,
			)
		}
	}

	collector := telemetry.NewCollector(10000)

	engine := policy.NewEngine(cfg.DefaultDecision)

	a := &Agent{
		adapter:   deps.Adapter,
		configMgr: cfgMgr,
		identity:  idStore,
		engine:    engine,
		collector: collector,
		log:       logger,
		version:   deps.Version,
	}
	return a, nil
}

// BootstrapIdentity loads mTLS credentials directly into the identity store
// from raw DER data. Use this for file-based credential loading (e.g. demo
// or development setups) instead of relying on the OS cert store.
// caDER is a single DER-encoded CA certificate; it is converted to PEM
// internally so the full chain pool works correctly.
func (a *Agent) BootstrapIdentity(caID, clientID string, certDER, keyDER, caDER []byte) {
	caChainPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	a.identity.Bootstrap(caID, clientID, certDER, keyDER, caChainPEM)
}

// PreloadPolicy injects a policy payload directly into the engine. Use this
// when the gateway is not available to deliver policies (e.g. demo mode).
func (a *Agent) PreloadPolicy(payload *domain.PolicyPayload) error {
	return a.engine.Update(payload)
}

// Start runs the agent through its startup phases and blocks until ctx is
// cancelled or Stop is called.
func (a *Agent) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.startTime = time.Now()
	a.gt = newGoroutineTracker(ctx)

	enrollmentReady := make(chan struct{}, 1)

	a.log.Info("phase: status api")
	a.statusAPI = statusapi.New(statusapi.Deps{
		Config:     a.configMgr,
		Collector:  a.collector,
		Identity:   a.identity,
		Gateway:    nil,
		Logger:     a.log,
		StartTime:  a.startTime,
		Version:    a.version,
		ConfigPath: a.configMgr.SourcePath(),
		OnEnrollSuccess: func() error {
			if err := a.configMgr.Reload(); err != nil {
				return fmt.Errorf("reload config: %w", err)
			}
			if err := a.loadIdentityFromConfig(); err != nil {
				return err
			}
			select {
			case enrollmentReady <- struct{}{}:
			default:
			}
			return nil
		},
	})
	if err := a.statusAPI.Start(ctx); err != nil {
		a.log.Warn("status api unavailable", "error", err)
		if prereqErr := a.runtimePrereqError(); prereqErr != nil {
			return fmt.Errorf("status api unavailable before enrollment: %w", err)
		}
	} else {
		a.log.Info("status api ready", "addr", a.statusAPI.Addr())
	}

	if err := a.waitForRuntimeReady(ctx, enrollmentReady); err != nil {
		_ = a.shutdown()
		return err
	}

	cfg := a.configMgr.Get()

	a.log.Info("phase: identity")
	if _, err := a.identity.GetGatewayTLSConfig(); err != nil {
		a.log.Error("no mTLS credentials available", "error", err)
		return err
	}

	a.log.Info("phase: gateway")
	a.gateway = transport.NewGatewayClient(
		a.identity,
		cfg.GatewayURL,
		cfg.CircuitBreakerThreshold,
		cfg.CircuitBreakerCooldown,
		a.log,
	)
	if err := a.initGatewayWithRetry(ctx, cfg); err != nil {
		a.log.Warn("gateway unreachable, starting in degraded mode", "error", err)
	}
	if a.statusAPI != nil {
		a.statusAPI.SetGateway(a.gateway)
	}

	a.log.Info("phase: policy")
	a.fetcher = policy.NewFetcher(a.gateway, cfg.GatewayURL, cfg.PolicySyncInterval)

	payload, version, err := a.fetcher.Fetch()
	if err != nil {
		a.log.Warn("initial policy fetch failed, using default decision", "error", err)
	} else if payload != nil {
		if err := a.engine.Update(payload); err != nil {
			a.log.Error("initial policy apply failed", "error", err)
		} else {
			a.log.Info("policy loaded", "version", version, "rules", len(payload.Rules))
		}
	}

	bcfg := retry.BackoffConfig{
		InitialInterval: cfg.InitialBackoff,
		MaxInterval:     cfg.MaxBackoff,
		Multiplier:      cfg.BackoffMultiplier,
		JitterFraction:  cfg.JitterFraction,
	}
	a.syncLoop = policy.NewSyncLoop(a.fetcher, a.engine, a.collector, a.log, bcfg)
	a.gt.go_("policy-sync", a.syncLoop.Run)

	a.log.Info("phase: telemetry")
	a.emitter = telemetry.NewEmitter(a.collector, a.gateway, cfg.GatewayURL, cfg.TelemetryFlushInterval, a.log)
	a.gt.go_("telemetry-emitter", a.emitter.Run)

	a.collector.Emit("agent.started", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"protocol_version": domain.ProtocolVersion,
			"listen_addr":      cfg.ListenAddr,
		},
	})

	a.log.Info("phase: router")
	a.router = routing.NewRouter(a.engine, cfg.DefaultDecision, a.log)

	a.log.Info("phase: prompt capture")
	a.prompt = promptcapture.NewService(promptcapture.Deps{
		Config:     a.configMgr,
		Router:     a.router,
		ListenAddr: cfg.PromptCaptureListenAddr,
		Semantic: promptsemantics.NewCascadeEvaluator(promptsemantics.CascadeOptions{
			LocalURL:           cfg.PromptSemanticsLocalURL,
			GatewayURL:         cfg.GatewayURL,
			Gateway:            a.gateway,
			LocalTimeout:       cfg.PromptSemanticsLocalTimeout,
			GatewayTimeout:     cfg.PromptSemanticsGatewayTimeout,
			AmbiguousThreshold: cfg.PromptSemanticsAmbiguousThreshold,
			GatewayEnabled:     cfg.PromptSemanticsGatewayEnabled,
			Logger:             a.log,
		}),
		Metrics:  a.collector,
		Notifier: a.adapter,
		Logger:   a.log,
		OnBlockCleanup: func(req domain.PromptEvaluationRequest) {
			if req.Surface != domain.CaptureSurfaceCursor {
				return
			}
			transcriptPath := strings.TrimSpace(req.Metadata["transcript_path"])
			result := cursorcleanup.CleanBlockedPromptTranscript(transcriptPath)
			if result.RedactedCount > 0 {
				a.log.Info("post-block cleanup completed", "redacted", result.RedactedCount)
			}
			if result.Message != "" && result.RedactedCount == 0 {
				a.log.Info("post-block cleanup result", "message", result.Message)
			}
			if len(result.Errors) > 0 {
				a.log.Warn("post-block cleanup had errors", "errors", result.Errors)
			}
		},
	})
	if err := a.prompt.Start(ctx); err != nil {
		a.log.Warn("prompt capture service unavailable; adapters must fail-open", "error", err)
		a.collector.Emit("proxy.request", &domain.EventPayload{
			AgentID:   cfg.AgentID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"method":          "PROMPT",
				"host":            "local-agent",
				"path":            "/prompt-capture/service/desktop/allow/degraded_fail_open",
				"decision":        "allow",
				"status":          503,
				"capture_stage":   "service",
				"capture_surface": string(domain.CaptureSurfaceDesktop),
				"capture_outcome": string(domain.CaptureOutcomeDegradedFailOpen),
				"error":           err.Error(),
			},
		})
	} else {
		a.log.Info("prompt capture service ready", "addr", a.prompt.ListenAddr())
	}

	a.log.Info("phase: proxy")
	host, port := splitListenAddr(cfg.ListenAddr)
	proxyErrCh, err := a.startProxyWithRetry(ctx, cfg)
	if err != nil {
		a.log.Error("proxy listener failed before ready", "addr", cfg.ListenAddr, "error", err)
		_ = a.shutdown()
		return err
	}

	a.cleanStaleProxy(ctx, host, port)

	a.log.Info("phase: system proxy")
	if err := a.adapter.Register(ctx, host, port); err != nil {
		a.log.Warn("system proxy registration failed", "error", err)
	} else {
		a.log.Info("system proxy registered", "host", host, "port", port)
	}

	a.gt.go_("integrity-monitor", func(ctx context.Context) {
		a.runIntegrityMonitor(ctx, cfg)
	})

	a.gt.go_("health-check", func(ctx context.Context) {
		a.gateway.RunHealthCheck(ctx, cfg.HealthCheckInterval)
	})

	a.log.Info("agent started", "agent_id", cfg.AgentID)

	select {
	case err := <-proxyErrCh:
		if err != nil {
			a.log.Error("proxy exited unexpectedly", "error", err)
		} else {
			a.log.Warn("proxy exited unexpectedly without error")
		}
		if shutdownErr := a.shutdown(); shutdownErr != nil {
			if err != nil {
				return fmt.Errorf("proxy exit error: %w (shutdown failed: %v)", err, shutdownErr)
			}
			return shutdownErr
		}
		return err
	case <-ctx.Done():
		return a.shutdown()
	}
}

// Stop triggers graceful shutdown.
func (a *Agent) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *Agent) loadIdentityFromConfig() error {
	cfg := a.configMgr.Get()
	if cfg.CertPath == "" || cfg.KeyPath == "" || cfg.CAPath == "" {
		return fmt.Errorf("identity bootstrap incomplete: cert_path, key_path, and ca_path are required")
	}
	if err := bootstrapFromPEM(a.identity, cfg, a.log); err != nil {
		return fmt.Errorf("identity bootstrap from config: %w", err)
	}
	a.log.Info("identity bootstrapped from updated config",
		"cert_path", cfg.CertPath,
		"key_path", cfg.KeyPath,
		"ca_path", cfg.CAPath,
	)
	return nil
}

func (a *Agent) runtimePrereqError() error {
	cfg := a.configMgr.Get()
	var missing []string
	if strings.TrimSpace(cfg.AgentID) == "" {
		missing = append(missing, "agent_id")
	}
	if strings.TrimSpace(cfg.GatewayURL) == "" {
		missing = append(missing, "gateway_url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("waiting for enrollment config: missing %s", strings.Join(missing, ", "))
	}
	if _, err := a.identity.GetGatewayTLSConfig(); err != nil {
		return fmt.Errorf("waiting for enrollment credentials: %w", err)
	}
	return nil
}

func (a *Agent) waitForRuntimeReady(ctx context.Context, enrollmentReady <-chan struct{}) error {
	if err := a.runtimePrereqError(); err == nil {
		return nil
	} else {
		a.log.Info("bootstrap mode active; waiting for enrollment before starting proxy runtime", "reason", err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-enrollmentReady:
		case <-ticker.C:
		}

		if err := a.runtimePrereqError(); err == nil {
			a.log.Info("runtime prerequisites satisfied; continuing startup")
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func (a *Agent) shutdown() error {
	cfg := a.configMgr.Get()
	a.log.Info("agent shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := a.adapter.Unregister(ctx); err != nil {
		a.log.Warn("system proxy unregister failed", "error", err)
	}

	if a.proxy != nil {
		if err := a.proxy.Stop(); err != nil {
			a.log.Warn("proxy stop failed", "error", err)
		}
	}
	if a.prompt != nil {
		if err := a.prompt.Stop(); err != nil {
			a.log.Warn("prompt capture stop failed", "error", err)
		}
	}
	if a.statusAPI != nil {
		if err := a.statusAPI.Stop(); err != nil {
			a.log.Warn("status api stop failed", "error", err)
		}
	}

	// Wait for background goroutines (sync loop, emitter, monitors).
	// They observe the parent context which is already cancelled.
	a.gt.wait()

	if a.gateway != nil {
		a.gateway.Close()
	}

	a.collector.Emit("agent.stopped", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
	})

	a.log.Info("agent shutdown complete")
	return nil
}

func (a *Agent) initGatewayWithRetry(ctx context.Context, cfg *domain.AgentConfig) error {
	bcfg := retry.BackoffConfig{
		InitialInterval: cfg.InitialBackoff,
		MaxInterval:     cfg.MaxBackoff,
		Multiplier:      cfg.BackoffMultiplier,
		JitterFraction:  cfg.JitterFraction,
	}
	return retry.Do(ctx, bcfg, 3, func() error {
		return a.gateway.Init(ctx)
	})
}

func (a *Agent) newProxy() *transport.HTTPProxy {
	return transport.NewHTTPProxy(transport.ProxyDeps{
		Config:                a.configMgr,
		Router:                a.router,
		Gateway:               a.gateway,
		Resolver:              a.adapter,
		NetInfo:               a.adapter,
		CertStore:             a.adapter,
		Notifier:              a.adapter,
		Metrics:               a.collector,
		Logger:                a.log,
		PolicyVersionFn:       a.engine.Version,
		ManagedInterceptionFn: a.engine.Interception,
	})
}

func (a *Agent) startProxyWithRetry(ctx context.Context, cfg *domain.AgentConfig) (<-chan error, error) {
	const maxAttempts = 3

	host, port := splitListenAddr(cfg.ListenAddr)
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		proxy := a.newProxy()
		proxyErrCh := make(chan error, 1)

		go func() {
			proxyErrCh <- proxy.Listen(ctx, cfg.ListenAddr)
		}()

		if err := a.waitForProxyListener(ctx, host, port, proxyErrCh, 5*time.Second); err == nil {
			a.proxy = proxy
			return proxyErrCh, nil
		} else {
			lastErr = err
			_ = proxy.Stop()

			if attempt == maxAttempts {
				break
			}

			retryDelay := time.Duration(attempt) * 500 * time.Millisecond
			a.log.Warn("proxy listener not ready yet, retrying startup",
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"retry_in", retryDelay,
				"error", err,
			)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}
	}

	return nil, fmt.Errorf("proxy startup failed after %d attempts: %w", maxAttempts, lastErr)
}

func (a *Agent) runIntegrityMonitor(ctx context.Context, cfg *domain.AgentConfig) {
	ticker := time.NewTicker(cfg.IntegrityCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			host, port := splitListenAddr(cfg.ListenAddr)
			listenerAlive := a.probeListenAddr(host, port)

			intact, err := a.adapter.VerifyIntegrity()
			if err != nil {
				a.log.Warn("integrity check error", "error", err)
				continue
			}

			if intact && !listenerAlive {
				// Registry looks correct but listener is dead â€” unsafe state.
				// Unregister to restore safe proxy state rather than leaving
				// settings pointing at a dead port.
				a.log.Error("proxy registry intact but listener unreachable, unregistering to restore safe state")
				a.collector.Emit("proxy.listener_unreachable", &domain.EventPayload{
					AgentID:   cfg.AgentID,
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"host": host,
						"port": port,
					},
				})
				if err := a.adapter.Unregister(ctx); err != nil {
					a.log.Error("emergency unregister failed", "error", err)
				}
				continue
			}

			if !intact {
				a.log.Warn("proxy settings tampered")
				a.collector.Emit("proxy.tamper_detected", &domain.EventPayload{
					AgentID:   cfg.AgentID,
					Timestamp: time.Now(),
				})
				if cfg.AutoReregister {
					if !listenerAlive {
						// Listener is dead â€” do NOT re-register (would point
						// system at dead port). Log and let main loop handle exit.
						a.log.Error("proxy tampered but listener is dead, skipping re-register",
							"host", host, "port", port)
						a.collector.Emit("proxy.listener_unreachable", &domain.EventPayload{
							AgentID:   cfg.AgentID,
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"host": host,
								"port": port,
							},
						})
						continue
					}
					if err := a.adapter.Register(ctx, host, port); err != nil {
						a.log.Error("auto re-register failed", "error", err)
					} else {
						a.log.Info("proxy re-registered after tamper")
					}
				}
			}
		}
	}
}

// cleanStaleProxy checks for leftover proxy settings from a previous agent
// crash and restores safe state before fresh registration.
func (a *Agent) cleanStaleProxy(ctx context.Context, host string, port uint16) {
	state, err := a.adapter.State()
	if err != nil {
		a.log.Warn("cannot read proxy state for stale check", "error", err)
		return
	}

	if !state.Active || !state.OwnedByAgent {
		return
	}

	// Proxy is active and owned by Themisto. Check if the listener at the
	// registered address is actually reachable (bounded retry: 3 attempts).
	for attempt := 0; attempt < 3; attempt++ {
		if a.probeListenAddr(state.Host, state.Port) {
			// Listener is alive â€” previous agent may still be running, or we
			// are about to bind on the same address. Either way, fresh
			// Register will handle it (idempotent or conflict).
			return
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Stale proxy confirmed: owned by us, listener unreachable after retries.
	a.log.Warn("stale proxy detected from previous crash, unregistering to restore safe state",
		"host", state.Host, "port", state.Port)
	a.collector.Emit("proxy.stale_cleanup", &domain.EventPayload{
		AgentID:   a.configMgr.Get().AgentID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"host": state.Host,
			"port": state.Port,
		},
	})
	if err := a.adapter.Unregister(ctx); err != nil {
		a.log.Error("stale proxy cleanup failed", "error", err)
	}
}

// probeListenAddr performs a TCP dial to check if a listener is reachable.
// This is a practical liveness check (TCP connect), not full proxy-functionality
// verification.
func (a *Agent) probeListenAddr(host string, port uint16) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	conn, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func splitListenAddr(addr string) (string, uint16) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1", 8080
	}
	p, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return host, 8080
	}
	return host, uint16(p)
}

func (a *Agent) waitForProxyListener(ctx context.Context, host string, port uint16, proxyErrCh <-chan error, timeout time.Duration) error {
	if host == "" {
		host = "127.0.0.1"
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-proxyErrCh:
			if err == nil {
				return fmt.Errorf("proxy listener stopped before becoming ready")
			}
			return err
		default:
		}

		conn, err := net.DialTimeout("tcp", target, 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("proxy listener did not become ready on %s within %s", target, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// bootstrapFromPEM reads PEM certificate files, converts to DER, and loads
// them into the identity store via Bootstrap.
func bootstrapFromPEM(store *identity.IdentityStore, cfg *domain.AgentConfig, logger log.Logger) error {
	certPEM, err := os.ReadFile(cfg.CertPath)
	if err != nil {
		return fmt.Errorf("read cert: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("no PEM block in cert file %s", cfg.CertPath)
	}

	keyPEM, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("no PEM block in key file %s", cfg.KeyPath)
	}

	caPEM, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		return fmt.Errorf("read CA chain: %w", err)
	}
	// Validate at least one PEM block exists.
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil {
		return fmt.Errorf("no PEM block in CA chain file %s", cfg.CAPath)
	}

	clientCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse client certificate: %w", err)
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA certificate: %w", err)
	}

	// Pass the full caPEM (may contain issuing CA + root CA) so that
	// BuildTLSConfig can load the complete chain via AppendCertsFromPEM.
	store.Bootstrap(caCert.Subject.CommonName, clientCert.Subject.CommonName,
		certBlock.Bytes, keyBlock.Bytes, caPEM)
	return nil
}
