package telemetry

import (
	"sync"
	"testing"
	"time"

	"github.com/themisto/agent/core/domain"
)

func TestCollector_Counter(t *testing.T) {
	c := NewCollector(100)
	c.Counter("req.total", 5, map[string]string{"method": "GET"})

	entries := c.Drain()
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Kind != kindCounter {
		t.Errorf("kind = %d, want counter", e.Kind)
	}
	if e.Name != "req.total" {
		t.Errorf("name = %q", e.Name)
	}
	if e.IValue != 5 {
		t.Errorf("ivalue = %d, want 5", e.IValue)
	}
	if e.Labels["method"] != "GET" {
		t.Errorf("label method = %q", e.Labels["method"])
	}
}

func TestCollector_Gauge(t *testing.T) {
	c := NewCollector(100)
	c.Gauge("cpu.usage", 0.75, nil)

	entries := c.Drain()
	if entries[0].FValue != 0.75 {
		t.Errorf("fvalue = %f, want 0.75", entries[0].FValue)
	}
}

func TestCollector_Histogram(t *testing.T) {
	c := NewCollector(100)
	c.Histogram("latency.ms", 42.0, nil)

	entries := c.Drain()
	if entries[0].FValue != 42.0 {
		t.Errorf("fvalue = %f, want 42.0", entries[0].FValue)
	}
}

func TestCollector_Emit(t *testing.T) {
	c := NewCollector(100)
	err := c.Emit("agent.started", &domain.EventPayload{
		AgentID:   "a1",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := c.Drain()
	if entries[0].Kind != kindEvent {
		t.Errorf("kind = %d, want event", entries[0].Kind)
	}
	if entries[0].Payload.AgentID != "a1" {
		t.Error("payload agent ID mismatch")
	}
}

func TestCollector_RingBufferOverflow(t *testing.T) {
	c := NewCollector(3)
	for i := 0; i < 5; i++ {
		c.Counter("m", int64(i), nil)
	}
	if c.Size() != 3 {
		t.Errorf("size = %d, want 3", c.Size())
	}
	entries := c.Drain()
	// Should have the last 3: 2, 3, 4.
	for i, e := range entries {
		want := int64(2 + i)
		if e.IValue != want {
			t.Errorf("entry[%d] = %d, want %d", i, e.IValue, want)
		}
	}
}

func TestCollector_DrainEmpty(t *testing.T) {
	c := NewCollector(10)
	entries := c.Drain()
	if entries != nil {
		t.Errorf("drain of empty collector should return nil, got %d entries", len(entries))
	}
}

func TestCollector_Peek(t *testing.T) {
	c := NewCollector(10)
	for i := 0; i < 5; i++ {
		c.Counter("m", int64(i), nil)
	}

	// Peek should not drain
	peeked := c.Peek(3)
	if len(peeked) != 3 {
		t.Fatalf("peek len = %d, want 3", len(peeked))
	}
	// Should be the 3 most recent: 2, 3, 4
	for i, e := range peeked {
		want := int64(2 + i)
		if e.IValue != want {
			t.Errorf("peek[%d] = %d, want %d", i, e.IValue, want)
		}
	}

	// Size should remain unchanged
	if c.Size() != 5 {
		t.Errorf("size after peek = %d, want 5", c.Size())
	}

	// Peek with limit > size returns all
	all := c.Peek(100)
	if len(all) != 5 {
		t.Errorf("peek all len = %d, want 5", len(all))
	}

	// Peek with 0 limit returns all
	all0 := c.Peek(0)
	if len(all0) != 5 {
		t.Errorf("peek 0 limit len = %d, want 5", len(all0))
	}
}

func TestCollector_PeekEmpty(t *testing.T) {
	c := NewCollector(10)
	peeked := c.Peek(5)
	if peeked != nil {
		t.Errorf("peek of empty collector should return nil, got %d", len(peeked))
	}
}

func TestCollector_ConcurrentSafety(t *testing.T) {
	c := NewCollector(10000)
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				c.Counter("c", 1, nil)
			}
		}()
	}
	wg.Wait()
	if c.Size() != 1000 {
		t.Errorf("size = %d, want 1000", c.Size())
	}
}
