package codemode

// Package codemode implements Code Mode execution using Goja JavaScript sandbox.
// Provides VM pooling, tool injection, security isolation, and async/await support.

import (
	"context"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Sandbox represents a Code Mode execution environment
type Sandbox struct {
	vmPool  *VMPool
	timeout time.Duration
	// directMode for making actual MCP calls
	directExecutor execution.Executor
}

// NewSandbox creates a new Code Mode sandbox with VM pooling
func NewSandbox(poolSize int, timeout time.Duration, directExecutor execution.Executor) *Sandbox {
	return &Sandbox{
		vmPool:         NewVMPool(poolSize),
		timeout:        timeout,
		directExecutor: directExecutor,
	}
}

// Execute runs JavaScript code in a sandboxed environment
func (s *Sandbox) Execute(ctx context.Context, code string, tools []*types.Tool) (*execution.ExecutionResult, error) {
	startTime := time.Now()

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Acquire VM from pool
	vm, err := s.vmPool.Acquire()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire VM: %w", err)
	}
	defer s.vmPool.Release(vm)

	// Inject tools as JavaScript objects
	if err := s.injectTools(vm, tools, execCtx); err != nil {
		return nil, fmt.Errorf("failed to inject tools: %w", err)
	}

	// Execute code with panic recovery
	resultChan := make(chan *executionResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultChan <- &executionResult{
					err: fmt.Errorf("panic during execution: %v", r),
				}
			}
		}()

		value, err := vm.RunString(code)
		if err != nil {
			resultChan <- &executionResult{err: fmt.Errorf("execution error: %w", err)}
			return
		}

		// Extract result
		result := value.Export()
		resultChan <- &executionResult{
			value: result,
			err:   nil,
		}
	}()

	// Wait for execution or timeout
	select {
	case result := <-resultChan:
		if result.err != nil {
			log.Error().Err(result.err).Str("code", code).Msg("Code execution failed")
			return &execution.ExecutionResult{
				Success:  false,
				Result:   nil,
				Error:    result.err.Error(),
				Duration: time.Since(startTime),
			}, result.err
		}

		log.Info().
			Dur("duration", time.Since(startTime)).
			Msg("Code executed successfully")

		return &execution.ExecutionResult{
			Success:  true,
			Result:   result.value,
			Error:    "",
			Duration: time.Since(startTime),
		}, nil

	case <-execCtx.Done():
		// Interrupt VM to stop goroutine
		vm.Interrupt("execution timeout")
		
		// Wait briefly for goroutine to finish (it will panic and send to resultChan)
		select {
		case <-resultChan:
			// Goroutine finished
		case <-time.After(100 * time.Millisecond):
			// Goroutine still running, but channel is buffered so it won't leak
		}
		
		return &execution.ExecutionResult{
			Success:  false,
			Result:   nil,
			Error:    "execution timeout exceeded",
			Duration: time.Since(startTime),
		}, fmt.Errorf("execution timeout")
	}
}

// injectTools creates JavaScript wrapper functions for tools
func (s *Sandbox) injectTools(vm *goja.Runtime, tools []*types.Tool, ctx context.Context) error {
	// Inject each tool as a global function
	for _, tool := range tools {
		// Capture tool in closure
		toolRef := tool
		
		// Create function wrapper
		fn := func(call goja.FunctionCall) goja.Value {
			// Extract arguments from JavaScript
			var args map[string]interface{}
			if len(call.Arguments) > 0 {
				exported := call.Arguments[0].Export()
				if argMap, ok := exported.(map[string]interface{}); ok {
					args = argMap
				}
			}

			// Execute tool via DirectMode
			result, err := s.directExecutor.Execute(ctx, toolRef, args)
			if err != nil {
				panic(vm.NewGoError(err))
			}

			if !result.Success {
				panic(vm.NewGoError(fmt.Errorf(result.Error)))
			}

			// Return result to JavaScript
			return vm.ToValue(result.Result)
		}

		// Set function as global
		if err := vm.Set(tool.Name, fn); err != nil {
			return fmt.Errorf("failed to set tool %s: %w", tool.Name, err)
		}

		log.Debug().
			Str("tool", tool.Name).
			Msg("Injected tool function")
	}

	return nil
}

// Close releases resources
func (s *Sandbox) Close() error {
	s.vmPool.Close()
	return nil
}

// GetMode returns the execution mode
func (s *Sandbox) GetMode() execution.ExecutionMode {
	return execution.CodeMode
}

// executionResult holds the result of code execution
type executionResult struct {
	value interface{}
	err   error
}

