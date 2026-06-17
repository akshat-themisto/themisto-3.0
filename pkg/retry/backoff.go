// Package retry provides exponential backoff and circuit breaker primitives.
package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// BackoffConfig controls exponential backoff behaviour.
type BackoffConfig struct {
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	JitterFraction  float64 // ±fraction of computed delay (e.g. 0.2 = ±20%)
}

// Delay returns the backoff duration for the given zero-based attempt.
func (c *BackoffConfig) Delay(attempt int) time.Duration {
	delay := float64(c.InitialInterval) * math.Pow(c.Multiplier, float64(attempt))
	if delay > float64(c.MaxInterval) {
		delay = float64(c.MaxInterval)
	}
	if c.JitterFraction > 0 {
		jitter := delay * c.JitterFraction * (2*rand.Float64() - 1)
		delay += jitter
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// Do executes fn up to maxRetries times (0 = one attempt, no retries).
// It sleeps between attempts using the configured backoff. If maxRetries < 0
// the loop is unbounded (useful for background sync loops) and only stops
// when fn succeeds or ctx is cancelled.
func Do(ctx context.Context, cfg BackoffConfig, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if maxRetries >= 0 && attempt >= maxRetries {
			return err
		}
		delay := cfg.Delay(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
