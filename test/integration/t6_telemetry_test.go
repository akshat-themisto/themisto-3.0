package integration

import (
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/test/integration/testutil"
)

// T6.1 — Metrics are buffered in the collector.
func TestT6_1_MetricsBuffered(t *testing.T) {
	c := telemetry.NewCollector(1000)

	c.Counter("proxy.requests.total", 1, nil)
	c.Counter("proxy.requests.forwarded", 1, nil)
	c.Counter("proxy.requests.blocked", 1, nil)
	c.Histogram("proxy.latency.ms", 42.5, map[string]string{"host": "example.com"})
	c.Gauge("gateway.health", 1.0, nil)

	testutil.AssertEqual(t, c.Size(), 5, "5 entries buffered")

	entries := c.Drain()
	testutil.AssertEqual(t, len(entries), 5, "drain returns 5 entries")
	testutil.AssertEqual(t, c.Size(), 0, "buffer empty after drain")
}

// T6.2 — Events are buffered in the collector.
func TestT6_2_EventsBuffered(t *testing.T) {
	c := telemetry.NewCollector(1000)

	c.Emit("agent.started", &domain.EventPayload{
		AgentID:   "test-001",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"version": "2025.01"},
	})
	c.Emit("policy.updated", &domain.EventPayload{
		AgentID:   "test-001",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"version": "v1", "rule_count": 3},
	})

	testutil.AssertEqual(t, c.Size(), 2, "2 events buffered")

	entries := c.Drain()
	testutil.AssertEqual(t, len(entries), 2, "drain returns 2")
}

// T6.3 — Ring buffer drops oldest entries when full.
func TestT6_3_RingBufferOverflow(t *testing.T) {
	c := telemetry.NewCollector(5) // tiny buffer

	for i := 0; i < 10; i++ {
		c.Counter("metric", int64(i), nil)
	}

	// Buffer holds at most 5 entries.
	testutil.AssertEqual(t, c.Size(), 5, "capped at buffer size")

	entries := c.Drain()
	testutil.AssertEqual(t, len(entries), 5, "drain returns 5")

	// The oldest entries (0–4) were dropped; we should have 5–9.
	// Verify the entries are the most recent ones.
	for i, e := range entries {
		expected := int64(5 + i)
		if e.IValue != expected {
			t.Errorf("entry %d: got value %d, want %d", i, e.IValue, expected)
		}
	}
}

// T6 — Collector is safe for concurrent use.
func TestT6_ConcurrentAccess(t *testing.T) {
	c := telemetry.NewCollector(10000)
	done := make(chan struct{})

	// 10 goroutines writing simultaneously.
	for g := 0; g < 10; g++ {
		go func(id int) {
			for i := 0; i < 100; i++ {
				c.Counter("concurrent.metric", 1, map[string]string{"goroutine": string(rune('0' + id))})
			}
			done <- struct{}{}
		}(g)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	testutil.AssertEqual(t, c.Size(), 1000, "all 1000 entries buffered")
}
