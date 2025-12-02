package mcpclient

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStdioTransport_ValidConfig(t *testing.T) {
	// Test with echo command that will exit immediately
	cfg := &TransportConfig{
		Type:            TransportStdio,
		Command:         "echo",
		Args:            []string{"hello"},
		AutoRestart:     false, // Don't auto-restart for test
		MaxRestarts:     0,
		RestartInterval: time.Second,
		Timeout:         5 * time.Second,
	}

	transport, err := NewStdioTransport(cfg)
	require.NoError(t, err)
	assert.NotNil(t, transport)
	assert.Equal(t, TransportStdio, transport.Type())

	// Close the transport
	err = transport.Close()
	assert.NoError(t, err)
}

func TestNewStdioTransport_MissingCommand(t *testing.T) {
	cfg := &TransportConfig{
		Type:    TransportStdio,
		Command: "", // Empty command
	}

	transport, err := NewStdioTransport(cfg)
	assert.Error(t, err)
	assert.Nil(t, transport)
	assert.Contains(t, err.Error(), "command is required")
}

func TestHTTPTransport_Basic(t *testing.T) {
	cfg := &TransportConfig{
		Type:    TransportHTTP,
		URL:     "http://localhost:8080/mcp",
		Timeout: 5 * time.Second,
	}

	transport, err := NewHTTPTransport(cfg)
	require.NoError(t, err)
	assert.NotNil(t, transport)
	assert.Equal(t, TransportHTTP, transport.Type())
	assert.True(t, transport.IsConnected())

	err = transport.Close()
	assert.NoError(t, err)
}

func TestHTTPTransport_MissingURL(t *testing.T) {
	cfg := &TransportConfig{
		Type: TransportHTTP,
		URL:  "", // Empty URL
	}

	transport, err := NewHTTPTransport(cfg)
	assert.Error(t, err)
	assert.Nil(t, transport)
}

func TestNewTransport_HTTP(t *testing.T) {
	cfg := &TransportConfig{
		Type:    TransportHTTP,
		URL:     "http://localhost:8080",
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg)
	require.NoError(t, err)
	assert.NotNil(t, transport)
	assert.Equal(t, TransportHTTP, transport.Type())
	transport.Close()
}

func TestNewTransport_Stdio(t *testing.T) {
	cfg := &TransportConfig{
		Type:        TransportStdio,
		Command:     "echo",
		Args:        []string{"test"},
		AutoRestart: false,
	}

	transport, err := NewTransport(cfg)
	require.NoError(t, err)
	assert.NotNil(t, transport)
	assert.Equal(t, TransportStdio, transport.Type())
	transport.Close()
}

func TestHTTPTransport_Reconnect(t *testing.T) {
	cfg := &TransportConfig{
		Type:    TransportHTTP,
		URL:     "http://localhost:8080",
		Timeout: 5 * time.Second,
	}

	transport, err := NewHTTPTransport(cfg)
	require.NoError(t, err)

	// Close
	transport.Close()
	assert.False(t, transport.IsConnected())

	// Reconnect
	ctx := context.Background()
	err = transport.Reconnect(ctx)
	assert.NoError(t, err)
	assert.True(t, transport.IsConnected())

	transport.Close()
}

func TestDefaultTransportConfig(t *testing.T) {
	cfg := DefaultTransportConfig()
	
	assert.Equal(t, TransportHTTP, cfg.Type)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.True(t, cfg.AutoRestart)
	assert.Equal(t, 3, cfg.MaxRestarts)
	assert.Equal(t, 5*time.Second, cfg.RestartInterval)
}

