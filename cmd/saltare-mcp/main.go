package main

// Saltare MCP Proxy - Stdio MCP server that proxies to multiple backends
// Use this as your single MCP server in Cursor/Claude Desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/mcpclient"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// MCPBackend represents a connected MCP server
type MCPBackend struct {
	Name      string
	Client    *mcpclient.Client
	Transport mcpclient.TransportType
	Tools     []*types.Tool
}

// ProxyServer is the main MCP proxy server
type ProxyServer struct {
	backends    map[string]*MCPBackend
	toolIndex   map[string]*MCPBackend // tool name -> backend
	mu          sync.RWMutex
	initialized bool
	requestID   int
}

// MCPRequest represents incoming JSON-RPC request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// MCPResponse represents outgoing JSON-RPC response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func main() {
	// Setup logging to stderr (stdout is for MCP protocol)
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Msg("Starting Saltare MCP Proxy")

	// Create proxy server
	proxy := &ProxyServer{
		backends:  make(map[string]*MCPBackend),
		toolIndex: make(map[string]*MCPBackend),
	}

	// Connect to backends from environment or config
	if err := proxy.connectBackends(); err != nil {
		log.Error().Err(err).Msg("Failed to connect backends")
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info().Msg("Shutting down...")
		proxy.close()
		os.Exit(0)
	}()

	// Run stdio server
	proxy.runStdio()
}

func (p *ProxyServer) connectBackends() error {
	// Get backends from environment
	backendsEnv := os.Getenv("SALTARE_BACKENDS")
	if backendsEnv == "" {
		log.Warn().Msg("No backends configured. Set SALTARE_BACKENDS env var.")
		return nil
	}

	lines := strings.Split(backendsEnv, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		transportType := strings.TrimSpace(parts[1])

		var cfg *mcpclient.TransportConfig

		if transportType == "http" {
			cfg = &mcpclient.TransportConfig{
				Type:    mcpclient.TransportHTTP,
				URL:     strings.TrimSpace(parts[2]),
				Timeout: 30 * time.Second,
			}
		} else if transportType == "stdio" {
			command := strings.TrimSpace(parts[2])
			var args []string
			if len(parts) > 3 {
				for _, arg := range parts[3:] {
					args = append(args, strings.TrimSpace(arg))
				}
			}
			cfg = &mcpclient.TransportConfig{
				Type:            mcpclient.TransportStdio,
				Command:         command,
				Args:            args,
				AutoRestart:     true,
				MaxRestarts:     3,
				RestartInterval: 5 * time.Second,
				Timeout:         30 * time.Second,
			}
		} else {
			log.Warn().Str("name", name).Str("type", transportType).Msg("Unknown transport type")
			continue
		}

		if err := p.addBackend(name, cfg); err != nil {
			log.Error().Err(err).Str("name", name).Msg("Failed to add backend")
		}
	}

	return nil
}

func (p *ProxyServer) addBackend(name string, cfg *mcpclient.TransportConfig) error {
	log.Info().Str("name", name).Str("transport", string(cfg.Type)).Msg("Connecting to backend")

	client, err := mcpclient.NewWithConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// Get tools from backend
	tools, err := client.ListTools(ctx)
	if err != nil {
		log.Warn().Err(err).Str("name", name).Msg("Failed to list tools")
		tools = []*types.Tool{}
	}

	backend := &MCPBackend{
		Name:      name,
		Client:    client,
		Transport: cfg.Type,
		Tools:     tools,
	}

	p.mu.Lock()
	p.backends[name] = backend
	// Index tools
	for _, tool := range tools {
		toolName := fmt.Sprintf("%s_%s", name, tool.Name)
		p.toolIndex[toolName] = backend
		p.toolIndex[tool.Name] = backend // Also index by short name
	}
	p.mu.Unlock()

	log.Info().
		Str("name", name).
		Int("tools", len(tools)).
		Msg("Backend connected")

	return nil
}

func (p *ProxyServer) runStdio() {
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal(line, &req); err != nil {
			p.writeError(nil, -32700, "Parse error")
			continue
		}

		resp := p.handleRequest(&req)
		p.writeResponse(resp)
	}
}

func (p *ProxyServer) handleRequest(req *MCPRequest) *MCPResponse {
	switch req.Method {
	case "initialize":
		return p.handleInitialize(req)
	case "notifications/initialized":
		return nil // No response for notifications
	case "tools/list":
		return p.handleListTools(req)
	case "tools/call":
		return p.handleCallTool(req)
	case "resources/list":
		return p.handleListResources(req)
	default:
		return p.errorResponse(req.ID, -32601, "Method not found")
	}
}

func (p *ProxyServer) handleInitialize(req *MCPRequest) *MCPResponse {
	p.initialized = true

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "saltare-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true,
				},
				"resources": map[string]interface{}{
					"listChanged": true,
				},
			},
		},
	}
}

func (p *ProxyServer) handleListTools(req *MCPRequest) *MCPResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var tools []map[string]interface{}
	for name, backend := range p.backends {
		for _, tool := range backend.Tools {
			toolEntry := map[string]interface{}{
				"name":        fmt.Sprintf("%s_%s", name, tool.Name),
				"description": fmt.Sprintf("[%s] %s", name, tool.Description),
			}
			if tool.InputSchema != nil {
				toolEntry["inputSchema"] = tool.InputSchema
			}
			tools = append(tools, toolEntry)
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (p *ProxyServer) handleCallTool(req *MCPRequest) *MCPResponse {
	params := req.Params
	toolName, _ := params["name"].(string)
	args, _ := params["arguments"].(map[string]interface{})

	if toolName == "" {
		return p.errorResponse(req.ID, -32602, "Tool name required")
	}

	// Find backend for tool
	p.mu.RLock()
	backend, ok := p.toolIndex[toolName]
	p.mu.RUnlock()

	if !ok {
		return p.errorResponse(req.ID, -32602, fmt.Sprintf("Tool not found: %s", toolName))
	}

	// Extract actual tool name (remove backend prefix if present)
	actualToolName := toolName
	prefix := backend.Name + "_"
	if strings.HasPrefix(toolName, prefix) {
		actualToolName = strings.TrimPrefix(toolName, prefix)
	}

	// Call tool on backend
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := backend.Client.CallTool(ctx, actualToolName, args)
	if err != nil {
		return p.errorResponse(req.ID, -32603, err.Error())
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (p *ProxyServer) handleListResources(req *MCPRequest) *MCPResponse {
	// Aggregate resources from all backends
	var resources []map[string]interface{}

	p.mu.RLock()
	for name, backend := range p.backends {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		backendResources, err := backend.Client.ListResources(ctx)
		cancel()

		if err != nil {
			log.Warn().Err(err).Str("backend", name).Msg("Failed to list resources")
			continue
		}

		for _, res := range backendResources {
			// Prefix resource URI with backend name
			if uri, ok := res["uri"].(string); ok {
				res["uri"] = fmt.Sprintf("%s://%s", name, uri)
			}
			resources = append(resources, res)
		}
	}
	p.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"resources": resources,
		},
	}
}

func (p *ProxyServer) errorResponse(id interface{}, code int, message string) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}
}

func (p *ProxyServer) writeResponse(resp *MCPResponse) {
	if resp == nil {
		return
	}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

func (p *ProxyServer) writeError(id interface{}, code int, message string) {
	p.writeResponse(p.errorResponse(id, code, message))
}

func (p *ProxyServer) close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, backend := range p.backends {
		log.Info().Str("name", name).Msg("Closing backend")
		backend.Client.Close()
	}
}

