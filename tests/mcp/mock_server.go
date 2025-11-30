package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/rs/zerolog/log"
)

// MockMCPServer is a mock MCP server for testing
type MockMCPServer struct {
	server      *httptest.Server
	mu          sync.RWMutex
	tools       map[string]*MockTool
	callHistory []CallRecord
	failureMode bool // Simulate failures
	latency     int  // Artificial latency in ms
}

// MockTool represents a mock tool
type MockTool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(args map[string]interface{}) (interface{}, error)
}

// CallRecord tracks tool calls for testing
type CallRecord struct {
	ToolName  string
	Arguments map[string]interface{}
	Result    interface{}
	Error     error
}

// NewMockMCPServer creates a new mock MCP server
func NewMockMCPServer() *MockMCPServer {
	mock := &MockMCPServer{
		tools:       make(map[string]*MockTool),
		callHistory: []CallRecord{},
	}

	// Create HTTP test server
	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))

	log.Info().Str("url", mock.server.URL).Msg("Mock MCP server started")

	return mock
}

// RegisterTool registers a mock tool
func (m *MockMCPServer) RegisterTool(tool *MockTool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools[tool.Name] = tool
	log.Debug().Str("tool", tool.Name).Msg("Mock tool registered")
}

// SetFailureMode enables/disables failure simulation
func (m *MockMCPServer) SetFailureMode(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureMode = enabled
}

// GetCallHistory returns the call history
func (m *MockMCPServer) GetCallHistory() []CallRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]CallRecord{}, m.callHistory...)
}

// ClearHistory clears the call history
func (m *MockMCPServer) ClearHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callHistory = []CallRecord{}
}

// URL returns the server URL
func (m *MockMCPServer) URL() string {
	return m.server.URL
}

// Close closes the mock server
func (m *MockMCPServer) Close() {
	m.server.Close()
	log.Info().Msg("Mock MCP server stopped")
}

// handleRequest handles MCP JSON-RPC requests
func (m *MockMCPServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	method, ok := request["method"].(string)
	if !ok {
		http.Error(w, "Missing method", http.StatusBadRequest)
		return
	}

	switch method {
	case "initialize":
		m.handleInitialize(w, request)
	case "tools/list", "list_tools": // Support both formats
		m.handleListTools(w, request)
	case "tools/call", "call_tool": // Support both formats
		m.handleCallTool(w, request)
	default:
		m.sendErrorResponse(w, request, -32601, "Method not found")
	}
}

// handleInitialize handles initialize request
func (m *MockMCPServer) handleInitialize(w http.ResponseWriter, request map[string]interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mock-mcp-server",
				"version": "1.0.0",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListTools handles tools/list request
func (m *MockMCPServer) handleListTools(w http.ResponseWriter, request map[string]interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := []map[string]interface{}{}
	for _, tool := range m.tools {
		tools = append(tools, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result": map[string]interface{}{
			"tools": tools,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCallTool handles tools/call request
func (m *MockMCPServer) handleCallTool(w http.ResponseWriter, request map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate failure mode
	if m.failureMode {
		m.sendErrorResponse(w, request, -32603, "Internal server error (simulated)")
		return
	}

	params, ok := request["params"].(map[string]interface{})
	if !ok {
		m.sendErrorResponse(w, request, -32602, "Invalid params")
		return
	}

	toolName, ok := params["name"].(string)
	if !ok {
		m.sendErrorResponse(w, request, -32602, "Missing tool name")
		return
	}

	tool, exists := m.tools[toolName]
	if !exists {
		m.sendErrorResponse(w, request, -32602, fmt.Sprintf("Tool not found: %s", toolName))
		return
	}

	args, _ := params["arguments"].(map[string]interface{})

	// Call the tool handler
	result, err := tool.Handler(args)

	// Record the call
	m.callHistory = append(m.callHistory, CallRecord{
		ToolName:  toolName,
		Arguments: args,
		Result:    result,
		Error:     err,
	})

	if err != nil {
		m.sendErrorResponse(w, request, -32603, err.Error())
		return
	}

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result": map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%v", result),
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// sendErrorResponse sends a JSON-RPC error response
func (m *MockMCPServer) sendErrorResponse(w http.ResponseWriter, request map[string]interface{}, code int, message string) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return 200
	json.NewEncoder(w).Encode(response)
}

// CreateWeatherTool creates a mock weather tool
func CreateWeatherTool() *MockTool {
	return &MockTool{
		Name:        "get_current_weather",
		Description: "Get current weather for a city",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"city"},
		},
		Handler: func(args map[string]interface{}) (interface{}, error) {
			city, ok := args["city"].(string)
			if !ok {
				return nil, fmt.Errorf("city is required")
			}

			return map[string]interface{}{
				"city":        city,
				"temperature": 22,
				"condition":   "sunny",
				"humidity":    65,
			}, nil
		},
	}
}

// CreateCalculatorTool creates a mock calculator tool
func CreateCalculatorTool() *MockTool {
	return &MockTool{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"a", "b"},
		},
		Handler: func(args map[string]interface{}) (interface{}, error) {
			a, aOk := args["a"].(float64)
			b, bOk := args["b"].(float64)
			if !aOk || !bOk {
				return nil, fmt.Errorf("invalid arguments")
			}
			return a + b, nil
		},
	}
}

// CreateFailingTool creates a tool that always fails
func CreateFailingTool() *MockTool {
	return &MockTool{
		Name:        "failing_tool",
		Description: "A tool that always fails",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(args map[string]interface{}) (interface{}, error) {
			return nil, fmt.Errorf("simulated failure")
		},
	}
}

