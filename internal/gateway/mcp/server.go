package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/jobs"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Server represents an MCP protocol server
type Server struct {
	manager     *toolkit.Manager
	executor    *execution.ExecutorRegistry
	router      *semantic.Router
	search      search.Provider  // Optional: for smart tool discovery (Meilisearch/Typesense)
	jobManager  *jobs.JobManager // Optional: for async operations
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	initialized atomic.Bool // Thread-safe initialization flag
}

// NewServer creates a new MCP server
func NewServer(manager *toolkit.Manager, executor *execution.ExecutorRegistry, router *semantic.Router) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		manager:  manager,
		executor: executor,
		router:   router,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SetSearchClient sets the search provider for smart tool discovery
func (s *Server) SetSearchClient(client search.Provider) {
	s.search = client
	log.Info().Msg("MCP server: smart tool discovery enabled")
}

// SetJobManager sets the job manager for async operations
func (s *Server) SetJobManager(jm *jobs.JobManager) {
	s.jobManager = jm
	log.Info().Msg("MCP server: async operations enabled")
}

// HandleRequest handles a single MCP request
func (s *Server) HandleRequest(req *types.MCPRequest) *types.MCPResponse {
	log.Debug().
		Str("method", req.Method).
		Interface("id", req.ID).
		Msg("MCP request received")

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	// Standard MCP methods (tools/list, tools/call) + legacy aliases
	case "tools/list", "list_tools":
		return s.handleListTools(req)
	case "tools/call", "call_tool":
		return s.handleCallTool(req)
	case "resources/list", "list_resources":
		return s.handleListResources(req)
	// Job management methods (async operations)
	case "get_job":
		return s.handleGetJob(req)
	case "list_jobs":
		return s.handleListJobs(req)
	case "cancel_job":
		return s.handleCancelJob(req)
	default:
		return s.errorResponse(req.ID, types.MCPErrorMethodNotFound,
			fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleInitialize handles the initialize handshake
func (s *Server) handleInitialize(req *types.MCPRequest) *types.MCPResponse {
	s.initialized.Store(true)

	result := types.MCPInitializeResult{
		ProtocolVersion: "2024-11-05", // MCP uses date-based versioning (YYYY-MM-DD)
	}
	// Capabilities - объект с sub-capabilities по спецификации MCP
	result.Capabilities.Tools.ListChanged = true
	result.ServerInfo.Name = "Saltare"
	result.ServerInfo.Version = "0.1.0"

	log.Info().Msg("MCP client initialized")

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleListTools handles the list_tools method
func (s *Server) handleListTools(req *types.MCPRequest) *types.MCPResponse {
	// HTTP transport is stateless - no initialization check needed
	// For stdio transport, initialization happens in the same session

	tools := s.manager.ListAllTools()

	mcpTools := make([]types.MCPToolInfo, 0, len(tools))
	for _, tool := range tools {
		mcpTools = append(mcpTools, types.MCPToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	result := map[string]interface{}{
		"tools": mcpTools,
	}

	log.Debug().Int("count", len(mcpTools)).Msg("Listed tools")

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleCallTool handles the call_tool method
// Supports three modes:
// 1. Direct call: {"name": "weather.get_current", "arguments": {"city": "Moscow"}}
// 2. Smart call:  {"query": "какая погода в Москве?"} - uses Semantic Router + Typesense
// 3. Async call:  {"name": "...", "async": true} - returns job_id immediately
func (s *Server) handleCallTool(req *types.MCPRequest) *types.MCPResponse {
	if !s.initialized.Load() {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			"server not initialized, call initialize first")
	}

	// Check for async mode
	asyncMode := false
	if async, ok := req.Params["async"].(bool); ok && async {
		asyncMode = true
	}
	if asyncStr, ok := req.Params["_async"].(string); ok && asyncStr == "true" {
		asyncMode = true
	}

	var tool *types.Tool
	var args map[string]interface{}
	var toolName string
	var query string

	// Check for query (smart mode)
	if q, ok := req.Params["query"].(string); ok && q != "" {
		query = q
		log.Info().Str("query", query).Msg("Smart tool call: parsing query")

		// Use Semantic Router with LLM (Cerebras primary, Ollama fallback)
		foundTool, err := s.router.Route(context.Background(), query)
		if err != nil {
			log.Error().Err(err).Str("query", query).Msg("Failed to route query with LLM")
			return s.errorResponse(req.ID, types.MCPErrorServerError,
				fmt.Sprintf("no tool found for query: %s", query))
		}

		tool = foundTool
		toolName = tool.Name

		// Extract parameters using LLM (smart extraction!)
		args = make(map[string]interface{})
		if s.router != nil && tool.InputSchema != nil {
			// Use LLM to extract parameters from natural language
			extractedParams, err := s.router.ExtractParametersFromQuery(context.Background(), query, tool.InputSchema)
			if err != nil {
				log.Warn().Err(err).Str("query", query).Msg("LLM parameter extraction failed")
			} else {
				args = extractedParams
				log.Debug().Interface("params", args).Str("query", query).Msg("Parameters extracted via LLM")
			}
		}

		// Always pass the original query for tools that can use it
		args["_query"] = query

	} else {
		// Direct mode: require name
		var ok bool
		toolName, ok = req.Params["name"].(string)
		if !ok {
			return s.errorResponse(req.ID, types.MCPErrorInvalidParams,
				"missing required parameter: name or query")
		}

		// Extract arguments
		args, ok = req.Params["arguments"].(map[string]interface{})
		if !ok {
			args = make(map[string]interface{})
		}

		// Get tool from manager
		var err error
		tool, err = s.manager.GetToolByName(toolName)
		if err != nil {
			// Try by ID
			tool, err = s.manager.GetTool(toolName)
			if err != nil {
				// Fallback: search by short name (for MCP clients that don't use qualified names)
				allTools := s.manager.ListAllTools()
				for _, t := range allTools {
					if t.Name == toolName {
						tool = t
						err = nil
						log.Debug().
							Str("short_name", toolName).
							Str("tool_id", t.ID).
							Msg("Tool found by short name (fallback)")
						break
					}
				}
				if err != nil {
					return s.errorResponse(req.ID, types.MCPErrorServerError,
						fmt.Sprintf("tool not found: %s", toolName))
				}
			}
		}
	}

	// Async mode: create job and return immediately
	if asyncMode {
		if s.jobManager == nil {
			return s.errorResponse(req.ID, types.MCPErrorServerError,
				"async mode not available: job manager not configured")
		}

		jobReq := &jobs.CreateJobRequest{
			ToolName: toolName,
			ToolID:   tool.ID,
			Args:     args,
			Query:    query,
		}

		job, err := s.jobManager.CreateJob(context.Background(), jobReq)
		if err != nil {
			return s.errorResponse(req.ID, types.MCPErrorServerError,
				fmt.Sprintf("failed to create async job: %v", err))
		}

		log.Info().
			Str("job_id", job.ID).
			Str("tool", toolName).
			Msg("Async job created via MCP")

		// Return job info in MCP format
		result := map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Job created: %s", job.ID),
				},
			},
			"isError": false,
			"job": map[string]interface{}{
				"id":         job.ID,
				"status":     string(job.Status),
				"tool":       toolName,
				"created_at": job.CreatedAt.Format(time.RFC3339),
			},
		}

		return &types.MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
	}

	// Sync mode: execute tool and wait for result
	execResult, err := s.executor.Execute(context.Background(), execution.DirectMode, tool, args)
	if err != nil {
		log.Error().
			Err(err).
			Str("tool", toolName).
			Msg("Tool execution failed via MCP")

		return s.errorResponse(req.ID, types.MCPErrorServerError,
			fmt.Sprintf("tool execution failed: %v", err))
	}

	// Format result in MCP format
	var resultContent []map[string]interface{}

	if !execResult.Success {
		resultContent = []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Error: %s", execResult.Error),
			},
		}
	} else {
		// Convert result to MCP content format
		resultContent = []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%v", execResult.Result),
			},
		}
	}

	result := map[string]interface{}{
		"content":   resultContent,
		"isError":   !execResult.Success,
		"tool_used": toolName, // Include which tool was used (useful for smart mode)
	}

	log.Info().
		Str("tool", toolName).
		Interface("args", args).
		Bool("success", execResult.Success).
		Dur("duration", execResult.Duration).
		Msg("Tool executed via MCP")

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleListResources handles the list_resources method (optional)
func (s *Server) handleListResources(req *types.MCPRequest) *types.MCPResponse {
	// Resources are optional in MCP, return empty list
	result := map[string]interface{}{
		"resources": []interface{}{},
	}

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleGetJob handles the get_job method
// Retrieves the status and result of an async job
func (s *Server) handleGetJob(req *types.MCPRequest) *types.MCPResponse {
	if s.jobManager == nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			"job manager not available")
	}

	jobID, ok := req.Params["job_id"].(string)
	if !ok {
		return s.errorResponse(req.ID, types.MCPErrorInvalidParams,
			"missing required parameter: job_id")
	}

	job, err := s.jobManager.GetJob(context.Background(), jobID)
	if err != nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			fmt.Sprintf("job not found: %s", jobID))
	}

	// Format job in MCP-friendly format
	jobResult := map[string]interface{}{
		"id":         job.ID,
		"status":     string(job.Status),
		"tool":       job.ToolName,
		"progress":   job.Progress,
		"created_at": job.CreatedAt.Format(time.RFC3339),
	}

	if job.StartedAt != nil {
		jobResult["started_at"] = job.StartedAt.Format(time.RFC3339)
	}
	if job.CompletedAt != nil {
		jobResult["completed_at"] = job.CompletedAt.Format(time.RFC3339)
		jobResult["duration_ms"] = job.Duration().Milliseconds()
	}
	if job.Error != "" {
		jobResult["error"] = job.Error
	}
	if job.Result != nil {
		jobResult["result"] = job.Result.Result
		jobResult["success"] = job.Result.Success
	}

	result := map[string]interface{}{
		"job": jobResult,
	}

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleListJobs handles the list_jobs method
// Lists all async jobs with optional filters
func (s *Server) handleListJobs(req *types.MCPRequest) *types.MCPResponse {
	if s.jobManager == nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			"job manager not available")
	}

	filter := &jobs.JobListFilter{
		Limit: 50, // Default limit
	}

	// Parse optional filters
	if status, ok := req.Params["status"].(string); ok {
		s := jobs.JobStatus(status)
		filter.Status = &s
	}
	if toolName, ok := req.Params["tool_name"].(string); ok {
		filter.ToolName = toolName
	}
	if limit, ok := req.Params["limit"].(float64); ok {
		filter.Limit = int(limit)
	}

	jobList, err := s.jobManager.ListJobs(context.Background(), filter)
	if err != nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			fmt.Sprintf("failed to list jobs: %v", err))
	}

	// Format jobs
	jobsResult := make([]map[string]interface{}, len(jobList))
	for i, job := range jobList {
		jobsResult[i] = map[string]interface{}{
			"id":         job.ID,
			"status":     string(job.Status),
			"tool":       job.ToolName,
			"created_at": job.CreatedAt.Format(time.RFC3339),
		}
		if job.CompletedAt != nil {
			jobsResult[i]["completed_at"] = job.CompletedAt.Format(time.RFC3339)
		}
	}

	// Get stats
	stats := s.jobManager.GetStats()

	result := map[string]interface{}{
		"jobs":  jobsResult,
		"total": len(jobsResult),
		"stats": map[string]interface{}{
			"pending":   stats.PendingJobs,
			"running":   stats.RunningJobs,
			"completed": stats.CompletedJobs,
			"failed":    stats.FailedJobs,
		},
	}

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleCancelJob handles the cancel_job method
// Cancels an async job if it's not in terminal state
func (s *Server) handleCancelJob(req *types.MCPRequest) *types.MCPResponse {
	if s.jobManager == nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			"job manager not available")
	}

	jobID, ok := req.Params["job_id"].(string)
	if !ok {
		return s.errorResponse(req.ID, types.MCPErrorInvalidParams,
			"missing required parameter: job_id")
	}

	if err := s.jobManager.CancelJob(context.Background(), jobID); err != nil {
		return s.errorResponse(req.ID, types.MCPErrorServerError,
			fmt.Sprintf("failed to cancel job: %v", err))
	}

	log.Info().Str("job_id", jobID).Msg("Job cancelled via MCP")

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Job %s cancelled", jobID),
	}

	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// errorResponse creates an error response
func (s *Server) errorResponse(id interface{}, code int, message string) *types.MCPResponse {
	return &types.MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &types.MCPError{
			Code:    code,
			Message: message,
		},
	}
}

// ParseRequest parses a JSON-RPC request
func (s *Server) ParseRequest(data []byte) (*types.MCPRequest, error) {
	var req types.MCPRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		return nil, fmt.Errorf("invalid JSON-RPC version: %s", req.JSONRPC)
	}

	return &req, nil
}

// FormatResponse formats a response as JSON
func (s *Server) FormatResponse(resp *types.MCPResponse) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to format response: %w", err)
	}
	return data, nil
}

// Stop stops the MCP server
func (s *Server) Stop() error {
	log.Info().Msg("Stopping MCP server")
	s.cancel()
	s.wg.Wait()
	return nil
}
