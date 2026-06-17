package retry

import (
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedByDefault(t *testing.T) {
	cb := NewCircuitBreaker(5, time.Second)
	if cb.State() != StateClosed {
		t.Errorf("state = %v, want closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("closed breaker should allow")
	}
}

func TestCircuitBreaker_OpensAtThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		cb.Allow()
		cb.RecordFailure()
	}
	if cb.State() != StateOpen {
		t.Errorf("state = %v, want open", cb.State())
	}
	if cb.Allow() {
		t.Error("open breaker should reject")
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("state = %v, want closed after success", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	time.Sleep(60 * time.Millisecond)

	if cb.State() != StateHalfOpen {
		t.Errorf("state = %v, want half-open after cooldown", cb.State())
	}
	if !cb.Allow() {
		t.Error("half-open should allow one probe")
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure()

	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open probe
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want open after half-open failure", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure()

	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("state = %v, want closed after half-open success", cb.State())
	}
}
