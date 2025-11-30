package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// HTTPTransport handles HTTP/SSE communication
type HTTPTransport struct {
	server     *Server
	httpServer *http.Server
	port       int
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(server *Server, port int) *HTTPTransport {
	ctx, cancel := context.WithCancel(context.Background())

	return &HTTPTransport{
		server: server,
		port:   port,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the HTTP transport
func (t *HTTPTransport) Start() error {
	mux := http.NewServeMux()

	// MCP POST endpoint
	mux.HandleFunc("/mcp", t.corsMiddleware(t.handleMCP))

	// SSE stream endpoint
	mux.HandleFunc("/mcp/stream", t.corsMiddleware(t.handleSSE))

	// Health check
	mux.HandleFunc("/health", t.handleHealth)

	t.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", t.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Info().Int("port", t.port).Msg("Starting MCP HTTP transport")

	go func() {
		if err := t.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return nil
}

// handleMCP handles JSON-RPC POST requests
func (t *HTTPTransport) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read request body")
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse request
	req, err := t.server.ParseRequest(body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse request")
		t.writeJSONError(w, nil, types.MCPErrorParseError, "Parse error")
		return
	}

	// Handle request
	resp := t.server.HandleRequest(req)

	// Write response
	data, err := t.server.FormatResponse(resp)
	if err != nil {
		log.Error().Err(err).Msg("Failed to format response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleSSE handles Server-Sent Events streaming
func (t *HTTPTransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Send keepalive ping
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// handleHealth handles health check requests
func (t *HTTPTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// corsMiddleware adds CORS headers
func (t *HTTPTransport) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

// writeJSONError writes a JSON-RPC error response
func (t *HTTPTransport) writeJSONError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := t.server.errorResponse(id, code, message)
	data, err := t.server.FormatResponse(resp)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return 200
	w.Write(data)
}

// Stop stops the HTTP transport
func (t *HTTPTransport) Stop() error {
	log.Info().Msg("Stopping HTTP transport")

	t.cancel()

	if t.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := t.httpServer.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("HTTP server shutdown error")
			return err
		}
	}

	return nil
}
