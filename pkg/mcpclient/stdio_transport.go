package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// StdioTransport implements Transport interface for stdio-based MCP servers
// Spawns a process and communicates via stdin/stdout (JSON-RPC over stdio)
type StdioTransport struct {
	config *TransportConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Request tracking for async responses
	pending   map[interface{}]chan *AsyncResult
	pendingMu sync.RWMutex

	// State
	connected    atomic.Bool
	restartCount int
	lastRestart  time.Time

	// Control
	done      chan struct{}
	readDone  chan struct{}
	writeMu   sync.Mutex // Serialize writes to stdin
	closeOnce sync.Once
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(cfg *TransportConfig) (*StdioTransport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("command is required for stdio transport")
	}

	t := &StdioTransport{
		config:   cfg,
		pending:  make(map[interface{}]chan *AsyncResult),
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
	}

	// Start the process
	if err := t.start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	return t, nil
}

// start spawns the MCP server process
func (t *StdioTransport) start() error {
	log.Info().
		Str("command", t.config.Command).
		Strs("args", t.config.Args).
		Msg("Starting stdio MCP server process")

	// Create command
	t.cmd = exec.Command(t.config.Command, t.config.Args...)

	// Set working directory if specified
	if t.config.WorkDir != "" {
		t.cmd.Dir = t.config.WorkDir
	}

	// Set environment if specified
	if len(t.config.Env) > 0 {
		env := make([]string, 0, len(t.config.Env))
		for k, v := range t.config.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		t.cmd.Env = append(t.cmd.Environ(), env...)
	}

	// Get stdin pipe
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	t.stdin = stdin

	// Get stdout pipe
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	t.stdout = stdout

	// Get stderr pipe for logging
	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}
	t.stderr = stderr

	// Start the process
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	t.connected.Store(true)

	// Start reader goroutine
	go t.readLoop()

	// Start stderr logger goroutine
	go t.logStderr()

	// Start process monitor goroutine
	go t.monitorProcess()

	log.Info().
		Int("pid", t.cmd.Process.Pid).
		Msg("Stdio MCP server process started")

	return nil
}

// readLoop reads responses from stdout and dispatches to pending requests
func (t *StdioTransport) readLoop() {
	defer close(t.readDone)

	scanner := bufio.NewScanner(t.stdout)
	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max

	for scanner.Scan() {
		select {
		case <-t.done:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse response
		var resp types.MCPResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			log.Error().
				Err(err).
				Str("line", string(line)).
				Msg("Failed to parse MCP response")
			continue
		}

		// Check if this is a notification (no ID)
		if resp.ID == nil {
			// Handle notification
			log.Debug().
				Interface("result", resp.Result).
				Msg("Received MCP notification")
			continue
		}

		// Find pending request
		// Normalize ID (JSON unmarshals numbers as float64)
		normalizedID := normalizeID(resp.ID)
		
		t.pendingMu.Lock()
		ch, ok := t.pending[normalizedID]
		if ok {
			delete(t.pending, normalizedID)
		}
		t.pendingMu.Unlock()

		if ok {
			ch <- &AsyncResult{
				Response:  &resp,
				RequestID: resp.ID,
			}
			close(ch)
		} else {
			log.Warn().
				Interface("id", resp.ID).
				Interface("normalizedID", normalizedID).
				Msg("Received response for unknown request")
		}
	}

	if err := scanner.Err(); err != nil {
		if t.connected.Load() {
			log.Error().Err(err).Msg("Error reading from stdio")
		}
	}
}

// logStderr logs stderr output from the process
func (t *StdioTransport) logStderr() {
	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		select {
		case <-t.done:
			return
		default:
		}

		log.Debug().
			Str("source", "mcp-stderr").
			Str("command", t.config.Command).
			Str("line", scanner.Text()).
			Msg("MCP server stderr")
	}
}

// monitorProcess monitors the process and handles restarts
func (t *StdioTransport) monitorProcess() {
	err := t.cmd.Wait()

	select {
	case <-t.done:
		// Intentional shutdown
		return
	default:
	}

	t.connected.Store(false)

	if err != nil {
		log.Error().
			Err(err).
			Str("command", t.config.Command).
			Msg("MCP server process exited with error")
	} else {
		log.Warn().
			Str("command", t.config.Command).
			Msg("MCP server process exited")
	}

	// Cancel all pending requests
	t.cancelAllPending(fmt.Errorf("process exited: %v", err))
	
	// Wait for read loop to finish before resetting channels
	select {
	case <-t.readDone:
		// Read loop finished
	case <-time.After(2 * time.Second):
		log.Warn().Msg("Read loop didn't finish in time during restart")
	}

	// Auto-restart if enabled
	if t.config.AutoRestart && t.restartCount < t.config.MaxRestarts {
		t.restartCount++
		t.lastRestart = time.Now()

		log.Info().
			Int("attempt", t.restartCount).
			Int("max", t.config.MaxRestarts).
			Dur("interval", t.config.RestartInterval).
			Msg("Restarting MCP server process")

		time.Sleep(t.config.RestartInterval)

		// Reset channels AFTER read loop has finished
		t.done = make(chan struct{})
		t.readDone = make(chan struct{})

		if err := t.start(); err != nil {
			log.Error().Err(err).Msg("Failed to restart MCP server process")
		}
	}
}

// cancelAllPending cancels all pending requests with an error
func (t *StdioTransport) cancelAllPending(err error) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()

	for id, ch := range t.pending {
		ch <- &AsyncResult{
			Error:     err,
			RequestID: id,
		}
		close(ch)
	}
	t.pending = make(map[interface{}]chan *AsyncResult)
}

// Send sends a synchronous request
func (t *StdioTransport) Send(ctx context.Context, req *types.MCPRequest) (*types.MCPResponse, error) {
	ch := t.SendAsync(ctx, req)

	select {
	case result := <-ch:
		if result.Error != nil {
			return nil, result.Error
		}
		return result.Response, nil
	case <-ctx.Done():
		// Remove from pending
		t.pendingMu.Lock()
		delete(t.pending, req.ID)
		t.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// SendAsync sends an async request and returns a channel for the response
func (t *StdioTransport) SendAsync(ctx context.Context, req *types.MCPRequest) <-chan *AsyncResult {
	ch := make(chan *AsyncResult, 1)

	if !t.connected.Load() {
		ch <- &AsyncResult{
			Error:     fmt.Errorf("transport not connected"),
			RequestID: req.ID,
		}
		close(ch)
		return ch
	}

	// Register pending request (normalize ID for consistent lookup)
	normalizedReqID := normalizeID(req.ID)
	t.pendingMu.Lock()
	t.pending[normalizedReqID] = ch
	t.pendingMu.Unlock()

	// Marshal request
	data, err := json.Marshal(req)
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, req.ID)
		t.pendingMu.Unlock()

		ch <- &AsyncResult{
			Error:     fmt.Errorf("failed to marshal request: %w", err),
			RequestID: req.ID,
		}
		close(ch)
		return ch
	}

	// Write to stdin (serialized)
	t.writeMu.Lock()
	_, err = t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()

	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, req.ID)
		t.pendingMu.Unlock()

		ch <- &AsyncResult{
			Error:     fmt.Errorf("failed to write to stdin: %w", err),
			RequestID: req.ID,
		}
		close(ch)
		return ch
	}

	// Handle context cancellation
	go func() {
		select {
		case <-ctx.Done():
			t.pendingMu.Lock()
			if pendingCh, ok := t.pending[normalizedReqID]; ok {
				delete(t.pending, normalizedReqID)
				pendingCh <- &AsyncResult{
					Error:     ctx.Err(),
					RequestID: req.ID,
				}
				close(pendingCh)
			}
			t.pendingMu.Unlock()
		case <-ch:
			// Request completed normally
		case <-t.done:
			// Transport closed
		}
	}()

	return ch
}

// Close closes the transport and kills the process
func (t *StdioTransport) Close() error {
	var closeErr error

	t.closeOnce.Do(func() {
		log.Info().Str("command", t.config.Command).Msg("Closing stdio transport")

		t.connected.Store(false)
		close(t.done)

		// Cancel all pending requests
		t.cancelAllPending(fmt.Errorf("transport closed"))

		// Close stdin to signal the process
		if t.stdin != nil {
			t.stdin.Close()
		}

		// Wait for read loop to finish
		select {
		case <-t.readDone:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("Read loop didn't finish in time")
		}

		// Kill the process if still running
		if t.cmd != nil && t.cmd.Process != nil {
			// Try graceful shutdown first
			if err := t.cmd.Process.Kill(); err != nil {
				log.Warn().Err(err).Msg("Failed to kill process")
			}
		}
	})

	return closeErr
}

// IsConnected returns true if the transport is connected
func (t *StdioTransport) IsConnected() bool {
	return t.connected.Load()
}

// Type returns the transport type
func (t *StdioTransport) Type() TransportType {
	return TransportStdio
}

// Reconnect restarts the process
func (t *StdioTransport) Reconnect(ctx context.Context) error {
	log.Info().Msg("Reconnecting stdio transport")

	// Mark as disconnected first
	t.connected.Store(false)
	
	// Cancel all pending requests before closing
	t.cancelAllPending(fmt.Errorf("reconnecting"))
	
	// Close stdin to signal the process to exit
	if t.stdin != nil {
		t.stdin.Close()
	}
	
	// Kill the process if still running
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	
	// Wait for read loop to finish with timeout
	select {
	case <-t.readDone:
	case <-time.After(3 * time.Second):
		log.Warn().Msg("Read loop didn't finish during reconnect")
	}

	// Reset state with proper synchronization
	t.pendingMu.Lock()
	t.pending = make(map[interface{}]chan *AsyncResult)
	t.pendingMu.Unlock()
	
	t.done = make(chan struct{})
	t.readDone = make(chan struct{})
	t.restartCount = 0

	// Start again
	return t.start()
}

// GetPID returns the process ID if running
func (t *StdioTransport) GetPID() int {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

// GetRestartCount returns the number of restarts
func (t *StdioTransport) GetRestartCount() int {
	return t.restartCount
}

// normalizeID normalizes request/response IDs for consistent map lookup
// JSON unmarshals numbers as float64, but we store them as int
func normalizeID(id interface{}) interface{} {
	switch v := id.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int64:
		return int(v)
	case int32:
		return int(v)
	default:
		return id
	}
}

