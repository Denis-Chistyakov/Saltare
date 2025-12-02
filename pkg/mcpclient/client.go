package mcpclient

// Package mcpclient provides MCP protocol client implementation.
// Supports both HTTP and Stdio transports for maximum compatibility.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Client represents an MCP protocol client
// Supports both HTTP and Stdio transports
type Client struct {
	transport   Transport
	initialized atomic.Bool
	requestID   atomic.Int64
	mu          sync.RWMutex

	// Server capabilities (from initialize response)
	serverInfo   *ServerInfo
	capabilities *ServerCapabilities
}

// ServerInfo contains MCP server information
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities contains MCP server capabilities
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates tool support
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates resource support
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates prompt support
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// New creates a new MCP client with HTTP transport (backward compatible)
func New(serverURL string) *Client {
	transport, err := NewHTTPTransport(&TransportConfig{
		Type:    TransportHTTP,
		URL:     serverURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP transport")
		return nil
	}

	return &Client{
		transport: transport,
	}
}

// NewWithTransport creates a new MCP client with a custom transport
func NewWithTransport(transport Transport) *Client {
	return &Client{
		transport: transport,
	}
}

// NewWithConfig creates a new MCP client from configuration
func NewWithConfig(cfg *TransportConfig) (*Client, error) {
	transport, err := NewTransport(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	return &Client{
		transport: transport,
	}, nil
}

// Initialize performs the MCP handshake
func (c *Client) Initialize(ctx context.Context) error {
	if c.initialized.Load() {
		return nil
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"roots": map[string]interface{}{
					"listChanged": true,
				},
			},
			"clientInfo": map[string]interface{}{
				"name":    "saltare",
				"version": "1.0.0",
			},
		},
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Parse server info and capabilities from result
	if result, ok := resp.Result.(map[string]interface{}); ok {
		// Parse server info
		if serverInfo, ok := result["serverInfo"].(map[string]interface{}); ok {
			c.serverInfo = &ServerInfo{
				Name:    getString(serverInfo, "name"),
				Version: getString(serverInfo, "version"),
			}
		}

		log.Info().
			Str("transport", string(c.transport.Type())).
			Interface("serverInfo", c.serverInfo).
			Msg("MCP client initialized")
	}

	// Send initialized notification
	notifyReq := &types.MCPRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	// Fire and forget for notification
	go func() {
		_, _ = c.transport.Send(context.Background(), notifyReq)
	}()

	c.initialized.Store(true)
	return nil
}

// ListTools returns available tools from the server
func (c *Client) ListTools(ctx context.Context) ([]*types.Tool, error) {
	if !c.initialized.Load() {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/list",
		Params:  make(map[string]interface{}),
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	// Parse tools from result
	tools := []*types.Tool{}

	if result, ok := resp.Result.(map[string]interface{}); ok {
		if toolsList, ok := result["tools"].([]interface{}); ok {
			for _, t := range toolsList {
				if toolMap, ok := t.(map[string]interface{}); ok {
					tool := &types.Tool{
						Name:        getString(toolMap, "name"),
						Description: getString(toolMap, "description"),
					}
					if schema, ok := toolMap["inputSchema"].(map[string]interface{}); ok {
						tool.InputSchema = schema
					}
					tools = append(tools, tool)
				}
			}
		}
	}

	return tools, nil
}

// CallTool executes a tool on the server
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	if !c.initialized.Load() {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tools/call failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call error: %s", resp.Error.Message)
	}

	return resp.Result, nil
}

// CallToolAsync executes a tool asynchronously
func (c *Client) CallToolAsync(ctx context.Context, toolName string, args map[string]interface{}) <-chan *AsyncResult {
	resultCh := make(chan *AsyncResult, 1)

	if !c.initialized.Load() {
		if err := c.Initialize(ctx); err != nil {
			resultCh <- &AsyncResult{Error: err}
			close(resultCh)
			return resultCh
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/call",
		Params: map[string]interface{}{
		"name":      toolName,
		"arguments": args,
		},
	}

	// Use transport's async method
	transportCh := c.transport.SendAsync(ctx, req)

	go func() {
		result := <-transportCh
		if result.Error != nil {
			resultCh <- result
		} else if result.Response.Error != nil {
			result.Error = fmt.Errorf("tools/call error: %s", result.Response.Error.Message)
			resultCh <- result
		} else {
			result.Response.Result = result.Response.Result
			resultCh <- result
		}
		close(resultCh)
	}()

	return resultCh
}

// ListResources returns available resources from the server
func (c *Client) ListResources(ctx context.Context) ([]map[string]interface{}, error) {
	if !c.initialized.Load() {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "resources/list",
		Params:  make(map[string]interface{}),
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("resources/list failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("resources/list error: %s", resp.Error.Message)
	}

	// Parse resources from result
	resources := []map[string]interface{}{}

	if result, ok := resp.Result.(map[string]interface{}); ok {
		if resourcesList, ok := result["resources"].([]interface{}); ok {
			for _, r := range resourcesList {
				if resourceMap, ok := r.(map[string]interface{}); ok {
					resources = append(resources, resourceMap)
				}
			}
		}
	}

	return resources, nil
}

// ReadResource reads a resource by URI
func (c *Client) ReadResource(ctx context.Context, uri string) (interface{}, error) {
	if !c.initialized.Load() {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "resources/read",
		Params: map[string]interface{}{
			"uri": uri,
		},
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("resources/read failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("resources/read error: %s", resp.Error.Message)
	}

	return resp.Result, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	c.initialized.Store(false)
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

// IsConnected returns true if the transport is connected
func (c *Client) IsConnected() bool {
	return c.transport != nil && c.transport.IsConnected()
	}

// Transport returns the underlying transport
func (c *Client) Transport() Transport {
	return c.transport
}

// ServerInfo returns the server info from initialization
func (c *Client) GetServerInfo() *ServerInfo {
	return c.serverInfo
}

// nextRequestID returns the next request ID
func (c *Client) nextRequestID() int {
	return int(c.requestID.Add(1))
}

// getString safely extracts a string from a map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
