package http

import (
	"bufio"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/jobs"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Handler handles HTTP requests
type Handler struct {
	manager    *toolkit.Manager
	executor   *execution.ExecutorRegistry
	router     *semantic.Router
	jobManager *jobs.JobManager
}

// NewHandler creates a new handler
func NewHandler(manager *toolkit.Manager, executor *execution.ExecutorRegistry, router *semantic.Router) *Handler {
	return &Handler{
		manager:  manager,
		executor: executor,
		router:   router,
	}
}

// SetJobManager sets the job manager for async operations
func (h *Handler) SetJobManager(jm *jobs.JobManager) {
	h.jobManager = jm
}

// ListTools handles GET/POST /api/v1/tools
func (h *Handler) ListTools(c fiber.Ctx) error {
	var req types.ListToolsRequest

	// Try to parse body (for POST requests)
	if c.Method() == "POST" {
		if err := c.Bind().JSON(&req); err != nil {
			// Ignore parse errors, use defaults
			log.Debug().Err(err).Msg("Failed to parse request body")
		}
	}

	// Get query parameters (for GET requests)
	if req.Query == "" {
		req.Query = c.Query("query", "")
	}
	if req.Page == 0 {
		if page := c.Query("page"); page != "" {
			if p, err := strconv.Atoi(page); err == nil && p > 0 {
				req.Page = p
			}
		}
	}
	if req.PageSize == 0 {
		if pageSize := c.Query("page_size"); pageSize != "" {
			if ps, err := strconv.Atoi(pageSize); err == nil && ps > 0 {
				req.PageSize = ps
			}
		}
	}

	// Get all tools
	var tools []*types.Tool
	if req.Query != "" || len(req.Tags) > 0 {
		tools = h.manager.SearchTools(req.Query, req.Tags)
	} else {
		tools = h.manager.ListAllTools()
	}

	// Pagination (simple version)
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 50
	}

	total := len(tools)
	start := (req.Page - 1) * req.PageSize
	end := start + req.PageSize

	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	paginatedTools := tools[start:end]

	resp := types.ListToolsResponse{
		Tools:      paginatedTools,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: (total + req.PageSize - 1) / req.PageSize,
	}

	return c.JSON(resp)
}

// ExecuteTool handles POST /api/v1/tools/:id/execute
func (h *Handler) ExecuteTool(c fiber.Ctx) error {
	toolID := c.Params("id")

	// Validate toolID
	if toolID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Tool ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	var req types.ExecuteToolRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Invalid request body",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	// Get tool
	tool, err := h.manager.GetTool(toolID)
	if err != nil {
		// Try by name (toolbox.tool format)
		tool, err = h.manager.GetToolByName(toolID)
		if err != nil {
			return c.Status(404).JSON(types.ErrorResponse{
				Error:     "Tool not found",
				Code:      "not_found",
				Timestamp: time.Now(),
			})
		}
	}

	// Execute tool via execution engine
	result, err := h.executor.Execute(c.Context(), execution.DirectMode, tool, req.Args)
	if err != nil {
		log.Error().
			Err(err).
			Str("tool", tool.Name).
			Msg("Tool execution failed")

		return c.Status(500).JSON(types.ErrorResponse{
			Error:     "Tool execution failed",
			Code:      "execution_error",
			Details:   map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
		})
	}

	// Convert execution result to API response
	resp := types.ExecuteToolResponse{
		Success:    result.Success,
		Result:     result.Result,
		Duration:   result.Duration.Milliseconds(),
		TokensUsed: result.TokensUsed,
		Error:      result.Error,
	}

	log.Info().
		Str("tool", tool.Name).
		Interface("args", req.Args).
		Bool("success", result.Success).
		Dur("duration", result.Duration).
		Msg("Tool executed")

	// Return appropriate status code based on success
	if !result.Success {
		return c.Status(500).JSON(resp)
	}

	return c.JSON(resp)
}

// GetToolSchema handles GET /api/v1/tools/:id/schema
func (h *Handler) GetToolSchema(c fiber.Ctx) error {
	toolID := c.Params("id")

	tool, err := h.manager.GetTool(toolID)
	if err != nil {
		tool, err = h.manager.GetToolByName(toolID)
		if err != nil {
			return c.Status(404).JSON(types.ErrorResponse{
				Error:     "Tool not found",
				Code:      "not_found",
				Timestamp: time.Now(),
			})
		}
	}

	return c.JSON(fiber.Map{
		"id":          tool.ID,
		"name":        tool.Name,
		"description": tool.Description,
		"inputSchema": tool.InputSchema,
	})
}

// ListToolboxes handles GET /api/v1/toolboxes
func (h *Handler) ListToolboxes(c fiber.Ctx) error {
	toolboxes := h.manager.ListToolboxes()

	return c.JSON(fiber.Map{
		"toolboxes": toolboxes,
		"total":     len(toolboxes),
	})
}

// CreateToolkit handles POST /api/v1/toolkits
// Registers a new toolkit with toolboxes and tools
func (h *Handler) CreateToolkit(c fiber.Ctx) error {
	var toolkit types.Toolkit
	if err := c.Bind().JSON(&toolkit); err != nil {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Invalid request body",
			Code:      "bad_request",
			Details:   map[string]interface{}{"parse_error": err.Error()},
			Timestamp: time.Now(),
		})
	}

	// Validate
	if toolkit.Name == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Toolkit name is required",
			Code:      "validation_error",
			Timestamp: time.Now(),
		})
	}

	// Register toolkit (will auto-generate IDs and index to Typesense)
	if err := h.manager.RegisterToolkit(&toolkit); err != nil {
		return c.Status(500).JSON(types.ErrorResponse{
			Error:     err.Error(),
			Code:      "internal_error",
			Timestamp: time.Now(),
		})
	}

	log.Info().
		Str("toolkit_id", toolkit.ID).
		Str("name", toolkit.Name).
		Int("toolboxes", len(toolkit.Toolboxes)).
		Msg("Toolkit registered via API")

	return c.Status(201).JSON(fiber.Map{
		"success":    true,
		"toolkit_id": toolkit.ID,
		"message":    "Toolkit registered successfully",
		"stats": fiber.Map{
			"toolboxes": len(toolkit.Toolboxes),
			"tools":     countTools(toolkit.Toolboxes),
		},
	})
}

// DeleteToolkit handles DELETE /api/v1/toolkits/:id
func (h *Handler) DeleteToolkit(c fiber.Ctx) error {
	toolkitID := c.Params("id")

	if toolkitID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Toolkit ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	if err := h.manager.UnregisterToolkit(toolkitID); err != nil {
		return c.Status(404).JSON(types.ErrorResponse{
			Error:     err.Error(),
			Code:      "not_found",
			Timestamp: time.Now(),
		})
	}

	log.Info().
		Str("toolkit_id", toolkitID).
		Msg("Toolkit unregistered via API")

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Toolkit unregistered successfully",
	})
}

// GetToolkit handles GET /api/v1/toolkits/:id
func (h *Handler) GetToolkit(c fiber.Ctx) error {
	toolkitID := c.Params("id")

	toolkit, err := h.manager.GetToolkit(toolkitID)
	if err != nil {
		return c.Status(404).JSON(types.ErrorResponse{
			Error:     err.Error(),
			Code:      "not_found",
			Timestamp: time.Now(),
		})
	}

	return c.JSON(toolkit)
}

// ListToolkits handles GET /api/v1/toolkits
func (h *Handler) ListToolkits(c fiber.Ctx) error {
	toolkits := h.manager.ListToolkits()

	return c.JSON(fiber.Map{
		"toolkits": toolkits,
		"total":    len(toolkits),
	})
}

// countTools counts total tools in toolboxes
func countTools(toolboxes []*types.Toolbox) int {
	count := 0
	for _, tb := range toolboxes {
		count += len(tb.Tools)
	}
	return count
}

// GetToolbox handles GET /api/v1/toolboxes/:id
func (h *Handler) GetToolbox(c fiber.Ctx) error {
	toolboxID := c.Params("id")

	// Find toolbox
	toolboxes := h.manager.ListToolboxes()
	for _, tb := range toolboxes {
		if tb.ID == toolboxID || tb.Name == toolboxID {
			return c.JSON(tb)
		}
	}

	return c.Status(404).JSON(types.ErrorResponse{
		Error:     "Toolbox not found",
		Code:      "not_found",
		Timestamp: time.Now(),
	})
}

// HealthCheck handles GET /api/v1/health
func (h *Handler) HealthCheck(c fiber.Ctx) error {
	stats := h.manager.GetStats()

	return c.JSON(fiber.Map{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"stats":     stats,
	})
}

// ReadinessProbe handles GET /api/v1/ready
func (h *Handler) ReadinessProbe(c fiber.Ctx) error {
	// Check if manager has tools loaded
	tools := h.manager.ListAllTools()

	if len(tools) == 0 {
		return c.Status(503).JSON(fiber.Map{
			"status": "not_ready",
			"reason": "no tools loaded",
		})
	}

	return c.JSON(fiber.Map{
		"status": "ready",
	})
}

// LivenessProbe handles GET /api/v1/alive
func (h *Handler) LivenessProbe(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "alive",
	})
}

// PrometheusMetrics handles GET /metrics
func (h *Handler) PrometheusMetrics(c fiber.Ctx) error {
	stats := h.manager.GetStats()

	// Simple Prometheus text format
	metrics := fiber.Map{
		"saltare_toolboxes_total": stats["toolboxes"],
		"saltare_tools_total":     stats["tools"],
		"saltare_toolkits_total":  stats["toolkits"],
	}

	return c.JSON(metrics)
}

// ============================================
// Job Handlers (Async Operations)
// ============================================

// CreateJob handles POST /api/v1/jobs
// Creates a new async job for tool execution
func (h *Handler) CreateJob(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	var req jobs.CreateJobRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Invalid request body",
			Code:      "bad_request",
			Details:   map[string]interface{}{"parse_error": err.Error()},
			Timestamp: time.Now(),
		})
	}

	// Validate request
	if req.ToolName == "" && req.ToolID == "" && req.Query == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Either tool_name, tool_id, or query must be provided",
			Code:      "validation_error",
			Timestamp: time.Now(),
		})
	}

	// Create job
	job, err := h.jobManager.CreateJob(c.Context(), &req)
	if err != nil {
		return c.Status(500).JSON(types.ErrorResponse{
			Error:     "Failed to create job",
			Code:      "internal_error",
			Details:   map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
		})
	}

	log.Info().
		Str("job_id", job.ID).
		Str("tool", req.ToolName).
		Str("query", req.Query).
		Msg("Job created via API")

	return c.Status(202).JSON(job.ToResponse())
}

// GetJob handles GET /api/v1/jobs/:id
// Retrieves a job by ID
func (h *Handler) GetJob(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	jobID := c.Params("id")
	if jobID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Job ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	job, err := h.jobManager.GetJob(c.Context(), jobID)
	if err != nil {
		return c.Status(404).JSON(types.ErrorResponse{
			Error:     "Job not found",
			Code:      "not_found",
			Timestamp: time.Now(),
		})
	}

	return c.JSON(job.ToResponse())
}

// ListJobs handles GET /api/v1/jobs
// Lists jobs with optional filters
func (h *Handler) ListJobs(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	filter := &jobs.JobListFilter{}

	// Parse query params
	if status := c.Query("status"); status != "" {
		s := jobs.JobStatus(status)
		filter.Status = &s
	}
	if toolName := c.Query("tool_name"); toolName != "" {
		filter.ToolName = toolName
	}
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 {
			filter.Limit = l
		}
	}
	if offset := c.Query("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	jobsList, err := h.jobManager.ListJobs(c.Context(), filter)
	if err != nil {
		return c.Status(500).JSON(types.ErrorResponse{
			Error:     "Failed to list jobs",
			Code:      "internal_error",
			Timestamp: time.Now(),
		})
	}

	// Convert to responses
	responses := make([]*jobs.JobResponse, len(jobsList))
	for i, job := range jobsList {
		responses[i] = job.ToResponse()
	}

	return c.JSON(fiber.Map{
		"jobs":  responses,
		"total": len(responses),
	})
}

// CancelJob handles DELETE /api/v1/jobs/:id
// Cancels a job if it's not in terminal state
func (h *Handler) CancelJob(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	jobID := c.Params("id")
	if jobID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Job ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	if err := h.jobManager.CancelJob(c.Context(), jobID); err != nil {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     err.Error(),
			Code:      "cancel_failed",
			Timestamp: time.Now(),
		})
	}

	log.Info().Str("job_id", jobID).Msg("Job cancelled via API")

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Job cancelled",
	})
}

// StreamJob handles GET /api/v1/jobs/:id/stream
// Streams job events via SSE
func (h *Handler) StreamJob(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	jobID := c.Params("id")
	if jobID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Job ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	// Check job exists
	_, err := h.jobManager.GetJob(c.Context(), jobID)
	if err != nil {
		return c.Status(404).JSON(types.ErrorResponse{
			Error:     "Job not found",
			Code:      "not_found",
			Timestamp: time.Now(),
		})
	}

	// Set SSE headers
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	// Stream using fasthttp StreamWriter
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		sseWriter := jobs.NewSSEWriter(w, func() {
			w.Flush()
		})

		if err := jobs.StreamJob(c.Context(), h.jobManager, jobID, sseWriter); err != nil {
			log.Error().Err(err).Str("job_id", jobID).Msg("SSE stream error")
			sseWriter.WriteError(err)
		}
	})

	return nil
}

// GetJobStats handles GET /api/v1/jobs/stats
// Returns job queue statistics
func (h *Handler) GetJobStats(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	stats := h.jobManager.GetStats()

	return c.JSON(fiber.Map{
		"stats":     stats,
		"timestamp": time.Now().Unix(),
	})
}

// WaitForJob handles POST /api/v1/jobs/:id/wait
// Waits for a job to complete with optional timeout
func (h *Handler) WaitForJob(c fiber.Ctx) error {
	if h.jobManager == nil {
		return c.Status(503).JSON(types.ErrorResponse{
			Error:     "Job manager not available",
			Code:      "service_unavailable",
			Timestamp: time.Now(),
		})
	}

	jobID := c.Params("id")
	if jobID == "" {
		return c.Status(400).JSON(types.ErrorResponse{
			Error:     "Job ID is required",
			Code:      "bad_request",
			Timestamp: time.Now(),
		})
	}

	// Parse timeout from query (default 60s)
	timeout := 60 * time.Second
	if t := c.Query("timeout"); t != "" {
		if seconds, err := strconv.Atoi(t); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}

	// Wait for job
	job, err := h.jobManager.WaitForJob(c.Context(), jobID, timeout)
	if err != nil {
		return c.Status(408).JSON(types.ErrorResponse{
			Error:     err.Error(),
			Code:      "timeout",
			Timestamp: time.Now(),
		})
	}

	return c.JSON(job.ToResponse())
}
