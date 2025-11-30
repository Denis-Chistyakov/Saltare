package http

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/analytics"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/jobs"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Server represents the HTTP API server
type Server struct {
	app       *fiber.App
	manager   *toolkit.Manager
	executor  *execution.ExecutorRegistry
	router    *semantic.Router
	collector *analytics.Collector
	config    *types.ServerConfig
	handlers  *Handler
	port      int
}

// NewServer creates a new HTTP server
func NewServer(manager *toolkit.Manager, executor *execution.ExecutorRegistry, router *semantic.Router, collector *analytics.Collector, config *types.ServerConfig) *Server {
	app := fiber.New(fiber.Config{
		ServerHeader: "Saltare",
		AppName:      "Saltare v0.1.0",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		BodyLimit:    10 * 1024 * 1024, // 10MB
	})

	s := &Server{
		app:       app,
		manager:   manager,
		executor:  executor,
		router:    router,
		collector: collector,
		config:    config,
		port:      config.Port,
	}

	// Initialize handlers
	s.handlers = NewHandler(manager, executor, router)

	// Setup routes
	s.setupRoutes()

	return s
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	// Root
	s.app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name":    "Saltare",
			"version": "0.1.0",
			"status":  "running",
		})
	})

	// Metrics endpoint (Prometheus)
	s.app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	// Analytics/Stats endpoint
	s.app.Get("/stats", func(c fiber.Ctx) error {
		if s.collector == nil {
			return c.Status(503).JSON(fiber.Map{
				"error": "Analytics not enabled",
			})
		}
		stats := s.collector.GetStats()
		return c.JSON(stats)
	})

	// API v1 routes
	api := s.app.Group("/api/v1")

	// Tools
	api.Get("/tools", s.handlers.ListTools)
	api.Post("/tools", s.handlers.ListTools) // Support both GET and POST
	api.Post("/tools/:id/execute", s.handlers.ExecuteTool)
	api.Get("/tools/:id/schema", s.handlers.GetToolSchema)

	// Toolboxes
	api.Get("/toolboxes", s.handlers.ListToolboxes)
	api.Get("/toolboxes/:id", s.handlers.GetToolbox)

	// Toolkits (CRUD)
	api.Get("/toolkits", s.handlers.ListToolkits)
	api.Post("/toolkits", s.handlers.CreateToolkit)
	api.Get("/toolkits/:id", s.handlers.GetToolkit)
	api.Delete("/toolkits/:id", s.handlers.DeleteToolkit)

	// Health & Readiness
	api.Get("/health", s.handlers.HealthCheck)
	api.Get("/ready", s.handlers.ReadinessProbe)
	api.Get("/alive", s.handlers.LivenessProbe)

	// Jobs (Async Operations)
	api.Post("/jobs", s.handlers.CreateJob)
	api.Get("/jobs", s.handlers.ListJobs)
	api.Get("/jobs/stats", s.handlers.GetJobStats)
	api.Get("/jobs/:id", s.handlers.GetJob)
	api.Delete("/jobs/:id", s.handlers.CancelJob)
	api.Get("/jobs/:id/stream", s.handlers.StreamJob)
	api.Post("/jobs/:id/wait", s.handlers.WaitForJob)

	// Metrics (Prometheus-compatible)
	s.app.Get("/metrics", s.handlers.PrometheusMetrics)

	log.Info().Msg("HTTP routes configured")
}

// SetJobManager sets the job manager for async operations
func (s *Server) SetJobManager(jm *jobs.JobManager) {
	s.handlers.SetJobManager(jm)
	log.Info().Msg("Job manager connected to HTTP server")
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.port)

	log.Info().
		Str("addr", addr).
		Msg("Starting HTTP API server")

	// Start in goroutine to not block
	go func() {
		if err := s.app.Listen(addr); err != nil {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	return nil
}

// Stop stops the HTTP server gracefully
func (s *Server) Stop(ctx context.Context) error {
	log.Info().Msg("Stopping HTTP server")

	if err := s.app.ShutdownWithContext(ctx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
		return err
	}

	log.Info().Msg("HTTP server stopped")
	return nil
}
