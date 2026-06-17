package policy

import (
	"context"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/retry"
)

// SyncLoop runs a background goroutine that periodically fetches policy from
// the gateway and feeds updates into the Engine.
type SyncLoop struct {
	fetcher   Fetcher
	engine    *DefaultEngine
	collector *telemetry.Collector
	log       log.Logger
	backoff   retry.BackoffConfig
}

// NewSyncLoop creates a sync loop wired to the given fetcher and engine.
func NewSyncLoop(f Fetcher, e *DefaultEngine, c *telemetry.Collector, logger log.Logger, bcfg retry.BackoffConfig) *SyncLoop {
	return &SyncLoop{
		fetcher:   f,
		engine:    e,
		collector: c,
		log:       logger,
		backoff:   bcfg,
	}
}

// Run blocks until ctx is cancelled. It fetches policy on the configured
// interval, applies updates, and emits telemetry events.
func (s *SyncLoop) Run(ctx context.Context) {
	s.log.Info("policy sync loop started")
	defer s.log.Info("policy sync loop stopped")

	ticker := time.NewTicker(s.fetcher.PollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnce(ctx)
			// Re-read interval in case the gateway changed it.
			ticker.Reset(s.fetcher.PollInterval())
		}
	}
}

func (s *SyncLoop) syncOnce(ctx context.Context) {
	payload, version, err := s.fetcher.Fetch()
	if err != nil {
		s.log.Warn("policy sync failed", "error", err)
		s.collector.Emit("policy.sync_failed", &domain.EventPayload{
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"error": err.Error()},
		})
		return
	}

	// 304 Not Modified — no new policy.
	if payload == nil {
		s.log.Debug("policy unchanged", "version", version)
		return
	}

	if err := s.engine.Update(payload); err != nil {
		s.log.Error("policy update failed", "version", version, "error", err)
		s.collector.Emit("policy.sync_failed", &domain.EventPayload{
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"error": err.Error(), "version": version},
		})
		return
	}

	s.log.Info("policy updated", "version", version, "rules", len(payload.Rules))
	s.collector.Emit("policy.updated", &domain.EventPayload{
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"version": version, "rule_count": len(payload.Rules)},
	})
}
