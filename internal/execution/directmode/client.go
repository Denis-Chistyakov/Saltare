package directmode

// Package directmode implements Direct Mode execution - calling MCP servers directly via HTTP or Stdio.
// Includes connection pooling, circuit breakers, and retry logic for reliable tool execution.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/pkg/mcpclient"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/rs/zerolog/log"
)

// DirectExecutor executes tools via MCP protocol (HTTP or Stdio)
type DirectExecutor struct {
	pools   map[string]*ConnectionPool
	breaker *CircuitBreakerManager
	mu      sync.RWMutex
	timeout time.Duration

	maxConnectionsPerServer int
	idleTimeout             time.Duration

	// Stdio process configs (command -> config)
	stdioConfigs map[string]*mcpclient.TransportConfig
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
		stdioConfigs:            make(map[string]*mcpclient.TransportConfig),
	}
}

// RegisterStdioServer registers a stdio-based MCP server configuration
func (e *DirectExecutor) RegisterStdioServer(name string, cfg *mcpclient.TransportConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cfg.Type = mcpclient.TransportStdio
	e.stdioConfigs[name] = cfg

	log.Info().
		Str("name", name).
		Str("command", cfg.Command).
		Strs("args", cfg.Args).
		Msg("Registered stdio MCP server")
}

// Execute executes a tool via MCP protocol
func (e *DirectExecutor) Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*execution.ExecutionResult, error) {
	startTime := time.Now()

	// Determine transport type from tool configuration
	transportConfig := e.getTransportConfig(tool)

	// Generate pool ID (use command for stdio, URL for HTTP)
	poolID := tool.MCPServer
	if transportConfig.Type == mcpclient.TransportStdio {
		poolID = fmt.Sprintf("stdio:%s", transportConfig.Command)
	}

	// Get connection pool for the server
	pool, err := e.getPool(poolID, transportConfig)
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
			Str("transport", string(transportConfig.Type)).
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
		Str("transport", string(transportConfig.Type)).
		Dur("duration", duration).
		Msg("Tool executed successfully")

	return &execution.ExecutionResult{
		Result:    resultInterface,
		Success:   true,
		Duration:  duration,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"server":    tool.MCPServer,
			"mode":      "direct",
			"transport": string(transportConfig.Type),
		},
	}, nil
}

// getTransportConfig determines the transport configuration based on Tool config
func (e *DirectExecutor) getTransportConfig(tool *types.Tool) *mcpclient.TransportConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if tool has explicit transport configuration
	if tool.Transport == "stdio" && tool.StdioConfig != nil {
		return &mcpclient.TransportConfig{
			Type:            mcpclient.TransportStdio,
			Command:         tool.StdioConfig.Command,
			Args:            tool.StdioConfig.Args,
			Env:             tool.StdioConfig.Env,
			WorkDir:         tool.StdioConfig.WorkDir,
			AutoRestart:     true,
			MaxRestarts:     3,
			RestartInterval: 5 * time.Second,
			Timeout:         e.timeout,
		}
	}

	// Check if it's a registered stdio server (by name)
	if cfg, ok := e.stdioConfigs[tool.MCPServer]; ok {
		return cfg
	}

	// Check if MCPServer looks like a stdio command (starts with "stdio:" prefix)
	if strings.HasPrefix(tool.MCPServer, "stdio:") {
		parts := strings.SplitN(tool.MCPServer[6:], " ", 2)
		cfg := &mcpclient.TransportConfig{
			Type:            mcpclient.TransportStdio,
			Command:         parts[0],
			AutoRestart:     true,
			MaxRestarts:     3,
			RestartInterval: 5 * time.Second,
			Timeout:         30 * time.Second,
		}
		if len(parts) > 1 {
			cfg.Args = strings.Fields(parts[1])
		}
		return cfg
	}

	// Default to HTTP transport
	return &mcpclient.TransportConfig{
		Type:    mcpclient.TransportHTTP,
		URL:     tool.MCPServer,
		Timeout: e.timeout,
	}
}

// getPool returns or creates a connection pool for the given server
func (e *DirectExecutor) getPool(serverID string, cfg *mcpclient.TransportConfig) (*ConnectionPool, error) {
	// Check if pool already exists
	e.mu.RLock()
	pool, exists := e.pools[serverID]
	e.mu.RUnlock()

	if exists {
		return pool, nil
	}

	// Create new pool
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := e.pools[serverID]; exists {
		return pool, nil
	}

	// Create connection pool with transport config
	pool = NewConnectionPoolWithConfig(cfg, e.maxConnectionsPerServer, e.idleTimeout)
	e.pools[serverID] = pool

	log.Info().
		Str("server", serverID).
		Str("transport", string(cfg.Type)).
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

	stdioServers := make([]string, 0, len(e.stdioConfigs))
	for name := range e.stdioConfigs {
		stdioServers = append(stdioServers, name)
	}

	return map[string]interface{}{
		"timeout":          e.timeout.String(),
		"total_pools":      len(e.pools),
		"pools":            poolStats,
		"circuit_breakers": e.breaker.GetMetrics(),
		"stdio_servers":    stdioServers,
	}
}
