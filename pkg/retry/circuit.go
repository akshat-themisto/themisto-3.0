package retry

import (
	"sync"
	"time"
)

// CircuitState represents the circuit breaker's state.
type CircuitState int

const (
	StateClosed   CircuitState = iota // Normal — requests flow.
	StateOpen                        // Tripped — requests rejected.
	StateHalfOpen                    // Probing — single request allowed.
)

// String returns a human-readable label.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu          sync.Mutex
	threshold   int
	cooldown    time.Duration
	failures    int
	state       CircuitState
	openedAt    time.Time
	halfOpenCAS bool // true if half-open probe is in-flight
}

// NewCircuitBreaker creates a breaker that opens after threshold consecutive
// failures and stays open for cooldown before allowing a half-open probe.
func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// Allow reports whether a request should be attempted. In the open state it
// returns false until the cooldown expires, at which point it transitions to
// half-open and allows exactly one probe.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.cooldown {
			cb.state = StateHalfOpen
			cb.halfOpenCAS = true
			return true
		}
		return false
	case StateHalfOpen:
		if cb.halfOpenCAS {
			cb.halfOpenCAS = false
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful call. Resets the breaker to closed.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = StateClosed
}

// RecordFailure records a failed call. If the threshold is reached the
// breaker opens. In half-open state a single failure re-opens immediately.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.threshold {
			cb.state = StateOpen
			cb.openedAt = time.Now()
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.openedAt = time.Now()
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.cooldown {
		return StateHalfOpen
	}
	return cb.state
}
