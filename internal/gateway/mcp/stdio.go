package mcp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// StdioTransport handles stdio communication (for Cursor, Claude Desktop)
type StdioTransport struct {
	server *Server
	ctx    context.Context
	cancel context.CancelFunc
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(server *Server) *StdioTransport {
	ctx, cancel := context.WithCancel(context.Background())

	return &StdioTransport{
		server: server,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the stdio transport loop
func (t *StdioTransport) Start() error {
	log.Info().Msg("Starting MCP stdio transport")

	// Create buffered reader for stdin
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-t.ctx.Done():
			log.Info().Msg("Stdio transport stopped")
			return nil
		default:
			// Read line from stdin
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					log.Info().Msg("Stdin closed, exiting")
					return nil
				}
				log.Error().Err(err).Msg("Failed to read from stdin")
				continue
			}

			// Parse request
			req, err := t.server.ParseRequest(line)
			if err != nil {
				log.Error().Err(err).Str("line", string(line)).Msg("Failed to parse request")
				t.writeError(nil, -32700, fmt.Sprintf("Parse error: %v", err))
				continue
			}

			// Handle request
			resp := t.server.HandleRequest(req)

			// Write response
			if err := t.writeResponse(resp); err != nil {
				log.Error().Err(err).Msg("Failed to write response")
				continue
			}
		}
	}
}

// writeResponse writes a response to stdout
func (t *StdioTransport) writeResponse(resp interface{}) error {
	data, err := t.server.FormatResponse(resp.(*types.MCPResponse))
	if err != nil {
		return err
	}

	// Write to stdout with newline
	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		return err
	}

	// Flush stdout
	if err := os.Stdout.Sync(); err != nil {
		return err
	}

	return nil
}

// writeError writes an error to stdout
func (t *StdioTransport) writeError(id interface{}, code int, message string) error {
	resp := t.server.errorResponse(id, code, message)
	return t.writeResponse(resp)
}

// Stop stops the stdio transport
func (t *StdioTransport) Stop() error {
	log.Info().Msg("Stopping stdio transport")
	t.cancel()
	return nil
}
