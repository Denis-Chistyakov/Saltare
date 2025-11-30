package codemode

import (
	"context"
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// MockDirectExecutor for testing
type MockDirectExecutor struct{}

func (m *MockDirectExecutor) Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*execution.ExecutionResult, error) {
	// Mock weather tool
	if tool.Name == "get_current" {
		city, _ := args["city"].(string)
		return &execution.ExecutionResult{
			Success: true,
			Result: map[string]interface{}{
				"city":        city,
				"temperature": 20.5,
				"condition":   "Sunny",
			},
		}, nil
	}

	// Mock GitHub tool
	if tool.Name == "list_issues" {
		return &execution.ExecutionResult{
			Success: true,
			Result: []map[string]interface{}{
				{
					"title":  "Bug: crash on startup",
					"labels": []string{"critical", "bug"},
					"state":  "open",
				},
				{
					"title":  "Feature: add dark mode",
					"labels": []string{"enhancement"},
					"state":  "open",
				},
			},
		}, nil
	}

	return &execution.ExecutionResult{Success: false, Error: "unknown tool"}, nil
}

func (m *MockDirectExecutor) Close() error {
	return nil
}

func (m *MockDirectExecutor) GetMode() execution.ExecutionMode {
	return execution.DirectMode
}

// TestSandbox_SimpleExecution tests basic code execution
func TestSandbox_SimpleExecution(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	code := `
		const x = 10;
		const y = 20;
		x + y;
	`

	result, err := sandbox.Execute(context.Background(), code, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got failure: %s", result.Error)
	}

	if result.Result != int64(30) {
		t.Errorf("Expected 30, got %v", result.Result)
	}
}

// TestSandbox_ToolInjection tests tool injection and calling
func TestSandbox_ToolInjection(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	tools := []*types.Tool{
		{
			ID:          "weather-get_current",
			Name:        "get_current",
			Description: "Get current weather",
			InputSchema: map[string]interface{}{},
			MCPServer:   "http://localhost:8082/mcp",
		},
	}

	code := `
		const result = get_current({city: "London"});
		result.temperature;
	`

	result, err := sandbox.Execute(context.Background(), code, tools)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got failure: %s", result.Error)
	}

	if result.Result != 20.5 {
		t.Errorf("Expected 20.5, got %v", result.Result)
	}
}

// TestSandbox_Timeout tests timeout handling
func TestSandbox_Timeout(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 100*time.Millisecond, mock) // Very short timeout
	defer sandbox.Close()

	code := `
		let sum = 0;
		while (true) {
			sum++;
		}
	`

	result, err := sandbox.Execute(context.Background(), code, nil)
	if err == nil {
		t.Fatal("Expected timeout error")
	}

	if result.Success {
		t.Error("Expected failure due to timeout")
	}
}

// TestSandbox_SecurityDisabledEval tests that eval is disabled
func TestSandbox_SecurityDisabledEval(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	code := `
		typeof eval;
	`

	result, err := sandbox.Execute(context.Background(), code, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.Result != "undefined" {
		t.Errorf("Expected 'undefined', got %v (eval should be disabled)", result.Result)
	}
}

// TestSandbox_ComplexToolChain tests chaining multiple tool calls
func TestSandbox_ComplexToolChain(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	tools := []*types.Tool{
		{
			ID:          "github-list_issues",
			Name:        "list_issues",
			Description: "List GitHub issues",
			InputSchema: map[string]interface{}{},
			MCPServer:   "http://localhost:8082/mcp",
		},
	}

	code := `
		const issues = list_issues({});
		const critical = issues.filter(i => i.labels.includes("critical"));
		critical.length;
	`

	result, err := sandbox.Execute(context.Background(), code, tools)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got failure: %s", result.Error)
	}

	if result.Result != int64(1) {
		t.Errorf("Expected 1 critical issue, got %v", result.Result)
	}
}

// TestSandbox_ErrorHandling tests error propagation
func TestSandbox_ErrorHandling(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	code := `
		throw new Error("Test error");
	`

	result, err := sandbox.Execute(context.Background(), code, nil)
	if err == nil {
		t.Fatal("Expected error")
	}

	if result.Success {
		t.Error("Expected failure")
	}
}

// TestSandbox_ConsoleLog tests console.log functionality
func TestSandbox_ConsoleLog(t *testing.T) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(5, 30*time.Second, mock)
	defer sandbox.Close()

	code := `
		console.log("Hello from sandbox");
		42;
	`

	result, err := sandbox.Execute(context.Background(), code, nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got failure: %s", result.Error)
	}

	if result.Result != int64(42) {
		t.Errorf("Expected 42, got %v", result.Result)
	}
}

// BenchmarkSandbox_SimpleExecution benchmarks simple code execution
func BenchmarkSandbox_SimpleExecution(b *testing.B) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(10, 30*time.Second, mock)
	defer sandbox.Close()

	code := `
		const x = 10;
		const y = 20;
		x + y;
	`

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sandbox.Execute(ctx, code, nil)
	}
}

// BenchmarkSandbox_ToolCall benchmarks tool calling
func BenchmarkSandbox_ToolCall(b *testing.B) {
	mock := &MockDirectExecutor{}
	sandbox := NewSandbox(10, 30*time.Second, mock)
	defer sandbox.Close()

	tools := []*types.Tool{
		{
			ID:          "weather-get_current",
			Name:        "get_current",
			Description: "Get current weather",
			InputSchema: map[string]interface{}{},
			MCPServer:   "http://localhost:8082/mcp",
		},
	}

	code := `get_current({city: "Tokyo"});`

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sandbox.Execute(ctx, code, tools)
	}
}

