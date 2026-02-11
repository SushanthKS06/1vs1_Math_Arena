package redis

import (
	"errors"
	"sync"
	"time"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

type CircuitBreaker struct {
	mu sync.RWMutex

	state           CircuitState
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	lastStateChange time.Time

	failureThreshold int
	successThreshold int
	timeout          time.Duration
	cooldown         time.Duration
}

type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	Cooldown         time.Duration
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		Cooldown:         5 * time.Second,
	}
}

func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		cooldown:         cfg.Cooldown,
		lastStateChange:  time.Now(),
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailureTime
	cb.mu.RUnlock()

	switch state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(lastFailure) > cb.timeout {
			cb.transitionTo(CircuitHalfOpen)
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()
	cb.successCount = 0

	if cb.state == CircuitHalfOpen {
		cb.setState(CircuitOpen)
	} else if cb.failureCount >= cb.failureThreshold {
		cb.setState(CircuitOpen)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++

	if cb.state == CircuitHalfOpen && cb.successCount >= cb.successThreshold {
		cb.setState(CircuitClosed)
	}

	if cb.state == CircuitClosed {
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) transitionTo(state CircuitState) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(state)
}

func (cb *CircuitBreaker) setState(state CircuitState) {
	if time.Since(cb.lastStateChange) < cb.cooldown {
		return
	}
	cb.state = state
	cb.lastStateChange = time.Now()
	if state == CircuitClosed {
		cb.failureCount = 0
		cb.successCount = 0
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Stats() (state CircuitState, failures, successes int) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state, cb.failureCount, cb.successCount
}
