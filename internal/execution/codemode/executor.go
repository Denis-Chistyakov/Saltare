package codemode

import (
	"context"
	"fmt"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// CodeModeExecutor adapts Sandbox to Executor interface
type CodeModeExecutor struct {
	sandbox *Sandbox
}

// NewCodeModeExecutor creates a new Code Mode executor
func NewCodeModeExecutor(sandbox *Sandbox) *CodeModeExecutor {
	return &CodeModeExecutor{
		sandbox: sandbox,
	}
}

// Execute implements the Executor interface
// In Code Mode, the "tool" parameter contains code in its Description field
// or we expect the args to contain a "code" field
func (e *CodeModeExecutor) Execute(ctx context.Context, tool *types.Tool, args map[string]interface{}) (*execution.ExecutionResult, error) {
	// Extract code from args
	code, ok := args["code"].(string)
	if !ok {
		return nil, fmt.Errorf("code mode requires 'code' field in arguments")
	}

	// Extract tools to inject (if any)
	var tools []*types.Tool
	if toolsArg, ok := args["tools"]; ok {
		if toolsList, ok := toolsArg.([]*types.Tool); ok {
			tools = toolsList
		}
	}

	// Execute code in sandbox
	return e.sandbox.Execute(ctx, code, tools)
}

// Close implements the Executor interface
func (e *CodeModeExecutor) Close() error {
	return e.sandbox.Close()
}

// GetMode implements the Executor interface
func (e *CodeModeExecutor) GetMode() execution.ExecutionMode {
	return execution.CodeMode
}

