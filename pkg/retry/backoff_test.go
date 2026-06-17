package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelay_Exponential(t *testing.T) {
	cfg := BackoffConfig{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		JitterFraction:  0.0,
	}
	d0 := cfg.Delay(0)
	d1 := cfg.Delay(1)
	d2 := cfg.Delay(2)

	if d0 != 100*time.Millisecond {
		t.Errorf("delay(0) = %v, want 100ms", d0)
	}
	if d1 != 200*time.Millisecond {
		t.Errorf("delay(1) = %v, want 200ms", d1)
	}
	if d2 != 400*time.Millisecond {
		t.Errorf("delay(2) = %v, want 400ms", d2)
	}
}

func TestDelay_Capped(t *testing.T) {
	cfg := BackoffConfig{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     500 * time.Millisecond,
		Multiplier:      10.0,
		JitterFraction:  0.0,
	}
	d := cfg.Delay(5)
	if d > 500*time.Millisecond {
		t.Errorf("delay(5) = %v, exceeds max 500ms", d)
	}
}

func TestDelay_WithJitter(t *testing.T) {
	cfg := BackoffConfig{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		JitterFraction:  0.5,
	}
	// With 50% jitter, delay(0) should be in [50ms, 150ms].
	for i := 0; i < 100; i++ {
		d := cfg.Delay(0)
		if d < 50*time.Millisecond || d > 150*time.Millisecond {
			t.Errorf("delay(0) = %v, outside jitter range [50ms, 150ms]", d)
		}
	}
}

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	cfg := BackoffConfig{InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Multiplier: 1.0}
	attempts := 0
	err := Do(context.Background(), cfg, 3, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	cfg := BackoffConfig{InitialInterval: time.Millisecond, MaxInterval: 10 * time.Millisecond, Multiplier: 1.0}
	attempts := 0
	err := Do(context.Background(), cfg, 5, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestDo_ExhaustsRetries(t *testing.T) {
	cfg := BackoffConfig{InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Multiplier: 1.0}
	attempts := 0
	err := Do(context.Background(), cfg, 2, func() error {
		attempts++
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 3 { // 1 initial + 2 retries
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	cfg := BackoffConfig{InitialInterval: time.Second, MaxInterval: time.Second, Multiplier: 1.0}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Do(ctx, cfg, -1, func() error {
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
