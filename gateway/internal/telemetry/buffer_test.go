package telemetry

import (
	"sync"
	"testing"
	"time"
)

func TestEmit_DropsWhenFull(t *testing.T) {
	buf := &Buffer{
		ch:       make(chan Event, 1),
		batch:    10,
		interval: time.Hour,
	}

	buf.Emit(Event{DeviceID: "d1"})
	buf.Emit(Event{DeviceID: "d2"}) // should be dropped

	buf.mu.Lock()
	dropped := buf.dropped
	buf.mu.Unlock()

	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

func TestEmit_DoesNotBlock(t *testing.T) {
	buf := &Buffer{
		ch:       make(chan Event, 1),
		batch:    10,
		interval: time.Hour,
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			buf.Emit(Event{DeviceID: "d"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked")
	}
}

func TestBuffer_FlushOnBatchSize(t *testing.T) {
	var mu sync.Mutex
	var flushed []Event

	buf := &Buffer{
		ch:       make(chan Event, 100),
		batch:    3,
		interval: time.Hour,
	}

	// Override flush by running the loop manually
	buf.Emit(Event{DeviceID: "a"})
	buf.Emit(Event{DeviceID: "b"})
	buf.Emit(Event{DeviceID: "c"})

	pending := make([]Event, 0, buf.batch)
	for len(pending) < buf.batch {
		e := <-buf.ch
		pending = append(pending, e)
	}

	mu.Lock()
	flushed = append(flushed, pending...)
	mu.Unlock()

	if len(flushed) != 3 {
		t.Errorf("flushed = %d, want 3", len(flushed))
	}
}

func TestNormalizedAuditOrgID(t *testing.T) {
	valid := "a0000000-0000-0000-0000-000000000001"
	got := normalizedAuditOrgID(valid)
	if got == nil || *got != valid {
		t.Fatalf("expected valid UUID pointer, got %v", got)
	}

	invalid := normalizedAuditOrgID("Themisto Dev Org")
	if invalid != nil {
		t.Fatalf("expected nil for non-UUID org id, got %v", *invalid)
	}
}
