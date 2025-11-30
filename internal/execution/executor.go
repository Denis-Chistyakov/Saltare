package execution

// Package execution provides a unified executor interface and registry.
// Supports multiple execution modes with a pluggable executor pattern.

import (
	"context"
	"fmt"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// ExecutionMode represents the execution mode
type ExecutionMode string

const (
	// DirectMode executes tools via MCP protocol
	DirectMode ExecutionMode = "direct"
	// CodeMode executes code in sandbox
	CodeMode ExecutionMode = "code"
)

// Executor interface for executing tools
type Executor interface {
	// Execute executes a tool with given arguments
	Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*ExecutionResult, error)

	// Close closes the executor and cleans up resources
	Close() error

	// GetMode returns the execution mode
	GetMode() ExecutionMode
}

// ExecutionResult represents the result of tool execution
type ExecutionResult struct {
	Result     interface{}            `json:"result"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	Duration   time.Duration          `json:"duration"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// ExecutionError represents an execution error
type ExecutionError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Retryable  bool                   `json:"retryable"`
	StatusCode int                    `json:"status_code"`
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ExecutorRegistry manages multiple executors
type ExecutorRegistry struct {
	executors map[ExecutionMode]Executor
	default_  ExecutionMode
}

// NewExecutorRegistry creates a new executor registry
func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{
		executors: make(map[ExecutionMode]Executor),
		default_:  DirectMode,
	}
}

// Register registers an executor
func (r *ExecutorRegistry) Register(mode ExecutionMode, executor Executor) {
	r.executors[mode] = executor
}

// Get returns an executor for the given mode
func (r *ExecutorRegistry) Get(mode ExecutionMode) (Executor, error) {
	executor, ok := r.executors[mode]
	if !ok {
		return nil, fmt.Errorf("executor not found for mode: %s", mode)
	}
	return executor, nil
}

// GetDefault returns the default executor
func (r *ExecutorRegistry) GetDefault() (Executor, error) {
	return r.Get(r.default_)
}

// Execute executes a tool using the specified mode
func (r *ExecutorRegistry) Execute(ctx context.Context, mode ExecutionMode, tool *types.Tool, args map[string]interface{}) (*ExecutionResult, error) {
	executor, err := r.Get(mode)
	if err != nil {
		// Fallback to default executor
		executor, err = r.GetDefault()
		if err != nil {
			return nil, fmt.Errorf("no executor available: %w", err)
		}
	}

	return executor.Execute(ctx, tool, args)
}

// CloseAll closes all registered executors
func (r *ExecutorRegistry) CloseAll() error {
	var lastErr error
	for mode, executor := range r.executors {
		if err := executor.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close executor %s: %w", mode, err)
		}
	}
	return lastErr
}

