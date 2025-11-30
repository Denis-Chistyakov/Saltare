package toolkit

// Package toolkit provides configuration loading for toolkits from YAML config files.
// Loads toolkits, toolboxes, and tools from saltare.yaml and registers them with the manager.

import (
	"fmt"
	"net/url"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Loader loads toolkits from configuration
type Loader struct {
	manager *Manager
	config  *types.Config
}

// NewLoader creates a new config loader
func NewLoader(manager *Manager, config *types.Config) *Loader {
	return &Loader{
		manager: manager,
		config:  config,
	}
}

// Load loads all toolkits from configuration
func (l *Loader) Load() error {
	for _, tkConfig := range l.config.Toolkits {
		toolkit, err := l.convertToToolkit(tkConfig)
		if err != nil {
			log.Error().Err(err).Str("toolkit", tkConfig.Name).Msg("Failed to convert toolkit config")
			continue
		}

		if err := l.manager.RegisterToolkit(toolkit); err != nil {
			log.Error().Err(err).Str("toolkit", toolkit.Name).Msg("Failed to register toolkit")
			continue
		}

		log.Info().
			Str("toolkit", toolkit.Name).
			Int("toolboxes", len(toolkit.Toolboxes)).
			Msg("Loaded toolkit from config")
	}

	return nil
}

// convertToToolkit converts a ToolkitConfig to a Toolkit
func (l *Loader) convertToToolkit(cfg types.ToolkitConfig) (*types.Toolkit, error) {
	toolkit := &types.Toolkit{
		Name:      cfg.Name,
		Status:    "active",
		Toolboxes: make([]*types.Toolbox, 0, len(cfg.Toolboxes)),
	}

	for _, tbConfig := range cfg.Toolboxes {
		toolbox, err := l.convertToToolbox(tbConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert toolbox %s: %w", tbConfig.Name, err)
		}
		toolkit.Toolboxes = append(toolkit.Toolboxes, toolbox)
	}

	return toolkit, nil
}

// convertToToolbox converts a ToolboxConfig to a Toolbox
func (l *Loader) convertToToolbox(cfg types.ToolboxConfig) (*types.Toolbox, error) {
	// Validate required fields
	if cfg.Name == "" {
		return nil, fmt.Errorf("toolbox name is required")
	}
	if cfg.Version == "" {
		return nil, fmt.Errorf("toolbox version is required")
	}

	toolbox := &types.Toolbox{
		Name:        cfg.Name,
		Version:     cfg.Version,
		Tags:        cfg.Tags,
		Description: cfg.Description,
		Rating:      0.0,
		Tools:       make([]*types.Tool, 0, len(cfg.Tools)),
		Metadata:    make(map[string]interface{}),
	}

	for _, toolConfig := range cfg.Tools {
		tool, err := l.convertToTool(toolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool %s: %w", toolConfig.Name, err)
		}
		toolbox.Tools = append(toolbox.Tools, tool)
	}

	return toolbox, nil
}

// convertToTool converts a ToolConfig to a Tool
func (l *Loader) convertToTool(cfg types.ToolConfig) (*types.Tool, error) {
	// Validate required fields
	if cfg.Name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	if cfg.MCPServer == "" {
		return nil, fmt.Errorf("tool mcp_server is required")
	}

	// Validate MCP server URL
	if err := validateURL(cfg.MCPServer); err != nil {
		return nil, fmt.Errorf("invalid mcp_server URL %s: %w", cfg.MCPServer, err)
	}

	// Validate input schema
	if cfg.InputSchema == nil {
		cfg.InputSchema = make(map[string]interface{})
	}

	tool := &types.Tool{
		Name:        cfg.Name,
		Description: cfg.Description,
		InputSchema: cfg.InputSchema,
		MCPServer:   cfg.MCPServer,
		Timeout:     0, // Use default
	}

	return tool, nil
}

// validateURL validates a URL string
func validateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL is empty")
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	if parsed.Scheme == "" {
		return fmt.Errorf("URL scheme is missing")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got: %s", parsed.Scheme)
	}

	return nil
}

// Reload reloads toolkits from configuration
func (l *Loader) Reload() error {
	// For now, just clear and reload all
	log.Info().Msg("Reloading toolkits from config")

	return l.Load()
}
