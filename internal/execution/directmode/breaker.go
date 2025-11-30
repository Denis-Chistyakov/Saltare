package directmode

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker"
)

// CircuitBreakerManager manages circuit breakers for MCP servers
type CircuitBreakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*gobreaker.CircuitBreaker
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*gobreaker.CircuitBreaker),
	}
}

// GetBreaker returns or creates a circuit breaker for a server
func (m *CircuitBreakerManager) GetBreaker(serverURL string) *gobreaker.CircuitBreaker {
	// First try read lock for existing breaker
	m.mu.RLock()
	if breaker, exists := m.breakers[serverURL]; exists {
		m.mu.RUnlock()
		return breaker
	}
	m.mu.RUnlock()

	// Need to create - acquire write lock
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists := m.breakers[serverURL]; exists {
		return breaker
	}

	settings := gobreaker.Settings{
		Name:        serverURL,
		MaxRequests: 3,                // Max requests allowed in half-open state
		Interval:    10 * time.Second, // Window for failure counting
		Timeout:     30 * time.Second, // Duration of open state before half-open
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Trip breaker after 5 consecutive failures
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.6
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Warn().
				Str("server", name).
				Str("from", from.String()).
				Str("to", to.String()).
				Msg("Circuit breaker state changed")
		},
	}

	breaker := gobreaker.NewCircuitBreaker(settings)
	m.breakers[serverURL] = breaker

	log.Info().Str("server", serverURL).Msg("Circuit breaker created")

	return breaker
}

// Execute wraps a function call with circuit breaker protection
func (m *CircuitBreakerManager) Execute(ctx context.Context, serverURL string, fn func() (interface{}, error)) (interface{}, error) {
	breaker := m.GetBreaker(serverURL)

	result, err := breaker.Execute(func() (interface{}, error) {
		return fn()
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			log.Error().
				Str("server", serverURL).
				Msg("Circuit breaker open, request blocked")
			return nil, fmt.Errorf("circuit breaker open for %s: too many failures", serverURL)
		}
		if err == gobreaker.ErrTooManyRequests {
			log.Warn().
				Str("server", serverURL).
				Msg("Circuit breaker half-open, too many requests")
			return nil, fmt.Errorf("circuit breaker limiting requests for %s", serverURL)
		}
		return nil, err
	}

	return result, nil
}

// GetState returns the current state of a circuit breaker
func (m *CircuitBreakerManager) GetState(serverURL string) gobreaker.State {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if breaker, exists := m.breakers[serverURL]; exists {
		return breaker.State()
	}
	return gobreaker.StateClosed
}

// GetMetrics returns metrics for all circuit breakers
func (m *CircuitBreakerManager) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]interface{})

	for serverURL, breaker := range m.breakers {
		counts := breaker.Counts()
		metrics[serverURL] = map[string]interface{}{
			"state":                 breaker.State().String(),
			"requests":              counts.Requests,
			"total_successes":       counts.TotalSuccesses,
			"total_failures":        counts.TotalFailures,
			"consecutive_successes": counts.ConsecutiveSuccesses,
			"consecutive_failures":  counts.ConsecutiveFailures,
		}
	}

	return metrics
}
