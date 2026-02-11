package redis

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Cooldown:         1 * time.Millisecond,
	})

	if cb.State() != CircuitClosed {
		t.Error("initial state should be closed")
	}

	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Error("successful operation should not error")
	}
}

func TestCircuitBreakerOpensOnFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
		Cooldown:         0,
	})

	testErr := errors.New("test error")

	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return testErr
		})
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected circuit to be open, got %d", cb.State())
	}

	err := cb.Execute(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Error("requests should be rejected when circuit is open")
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		Cooldown:         0,
	})

	_ = cb.Execute(func() error { return nil })
	_ = cb.Execute(func() error { return errors.New("fail") })
	_ = cb.Execute(func() error { return nil })

	state, _, _ := cb.Stats()

	if state != CircuitClosed {
		t.Error("should still be closed with few failures")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()

	if cfg.FailureThreshold != 5 {
		t.Errorf("expected failure threshold 5, got %d", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 2 {
		t.Errorf("expected success threshold 2, got %d", cfg.SuccessThreshold)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout)
	}
}
