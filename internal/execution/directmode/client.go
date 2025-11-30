package directmode

// Package directmode implements Direct Mode execution - calling MCP servers directly via HTTP.
// Includes connection pooling, circuit breakers, and retry logic for reliable tool execution.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/rs/zerolog/log"
)

// DirectExecutor executes tools via MCP protocol
type DirectExecutor struct {
	pools   map[string]*ConnectionPool
	breaker *CircuitBreakerManager
	mu      sync.RWMutex
	timeout time.Duration

	maxConnectionsPerServer int
	idleTimeout             time.Duration
}

// NewDirectExecutor creates a new direct mode executor
func NewDirectExecutor(timeout time.Duration) *DirectExecutor {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &DirectExecutor{
		pools:                   make(map[string]*ConnectionPool),
		breaker:                 NewCircuitBreakerManager(),
		timeout:                 timeout,
		maxConnectionsPerServer: 10,
		idleTimeout:             5 * time.Minute,
	}
}

// Execute executes a tool via MCP protocol
func (e *DirectExecutor) Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*execution.ExecutionResult, error) {
	startTime := time.Now()

	// Get connection pool for the server
	pool, err := e.getPool(tool.MCPServer)
	if err != nil {
		return nil, &execution.ExecutionError{
			Code:       "pool_error",
			Message:    fmt.Sprintf("failed to get connection pool: %v", err),
			Retryable:  false,
			StatusCode: 500,
		}
	}

	// Execute with circuit breaker protection
	resultInterface, err := e.breaker.Execute(ctx, tool.MCPServer, func() (interface{}, error) {
		// Acquire connection from pool
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire connection: %w", err)
		}
		defer pool.Release(conn)

		// Create execution context with timeout
		execCtx, cancel := context.WithTimeout(ctx, e.timeout)
		defer cancel()

		// Call the tool via MCP
		conn.totalCalls++
		result, err := conn.client.CallTool(execCtx, tool.Name, args)
		if err != nil {
			conn.errorCount.Add(1)
			return nil, err
		}

		return result, nil
	})

	duration := time.Since(startTime)

	if err != nil {
		log.Error().
			Err(err).
			Str("tool", tool.Name).
			Str("server", tool.MCPServer).
			Dur("duration", duration).
			Msg("Tool execution failed")

		return &execution.ExecutionResult{
			Result:    nil,
			Success:   false,
			Error:     err.Error(),
			Duration:  duration,
			Timestamp: time.Now(),
		}, nil
	}

	log.Info().
		Str("tool", tool.Name).
		Str("server", tool.MCPServer).
		Dur("duration", duration).
		Msg("Tool executed successfully")

	return &execution.ExecutionResult{
		Result:    resultInterface,
		Success:   true,
		Duration:  duration,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"server": tool.MCPServer,
			"mode":   "direct",
		},
	}, nil
}

// getPool returns or creates a connection pool for the given server URL
func (e *DirectExecutor) getPool(serverURL string) (*ConnectionPool, error) {
	// Check if pool already exists
	e.mu.RLock()
	pool, exists := e.pools[serverURL]
	e.mu.RUnlock()

	if exists {
		return pool, nil
	}

	// Create new pool
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := e.pools[serverURL]; exists {
		return pool, nil
	}

	// Create connection pool
	pool = NewConnectionPool(serverURL, e.maxConnectionsPerServer, e.idleTimeout)
	e.pools[serverURL] = pool

	log.Info().
		Str("server", serverURL).
		Int("max_connections", e.maxConnectionsPerServer).
		Msg("Connection pool created for server")

	return pool, nil
}

// Close closes all connection pools
func (e *DirectExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var lastErr error
	for serverURL, pool := range e.pools {
		if err := pool.Close(); err != nil {
			log.Error().
				Err(err).
				Str("server", serverURL).
				Msg("Failed to close connection pool")
			lastErr = err
		}
	}

	e.pools = make(map[string]*ConnectionPool)
	return lastErr
}

// GetMode returns the execution mode
func (e *DirectExecutor) GetMode() execution.ExecutionMode {
	return execution.DirectMode
}

// GetStats returns executor statistics
func (e *DirectExecutor) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	poolStats := make(map[string]interface{})
	for serverURL, pool := range e.pools {
		poolStats[serverURL] = pool.GetMetrics()
	}

	return map[string]interface{}{
		"timeout":          e.timeout.String(),
		"total_pools":      len(e.pools),
		"pools":            poolStats,
		"circuit_breakers": e.breaker.GetMetrics(),
	}
}
