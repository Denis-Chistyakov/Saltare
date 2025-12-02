package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// HTTPTransport implements Transport interface for HTTP-based MCP servers
type HTTPTransport struct {
	url        string
	httpClient *http.Client
	timeout    time.Duration
	connected  bool
	mu         sync.RWMutex
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(cfg *TransportConfig) (*HTTPTransport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("URL is required for HTTP transport")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPTransport{
		url:     cfg.URL,
		timeout: timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		connected: true, // HTTP is stateless, always "connected"
	}, nil
}

// Send sends a synchronous request via HTTP
func (t *HTTPTransport) Send(ctx context.Context, req *types.MCPRequest) (*types.MCPResponse, error) {
	// Marshal request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Unmarshal response
	var resp types.MCPResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// SendAsync sends an async request (for HTTP, this just wraps Send in a goroutine)
func (t *HTTPTransport) SendAsync(ctx context.Context, req *types.MCPRequest) <-chan *AsyncResult {
	ch := make(chan *AsyncResult, 1)

	go func() {
		start := time.Now()
		resp, err := t.Send(ctx, req)
		ch <- &AsyncResult{
			Response:  resp,
			Error:     err,
			RequestID: req.ID,
			Duration:  time.Since(start),
		}
		close(ch)
	}()

	return ch
}

// Close closes the HTTP transport
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = false
	t.httpClient.CloseIdleConnections()
	return nil
}

// IsConnected returns true if connected
func (t *HTTPTransport) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.connected
}

// Type returns the transport type
func (t *HTTPTransport) Type() TransportType {
	return TransportHTTP
}

// Reconnect for HTTP just resets the connected state
func (t *HTTPTransport) Reconnect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = true
	return nil
}

// URL returns the server URL
func (t *HTTPTransport) URL() string {
	return t.url
}

