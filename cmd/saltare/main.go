package main

// Package main provides the main entry point for Saltare server.
import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"github.com/Denis-Chistyakov/Saltare/internal/analytics"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/execution/codemode"
	"github.com/Denis-Chistyakov/Saltare/internal/execution/directmode"
	"github.com/Denis-Chistyakov/Saltare/internal/gateway/cli"
	"github.com/Denis-Chistyakov/Saltare/internal/gateway/http"
	"github.com/Denis-Chistyakov/Saltare/internal/gateway/mcp"
	"github.com/Denis-Chistyakov/Saltare/internal/jobs"
	"github.com/Denis-Chistyakov/Saltare/internal/router/providers"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/badger"
	meili "github.com/Denis-Chistyakov/Saltare/internal/storage/meilisearch"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/typesense"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Parse CLI args and execute commands
	if len(os.Args) > 1 && os.Args[1] != "server" {
		// Execute CLI command (not server mode)
		if err := cli.Execute(); err != nil {
			log.Fatal().Err(err).Msg("CLI command failed")
		}
		return
	}

	// Server mode
	log.Info().Msg("Starting Saltare v1.0.0")

	// Load configuration
	config, err := loadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Set log level
	level, err := zerolog.ParseLevel(config.Observability.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Initialize BadgerDB
	dbPath := "./data/badger"
	if config.Storage.Badger.Path != "" {
		dbPath = config.Storage.Badger.Path
	}
	
	db, err := badger.NewDB(dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize BadgerDB")
	}
	
	log.Info().
		Str("path", dbPath).
		Interface("stats", db.GetStats()).
		Msg("BadgerDB initialized")

	// Initialize toolkit manager
	manager := toolkit.NewManager()
	log.Info().Msg("Toolkit manager initialized")

	// Connect storage to manager for persistence
	manager.SetStorage(db)
	
	// Load existing toolkits from BadgerDB (persistence)
	if err := manager.LoadFromStorage(context.Background()); err != nil {
		log.Debug().Err(err).Msg("No toolkits in storage (fresh start)")
	}

	// Initialize search provider based on configuration
	var searchProvider search.Provider
	searchProviderName := config.Storage.Search.Provider
	if searchProviderName == "" {
		// Fallback to typesense if no provider specified but typesense is enabled
		if config.Storage.Typesense.Enabled {
			searchProviderName = "typesense"
		}
	}

	switch searchProviderName {
	case "meilisearch":
		if config.Storage.Search.Meilisearch.Enabled {
			msClient, msErr := meili.NewClient(config.Storage.Search.Meilisearch)
			if msErr != nil {
				log.Warn().Err(msErr).Msg("Failed to initialize Meilisearch, search will be local only")
			} else {
				// Create schema if not exists
				if err := msClient.CreateSchema(context.Background()); err != nil {
					log.Warn().Err(err).Msg("Failed to create Meilisearch schema")
				}
				searchProvider = msClient
				log.Info().
					Str("provider", "meilisearch").
					Str("index", config.Storage.Search.Meilisearch.IndexName).
					Bool("hybrid_enabled", config.Storage.Search.Meilisearch.HybridSearch.Enabled).
					Msg("Meilisearch search provider initialized")
			}
		}
	case "typesense":
		fallthrough
	default:
		if config.Storage.Typesense.Enabled {
			tsClient, tsErr := typesense.NewClient(config.Storage.Typesense)
			if tsErr != nil {
				log.Warn().Err(tsErr).Msg("Failed to initialize Typesense, search will be local only")
			} else {
				// Create schema if not exists
				if err := tsClient.CreateSchema(context.Background()); err != nil {
					log.Warn().Err(err).Msg("Failed to create Typesense schema")
				}
				searchProvider = tsClient
				log.Info().
					Str("provider", "typesense").
					Str("collection", config.Storage.Typesense.Collection).
					Msg("Typesense search provider initialized")
			}
		}
	}

	// Connect search provider to toolkit manager for auto-indexing
	if searchProvider != nil {
		manager.SetSearchIndexer(searchProvider)
		log.Info().
			Str("provider", searchProvider.Name()).
			Msg("Search indexer connected to toolkit manager")
		
		// Re-index loaded toolkits
		for _, tk := range manager.ListToolkits() {
			for _, tb := range tk.Toolboxes {
				if err := searchProvider.IndexToolbox(context.Background(), tb, tk.ID); err != nil {
					log.Warn().Err(err).Str("toolbox", tb.Name).Msg("Failed to index toolbox")
				}
			}
		}
	}

	// Load toolkits from config (if any in config - optional, for backward compat)
	loader := toolkit.NewLoader(manager, config)
	if err := loader.Load(); err != nil {
		log.Debug().Err(err).Msg("No toolkits in config (OK - use API to register)")
	}

	// Display loaded stats
	stats := manager.GetStats()
	log.Info().
		Interface("stats", stats).
		Msg("Toolkits loaded")

	// Initialize analytics collector
	log.Info().Msg("Initializing analytics collector")
	collector := analytics.NewCollector(config.Analytics.Enabled, false)

	// Initialize semantic router with Cerebras LLM
	log.Info().Msg("Initializing semantic router with LLM fallback")
	
	// Override API keys from env if they contain placeholders
	primaryKeyFromConfig := config.LLM.Primary.APIKey
	fallbackKeyFromConfig := config.LLM.Fallback.APIKey
	
	if config.LLM.Primary.APIKey == "" || config.LLM.Primary.APIKey == "${CEREBRAS_API_KEY}" {
		config.LLM.Primary.APIKey = os.Getenv("CEREBRAS_API_KEY")
	}
	if config.LLM.Fallback.APIKey == "" || config.LLM.Fallback.APIKey == "${OPENROUTER_API_KEY}" {
		config.LLM.Fallback.APIKey = os.Getenv("OPENROUTER_API_KEY")
	}
	
	log.Info().
		Str("primary_config", primaryKeyFromConfig).
		Str("fallback_config", fallbackKeyFromConfig).
		Bool("primary_has_env", config.LLM.Primary.APIKey != "").
		Bool("fallback_has_env", config.LLM.Fallback.APIKey != "").
		Msg("LLM API keys resolved")
	
	// Primary LLM: Cerebras (always)
	primaryLLM := providers.NewCerebrasProvider(
		config.LLM.Primary.APIKey,
		config.LLM.Primary.Model,
		config.LLM.Primary.Endpoint,
	)
	log.Info().
		Str("provider", "cerebras").
		Str("model", config.LLM.Primary.Model).
		Msg("Primary LLM provider initialized")
	
	// Fallback LLM: OpenRouter (OpenAI-compatible) or local Ollama
	var fallbackLLM providers.LLMProvider
	
	// If OpenRouter API key is set -> use OpenRouter (OpenAI format)
	if config.LLM.Fallback.APIKey != "" {
		fallbackLLM = providers.NewCerebrasProvider(
			config.LLM.Fallback.APIKey,
			config.LLM.Fallback.Model,
			config.LLM.Fallback.Endpoint,
		)
		log.Info().
			Str("provider", "openrouter").
			Str("model", config.LLM.Fallback.Model).
			Msg("Fallback LLM: OpenRouter (OpenAI-compatible API)")
	} else {
		// Local Ollama (Ollama format)
		fallbackTimeout, _ := time.ParseDuration(config.LLM.Fallback.Timeout)
		if fallbackTimeout == 0 {
			fallbackTimeout = 30 * time.Second
		}
		fallbackLLM = providers.NewOllamaProvider(
			config.LLM.Fallback.Endpoint,
			config.LLM.Fallback.Model,
			fallbackTimeout,
		)
		log.Info().
			Str("provider", "ollama").
			Str("model", config.LLM.Fallback.Model).
			Msg("Fallback LLM: Local Ollama")
	}
	
	// Create provider with fallback
	llmProvider := providers.NewFallbackProvider(primaryLLM, fallbackLLM)
		
	router := semantic.NewRouterWithLLM(manager, llmProvider)
	log.Info().Msg("Semantic router initialized with Cerebras (primary) + fallback")

	// Initialize execution engine
	log.Info().Msg("Initializing execution engine")
	executorRegistry := execution.NewExecutorRegistry()

	// Register DirectMode executor
	directExecutor := directmode.NewDirectExecutor(30 * time.Second)
	executorRegistry.Register(execution.DirectMode, directExecutor)

	log.Info().
		Str("mode", string(execution.DirectMode)).
		Msg("DirectMode executor registered")

	// Register CodeMode executor (killer feature!)
	codeSandbox := codemode.NewSandbox(10, 30*time.Second, directExecutor)
	codeExecutor := codemode.NewCodeModeExecutor(codeSandbox)
	executorRegistry.Register(execution.CodeMode, codeExecutor)

	log.Info().
		Str("mode", string(execution.CodeMode)).
		Int("pool_size", 10).
		Msg("CodeMode executor registered")

	// Initialize MCP server
	mcpServer := mcp.NewServer(manager, executorRegistry, router)
	
	// Connect search provider to MCP for smart tool discovery
	if searchProvider != nil {
		mcpServer.SetSearchClient(searchProvider)
		log.Info().
			Str("provider", searchProvider.Name()).
			Msg("Search provider connected to MCP server for smart tool discovery")
	}

	// Initialize Job Manager for async operations
	log.Info().Msg("Initializing job manager for async operations")
	jobConfig := loadJobConfig()
	jobManager := jobs.NewJobManager(db.DB(), executorRegistry, manager, router, jobConfig)
	
	if err := jobManager.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start job manager")
	}
	
	// Connect search provider to JobManager for async smart calls
	if searchProvider != nil {
		jobManager.SetSearchClient(searchProvider)
	}
	
	// Connect JobManager to MCP and HTTP servers
	mcpServer.SetJobManager(jobManager)
	log.Info().
		Int("workers", jobConfig.NumWorkers).
		Int("queue_size", jobConfig.QueueSize).
		Msg("Job manager started")

	// Start MCP stdio transport (if enabled)
	if config.MCP.Stdio.Enabled {
		log.Info().Msg("MCP stdio transport enabled")
		// Note: stdio is started on demand when called by CLI
	}

	// Start MCP HTTP transport (if enabled)
	var mcpHTTP *mcp.HTTPTransport
	if config.MCP.HTTP.Enabled {
		mcpHTTP = mcp.NewHTTPTransport(mcpServer, config.MCP.HTTP.Port)
		if err := mcpHTTP.Start(); err != nil {
			log.Fatal().Err(err).Msg("Failed to start MCP HTTP transport")
		}
	}

	// Start HTTP API server
	httpServer := http.NewServer(manager, executorRegistry, router, collector, &config.Server)
	
	// Connect JobManager to HTTP server
	httpServer.SetJobManager(jobManager)
	
	if err := httpServer.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start HTTP server")
	}

	// Wait for interrupt signal
	log.Info().Msg("Saltare server is running")
	log.Info().Msgf("HTTP API: http://%s:%d", config.Server.Host, config.Server.Port)
	if config.MCP.HTTP.Enabled {
		log.Info().Msgf("MCP HTTP: http://localhost:%d/mcp", config.MCP.HTTP.Port)
	}
	log.Info().Msg("Press Ctrl+C to stop")

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Wait for signal
	<-sigChan
	log.Info().Msg("Shutdown signal received")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop HTTP server
	if err := httpServer.Stop(ctx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	// Stop MCP HTTP transport
	if mcpHTTP != nil {
		if err := mcpHTTP.Stop(); err != nil {
			log.Error().Err(err).Msg("MCP HTTP transport shutdown error")
		}
	}

	// Stop MCP server
	if err := mcpServer.Stop(); err != nil {
		log.Error().Err(err).Msg("MCP server shutdown error")
	}

	// Stop job manager
	if err := jobManager.Stop(); err != nil {
		log.Error().Err(err).Msg("Job manager shutdown error")
	}

	// Stop executors
	if err := executorRegistry.CloseAll(); err != nil {
		log.Error().Err(err).Msg("Executor shutdown error")
	}

	// Close BadgerDB
	if err := db.Close(); err != nil {
		log.Error().Err(err).Msg("BadgerDB shutdown error")
	}

	log.Info().Msg("Saltare server stopped")
}

// loadConfig loads configuration from file
func loadConfig() (*types.Config, error) {
	// Set default config file
	viper.SetConfigName("saltare")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/saltare")

	// Allow environment variables
	viper.SetEnvPrefix("SALTARE")
	viper.AutomaticEnv()

	// Read config
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	log.Info().Str("config", viper.ConfigFileUsed()).Msg("Configuration loaded")

	// Unmarshal config
	var config types.Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}

	return &config, nil
}

// loadJobConfig loads job configuration from viper with defaults
func loadJobConfig() *jobs.JobConfig {
	config := jobs.DefaultJobConfig()

	// Override with viper values if present
	if viper.IsSet("jobs.num_workers") {
		config.NumWorkers = viper.GetInt("jobs.num_workers")
	}
	if viper.IsSet("jobs.queue_size") {
		config.QueueSize = viper.GetInt("jobs.queue_size")
	}
	if viper.IsSet("jobs.job_timeout") {
		config.JobTimeout = viper.GetDuration("jobs.job_timeout")
	}
	if viper.IsSet("jobs.cleanup_interval") {
		config.CleanupInterval = viper.GetDuration("jobs.cleanup_interval")
	}
	if viper.IsSet("jobs.max_job_age") {
		config.MaxJobAge = viper.GetDuration("jobs.max_job_age")
	}
	if viper.IsSet("jobs.auto_delete_completed") {
		config.AutoDeleteCompleted = viper.GetBool("jobs.auto_delete_completed")
	}
	if viper.IsSet("jobs.keep_failed_jobs") {
		config.KeepFailedJobs = viper.GetBool("jobs.keep_failed_jobs")
	}

	log.Info().
		Int("workers", config.NumWorkers).
		Int("queue_size", config.QueueSize).
		Dur("job_timeout", config.JobTimeout).
		Bool("auto_delete", config.AutoDeleteCompleted).
		Bool("keep_failed", config.KeepFailedJobs).
		Msg("Job configuration loaded")

	return config
}
