package directmode

import (
	"context"
	"errors"
	"testing"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
)

func TestNewCircuitBreakerManager(t *testing.T) {
	manager := NewCircuitBreakerManager()
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.breakers)
	assert.Equal(t, 0, len(manager.breakers))
}

func TestCircuitBreakerManager_GetBreaker(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"

	breaker := manager.GetBreaker(serverURL)
	assert.NotNil(t, breaker)
	assert.Equal(t, gobreaker.StateClosed, breaker.State())

	// Getting same breaker should return existing one
	breaker2 := manager.GetBreaker(serverURL)
	assert.Equal(t, breaker, breaker2)
}

func TestCircuitBreakerManager_Execute_Success(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"
	ctx := context.Background()

	result, err := manager.Execute(ctx, serverURL, func() (interface{}, error) {
		return "success", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "success", result)

	// Breaker should remain closed
	assert.Equal(t, gobreaker.StateClosed, manager.GetState(serverURL))
}

func TestCircuitBreakerManager_Execute_Failure(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"
	ctx := context.Background()

	_, err := manager.Execute(ctx, serverURL, func() (interface{}, error) {
		return nil, errors.New("test error")
	})

	assert.Error(t, err)
	assert.Equal(t, gobreaker.StateClosed, manager.GetState(serverURL)) // Still closed after 1 failure
}

func TestCircuitBreakerManager_Execute_OpenState(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"
	ctx := context.Background()

	// Simulate multiple failures to trip the breaker
	for i := 0; i < 10; i++ {
		manager.Execute(ctx, serverURL, func() (interface{}, error) {
			return nil, errors.New("test error")
		})
	}

	// Breaker should be open now
	state := manager.GetState(serverURL)
	assert.Equal(t, gobreaker.StateOpen, state)

	// Next request should be blocked
	_, err := manager.Execute(ctx, serverURL, func() (interface{}, error) {
		return "success", nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open")
}

func TestCircuitBreakerManager_GetState(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"

	// State should be closed for non-existent breaker
	state := manager.GetState(serverURL)
	assert.Equal(t, gobreaker.StateClosed, state)

	// Create breaker
	manager.GetBreaker(serverURL)
	state = manager.GetState(serverURL)
	assert.Equal(t, gobreaker.StateClosed, state)
}

func TestCircuitBreakerManager_GetMetrics(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL1 := "http://localhost:8082"
	serverURL2 := "http://localhost:8083"
	ctx := context.Background()

	// Execute some requests
	manager.Execute(ctx, serverURL1, func() (interface{}, error) {
		return "success", nil
	})
	manager.Execute(ctx, serverURL2, func() (interface{}, error) {
		return nil, errors.New("error")
	})

	metrics := manager.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, 2, len(metrics))

	// Check server1 metrics
	server1Metrics := metrics[serverURL1].(map[string]interface{})
	assert.Equal(t, "closed", server1Metrics["state"])
	assert.Equal(t, uint32(1), server1Metrics["requests"])
	assert.Equal(t, uint32(1), server1Metrics["total_successes"])

	// Check server2 metrics
	server2Metrics := metrics[serverURL2].(map[string]interface{})
	assert.Equal(t, "closed", server2Metrics["state"])
	assert.Equal(t, uint32(1), server2Metrics["total_failures"])
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"
	ctx := context.Background()

	// Initial state: Closed
	assert.Equal(t, gobreaker.StateClosed, manager.GetState(serverURL))

	// Trigger failures
	for i := 0; i < 10; i++ {
		manager.Execute(ctx, serverURL, func() (interface{}, error) {
			return nil, errors.New("failure")
		})
	}

	// Should be Open
	assert.Equal(t, gobreaker.StateOpen, manager.GetState(serverURL))

	// Wait for timeout (breaker moves to half-open)
	// Note: This would require waiting 30s in real scenario
	// For test purposes, we just verify the state
}

// BenchmarkCircuitBreaker_Execute benchmarks breaker execution
func BenchmarkCircuitBreaker_Execute(b *testing.B) {
	manager := NewCircuitBreakerManager()
	serverURL := "http://localhost:8082"
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.Execute(ctx, serverURL, func() (interface{}, error) {
			return "success", nil
		})
	}
}

