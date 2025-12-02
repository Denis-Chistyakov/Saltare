package mcpclient

// Package mcpclient provides MCP protocol client implementation.
// Supports both HTTP and Stdio transports for maximum compatibility.

import (
	"context"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// TransportType identifies the transport mechanism
type TransportType string

const (
	TransportHTTP  TransportType = "http"
	TransportStdio TransportType = "stdio"
)

// Transport defines the interface for MCP communication
// Both HTTP and Stdio transports implement this interface
type Transport interface {
	// Send sends a request and waits for response (synchronous)
	Send(ctx context.Context, req *types.MCPRequest) (*types.MCPResponse, error)

	// SendAsync sends a request and returns immediately with a channel for the response
	// Useful for stdio transport where we can have multiple in-flight requests
	SendAsync(ctx context.Context, req *types.MCPRequest) <-chan *AsyncResult

	// Close closes the transport and releases resources
	Close() error

	// IsConnected returns true if the transport is ready to send requests
	IsConnected() bool

	// Type returns the transport type
	Type() TransportType

	// Reconnect attempts to reconnect/restart the transport
	Reconnect(ctx context.Context) error
}

// AsyncResult wraps the response or error from an async request
type AsyncResult struct {
	Response  *types.MCPResponse
	Error     error
	RequestID interface{}
	Duration  time.Duration
}

// TransportConfig holds configuration for creating transports
type TransportConfig struct {
	// Common
	Type    TransportType
	Timeout time.Duration

	// HTTP specific
	URL string

	// Stdio specific
	Command string
	Args    []string
	Env     map[string]string
	WorkDir string

	// Process management
	AutoRestart     bool
	MaxRestarts     int
	RestartInterval time.Duration
}

// DefaultTransportConfig returns sensible defaults
func DefaultTransportConfig() *TransportConfig {
	return &TransportConfig{
		Type:            TransportHTTP,
		Timeout:         30 * time.Second,
		AutoRestart:     true,
		MaxRestarts:     3,
		RestartInterval: 5 * time.Second,
	}
}

// NewTransport creates a transport based on configuration
func NewTransport(cfg *TransportConfig) (Transport, error) {
	switch cfg.Type {
	case TransportStdio:
		return NewStdioTransport(cfg)
	case TransportHTTP:
		fallthrough
	default:
		return NewHTTPTransport(cfg)
	}
}

