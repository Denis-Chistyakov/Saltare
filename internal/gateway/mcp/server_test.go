package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockExecutor implements execution.Executor for testing
type MockExecutor struct {
	result *execution.ExecutionResult
	err    error
}

func (m *MockExecutor) Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*execution.ExecutionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &execution.ExecutionResult{
		Success:  true,
		Result:   map[string]interface{}{"response": "mock result", "args": args},
		Duration: 10 * time.Millisecond,
	}, nil
}

func (m *MockExecutor) Close() error {
	return nil
}

func (m *MockExecutor) GetMode() execution.ExecutionMode {
	return execution.DirectMode
}

func setupTestServer(t *testing.T) *Server {
	// Create manager
	manager := toolkit.NewManager()

	// Register test toolkit with weather tool
	testToolkit := &types.Toolkit{
		ID:   "test-toolkit",
		Name: "Test Toolkit",
		Toolboxes: []*types.Toolbox{
			{
				ID:   "weather-toolbox",
				Name: "weather",
				Tags: []string{"weather", "api"},
				Tools: []*types.Tool{
					{
						ID:          "weather-get-current",
						Name:        "get_current",
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
						MCPServer: "http://localhost:8082",
					},
				},
			},
		},
	}
	err := manager.RegisterToolkit(testToolkit)
	require.NoError(t, err)

	// Create mock executor
	mockExec := &MockExecutor{
		result: &execution.ExecutionResult{
			Success: true,
			Result: map[string]interface{}{
				"city":        "Moscow",
				"temperature": 5,
				"condition":   "cloudy",
			},
			Duration: 50 * time.Millisecond,
		},
	}

	// Create executor registry with mock
	registry := execution.NewExecutorRegistry()
	registry.Register(execution.DirectMode, mockExec)

	// Create router (without LLM for testing)
	router := semantic.NewRouter(manager)

	// Create server
	server := NewServer(manager, registry, router)

	return server
}

func TestMCPServer_Initialize(t *testing.T) {
	server := setupTestServer(t)

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]interface{}{},
	}

	resp := server.HandleRequest(req)

	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	result, ok := resp.Result.(types.MCPInitializeResult)
	assert.True(t, ok)
	assert.Equal(t, "2024-11-05", result.ProtocolVersion)
	assert.Equal(t, "Saltare", result.ServerInfo.Name)
}

func TestMCPServer_ListTools(t *testing.T) {
	server := setupTestServer(t)

	// Initialize first
	server.HandleRequest(&types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "list_tools",
		Params:  map[string]interface{}{},
	}

	resp := server.HandleRequest(req)

	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	result, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)

	tools, ok := result["tools"].([]types.MCPToolInfo)
	assert.True(t, ok)
	assert.Len(t, tools, 1)
	assert.Equal(t, "get_current", tools[0].Name)
}

func TestMCPServer_CallTool_Direct(t *testing.T) {
	server := setupTestServer(t)

	// Initialize first
	server.HandleRequest(&types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})

	// Direct call with name
	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "call_tool",
		Params: map[string]interface{}{
			"name": "weather.get_current",
			"arguments": map[string]interface{}{
				"city": "Moscow",
			},
		},
	}

	resp := server.HandleRequest(req)

	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	result, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	assert.False(t, result["isError"].(bool))
	assert.Equal(t, "weather.get_current", result["tool_used"])
}

func TestMCPServer_CallTool_MissingParams(t *testing.T) {
	server := setupTestServer(t)

	// Initialize
	server.HandleRequest(&types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})

	// Call without name or query
	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "call_tool",
		Params:  map[string]interface{}{},
	}

	resp := server.HandleRequest(req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, types.MCPErrorInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "name or query")
}

func TestMCPServer_CallTool_ToolNotFound(t *testing.T) {
	server := setupTestServer(t)

	// Initialize
	server.HandleRequest(&types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})

	// Call non-existent tool
	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "call_tool",
		Params: map[string]interface{}{
			"name": "nonexistent.tool",
		},
	}

	resp := server.HandleRequest(req)

	assert.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "tool not found")
}

func TestMCPServer_CallTool_NotInitialized(t *testing.T) {
	server := setupTestServer(t)

	// Call without initialize
	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "call_tool",
		Params: map[string]interface{}{
			"name": "weather.get_current",
		},
	}

	resp := server.HandleRequest(req)

	assert.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not initialized")
}

func TestMCPServer_UnknownMethod(t *testing.T) {
	server := setupTestServer(t)

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown_method",
	}

	resp := server.HandleRequest(req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, types.MCPErrorMethodNotFound, resp.Error.Code)
}

func TestMCPServer_ParseRequest(t *testing.T) {
	server := setupTestServer(t)

	// Valid request
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req, err := server.ParseRequest(data)
	assert.NoError(t, err)
	assert.Equal(t, "initialize", req.Method)

	// Invalid JSON
	_, err = server.ParseRequest([]byte(`invalid`))
	assert.Error(t, err)

	// Wrong version
	_, err = server.ParseRequest([]byte(`{"jsonrpc":"1.0","id":1,"method":"test"}`))
	assert.Error(t, err)
}

func TestMCPServer_FormatResponse(t *testing.T) {
	server := setupTestServer(t)

	resp := &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result:  map[string]interface{}{"test": "value"},
	}

	data, err := server.FormatResponse(resp)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "test")
	assert.Contains(t, string(data), "value")
}

func TestMCPServer_ListResources(t *testing.T) {
	server := setupTestServer(t)

	// Initialize
	server.HandleRequest(&types.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})

	req := &types.MCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "list_resources",
	}

	resp := server.HandleRequest(req)

	assert.Nil(t, resp.Error)
	result := resp.Result.(map[string]interface{})
	resources := result["resources"].([]interface{})
	assert.Empty(t, resources)
}

