package unified

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/policy"
	"github.com/themisto/agent/core/routing"
)

// Service is the top-level unified development service. It wires the
// proxy, policy engine, forwarder, and telemetry into a single process.
//
// TODO(gateway-split): When splitting back to agent+gateway, this type
// is replaced by core.Agent (agent side) and a separate gateway binary.
// The interfaces below (Router, Forwarder, Telemetry) remain the same;
// only the wiring changes.
type Service struct {
	cfg       *DevConfig
	telem     *DevTelemetry
	engine    *policy.DefaultEngine
	router    *routing.DefaultRouter
	forwarder *DirectForwarder
	proxy     *DevProxy
	server    *http.Server
}

// New creates a fully wired unified service from the given config.
func New(cfg *DevConfig) (*Service, error) {
	telem, err := NewDevTelemetry(cfg)
	if err != nil {
		return nil, fmt.Errorf("init telemetry: %w", err)
	}

	// Policy engine — stub allow-all by default; load from file if provided.
	engine := policy.NewEngine(domain.DecisionForward)
	policyPayload, err := LoadPolicyFromFile(cfg.PolicyFile)
	if err != nil {
		telem.Warn("failed to load policy file, using default allow-all", "error", err)
		policyPayload = &domain.PolicyPayload{Version: "dev-default"}
	}
	if err := engine.Update(policyPayload); err != nil {
		telem.Warn("failed to apply policy, using default", "error", err)
	}

	router := routing.NewRouter(engine, domain.DecisionForward, telem)
	forwarder := NewDirectForwarder()
	proxy := NewDevProxy(router, forwarder, telem, cfg, engine.Version())

	return &Service{
		cfg:       cfg,
		telem:     telem,
		engine:    engine,
		router:    router,
		forwarder: forwarder,
		proxy:     proxy,
	}, nil
}

// Run starts the proxy and blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	s.server = &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.proxy,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	s.telem.Info("unified service starting",
		"listen_addr", s.cfg.ListenAddr,
		"policy_version", s.engine.Version(),
		"log_output", s.cfg.LogOutput,
	)

	s.telem.Emit("agent.started", &domain.EventPayload{
		AgentID:   "dev-agent",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"mode":           "unified-dev",
			"protocol":       domain.ProtocolVersion,
			"policy_version": s.engine.Version(),
		},
	})

	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.telem.Info("proxy listening — configure your browser/client to use this proxy",
		"addr", s.cfg.ListenAddr,
		"example", fmt.Sprintf("curl --proxy http://%s http://example.com", s.cfg.ListenAddr),
	)

	errCh := make(chan error, 1)
	go func() { errCh <- s.server.Serve(ln) }()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Service) shutdown() error {
	s.telem.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.server != nil {
		s.server.Shutdown(ctx)
	}
	s.forwarder.Close()

	s.telem.Emit("agent.stopped", &domain.EventPayload{
		AgentID:   "dev-agent",
		Timestamp: time.Now(),
	})
	s.telem.Info("shutdown complete")
	return nil
}
