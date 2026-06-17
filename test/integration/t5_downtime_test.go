package integration

import (
	"context"
	"testing"
	"time"

	"github.com/themisto/agent/pkg/retry"
	"github.com/themisto/agent/test/integration/testutil"
)

// T5.1 — Gateway unreachable at startup: retry with backoff.
func TestT5_1_GatewayUnreachableRetry(t *testing.T) {
	bcfg := retry.BackoffConfig{
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     200 * time.Millisecond,
		Multiplier:      2.0,
		JitterFraction:  0.1,
	}

	attempts := 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := retry.Do(ctx, bcfg, 3, func() error {
		attempts++
		return context.DeadlineExceeded // simulate unreachable gateway
	})

	testutil.AssertError(t, err, "should fail after retries")
	testutil.AssertEqual(t, attempts, 4, "initial + 3 retries = 4 attempts")
}

// T5.2 — Circuit breaker opens after threshold failures.
func TestT5_2_CircuitBreakerOpens(t *testing.T) {
	cb := retry.NewCircuitBreaker(3, 100*time.Millisecond)

	// Three failures should open the circuit.
	for i := 0; i < 3; i++ {
		testutil.AssertTrue(t, cb.Allow(), "should allow before threshold")
		cb.RecordFailure()
	}

	testutil.AssertFalse(t, cb.Allow(), "circuit should be open")
	testutil.AssertEqual(t, cb.State(), retry.StateOpen, "state is open")
}

// T5.3 — Circuit breaker recovers after cooldown.
func TestT5_3_CircuitBreakerRecovery(t *testing.T) {
	cb := retry.NewCircuitBreaker(2, 50*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	testutil.AssertEqual(t, cb.State(), retry.StateOpen, "state is open")

	// Wait for cooldown.
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open and allow one probe.
	testutil.AssertTrue(t, cb.Allow(), "half-open allows probe")

	// Success closes the circuit.
	cb.RecordSuccess()
	testutil.AssertEqual(t, cb.State(), retry.StateClosed, "state is closed")
	testutil.AssertTrue(t, cb.Allow(), "closed allows requests")
}

// T5.4 — Backoff is capped at MaxInterval.
func TestT5_4_BackoffCapped(t *testing.T) {
	bcfg := retry.BackoffConfig{
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      10.0,
		JitterFraction:  0.0,
	}

	// After many attempts, delay should not exceed MaxInterval.
	delay := bcfg.Delay(100)
	if delay > 100*time.Millisecond {
		t.Errorf("delay %v exceeds max %v", delay, 100*time.Millisecond)
	}
}

// T5 — Context cancellation stops retry loop.
func TestT5_ContextCancellationStopsRetry(t *testing.T) {
	bcfg := retry.BackoffConfig{
		InitialInterval: 1 * time.Second,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := retry.Do(ctx, bcfg, -1, func() error {
		attempts++
		return context.DeadlineExceeded
	})

	testutil.AssertError(t, err, "should return context error")
	testutil.AssertTrue(t, attempts >= 1, "at least one attempt made")
}
