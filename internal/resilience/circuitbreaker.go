package resilience

import (
	"fmt"
	"sync"
	"time"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

type CBConfig struct {
	Name             string
	MaxFailures      int
	OpenTimeout      time.Duration
	HalfOpenMaxCalls int
	SuccessThreshold int
}

// fsm (finite state machine) based circuit breaker
type CircuitBreaker struct {
	cfg              CBConfig
	mu               sync.Mutex
	state            api.CircuitState
	consecutiveFails int
	consecutiveOK    int
	halfOpenCalls    int
	openedAt         time.Time
}

func NewCircuitBreaker(cfg CBConfig) *CircuitBreaker {
	// def values
	if cfg.MaxFailures == 0 {
		cfg.MaxFailures = 5
	}
	if cfg.OpenTimeout == 0 {
		cfg.OpenTimeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxCalls == 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = 1
	}

	return &CircuitBreaker{
		cfg:   cfg,
		state: api.StateClosed,
	}
}

func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case api.StateClosed:
		return nil
	case api.StateOpen:
		if time.Since(cb.openedAt) >= cb.cfg.OpenTimeout {
			cb.transitionTo(api.StateHalfOpen)
			cb.halfOpenCalls++
			return nil
		}
		return fmt.Errorf("%w: upstream %q", api.ErrCircuitOpen, cb.cfg.Name)
	case api.StateHalfOpen:
		if cb.halfOpenCalls >= cb.cfg.HalfOpenMaxCalls {
			return fmt.Errorf("%w: upstream %q (half-open limit)", api.ErrCircuitOpen, cb.cfg.Name)
		}
		cb.halfOpenCalls++
		return nil
	}
	return nil
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails = 0
	if cb.state == api.StateHalfOpen {
		cb.halfOpenCalls--
		cb.consecutiveOK++
		if cb.consecutiveOK >= cb.cfg.SuccessThreshold {
			cb.transitionTo(api.StateClosed)
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case api.StateClosed:
		cb.consecutiveFails++
		if cb.consecutiveFails >= cb.cfg.MaxFailures {
			cb.transitionTo(api.StateOpen)
		}
	case api.StateHalfOpen:
		cb.halfOpenCalls--
		cb.transitionTo(api.StateOpen)
	}
}

func (cb *CircuitBreaker) transitionTo(next api.CircuitState) {
	cb.state = next
	switch next {
	case api.StateClosed:
		cb.consecutiveFails, cb.consecutiveOK, cb.halfOpenCalls = 0, 0, 0
	case api.StateOpen:
		cb.openedAt = time.Now()
		cb.consecutiveOK, cb.halfOpenCalls = 0, 0
	case api.StateHalfOpen:
		cb.consecutiveOK, cb.halfOpenCalls = 0, 0
	}
}
