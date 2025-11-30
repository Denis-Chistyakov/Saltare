package mcpclient

// Package mcpclient provides MCP protocol client implementation.
// Handles JSON-RPC communication with MCP servers.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Client represents an MCP protocol client
type Client struct {
	serverURL  string
	httpClient *http.Client
	initialized bool
	requestID   int
}

// New creates a new MCP client
func New(serverURL string) *Client {
	return &Client{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		requestID: 1,
	}
}

// Initialize performs the MCP handshake
func (c *Client) Initialize(ctx context.Context) error {
	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params:  make(map[string]interface{}),
	}

	var resp types.MCPResponse
	if err := c.doRequest(ctx, req, &resp); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	c.initialized = true
	return nil
}

// ListTools returns available tools from the server
func (c *Client) ListTools(ctx context.Context) ([]*types.Tool, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "list_tools",
		Params:  make(map[string]interface{}),
	}

	var resp types.MCPResponse
	if err := c.doRequest(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("list_tools failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list_tools error: %s", resp.Error.Message)
	}

	// Parse tools from result
	return []*types.Tool{}, nil
}

// CallTool executes a tool on the server
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	params := map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	}

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "call_tool",
		Params:  params,
	}

	var resp types.MCPResponse
	if err := c.doRequest(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("call_tool failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("call_tool error: %s", resp.Error.Message)
	}

	return resp.Result, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	c.initialized = false
	return nil
}

// doRequest performs an HTTP request to the MCP server
func (c *Client) doRequest(ctx context.Context, req *types.MCPRequest, resp *types.MCPResponse) error {
	// Marshal request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.serverURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Unmarshal response
	if err := json.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}

// nextRequestID returns the next request ID
func (c *Client) nextRequestID() int {
	id := c.requestID
	c.requestID++
	return id
}

