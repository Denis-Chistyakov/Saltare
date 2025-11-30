package types

// Package types provides shared type definitions for Saltare.
// Contains core data structures used across the application.
import (
	"time"
)

// Core Types

// Toolkit represents a physical node/instance
type Toolkit struct {
	ID        string     `json:"id" validate:"required"`
	Name      string     `json:"name" validate:"required"`
	OwnerID   string     `json:"owner_id"`
	Status    string     `json:"status" validate:"oneof=active inactive"`
	Toolboxes []*Toolbox `json:"toolboxes"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Toolbox represents a logical grouping of tools
type Toolbox struct {
	ID          string                 `json:"id" validate:"required"`
	Name        string                 `json:"name" validate:"required"`
	Version     string                 `json:"version" validate:"required"`
	Tags        []string               `json:"tags"`
	Description string                 `json:"description"`
	Rating      float64                `json:"rating" validate:"gte=0,lte=5"`
	Tools       []*Tool                `json:"tools"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Tool represents an executable unit
type Tool struct {
	ID          string                 `json:"id" validate:"required"`
	Name        string                 `json:"name" validate:"required"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema" validate:"required"`
	MCPServer   string                 `json:"mcp_server" validate:"required"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// Intent represents parsed user intent from LLM
type Intent struct {
	Action     string                 `json:"action"`
	Domain     string                 `json:"domain"`
	Entity     string                 `json:"entity"`
	Filters    map[string]interface{} `json:"filters"`
	Parameters map[string]interface{} `json:"parameters"` // Extracted tool parameters from natural language
	Confidence float64                `json:"confidence"`
	RawQuery   string                 `json:"raw_query"`
}

// CallEvent represents an analytics event
type CallEvent struct {
	ID         string                 `json:"id"`
	ToolID     string                 `json:"tool_id"`
	UserID     string                 `json:"user_id,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Duration   time.Duration          `json:"duration"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Request/Response Types

// ExecuteToolRequest represents a tool execution request
type ExecuteToolRequest struct {
	ToolID string                 `json:"tool_id" validate:"required"`
	Args   map[string]interface{} `json:"args"`
}

// ExecuteToolResponse represents a tool execution response
type ExecuteToolResponse struct {
	Success    bool        `json:"success"`
	Result     interface{} `json:"result,omitempty"`
	Error      string      `json:"error,omitempty"`
	Duration   int64       `json:"duration_ms"`
	TokensUsed int         `json:"tokens_used,omitempty"`
}

// ListToolsRequest represents a tool list request
type ListToolsRequest struct {
	Tags     []string               `json:"tags,omitempty"`
	Query    string                 `json:"query,omitempty"`
	Filters  map[string]interface{} `json:"filters,omitempty"`
	Page     int                    `json:"page" validate:"gte=1"`
	PageSize int                    `json:"page_size" validate:"gte=1,lte=100"`
}

// ListToolsResponse represents a tool list response
type ListToolsResponse struct {
	Tools      []*Tool `json:"tools"`
	Total      int     `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	TotalPages int     `json:"total_pages"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string                 `json:"error"`
	Code      string                 `json:"code,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// MCP Types (JSON-RPC 2.0)

// MCPRequest represents a JSON-RPC 2.0 request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// MCPResponse represents a JSON-RPC 2.0 response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC 2.0 error
type MCPError struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// MCP Error Codes (JSON-RPC 2.0 standard + custom)
const (
	MCPErrorParseError     = -32700
	MCPErrorInvalidRequest = -32600
	MCPErrorMethodNotFound = -32601
	MCPErrorInvalidParams  = -32602
	MCPErrorInternalError  = -32603
	MCPErrorServerError    = -32000
)

// MCPInitializeResult represents the initialize method result
type MCPInitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools struct {
			ListChanged bool `json:"listChanged,omitempty"`
		} `json:"tools,omitempty"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// MCPToolInfo represents tool information in MCP format
type MCPToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Config Types

// Config represents the entire application configuration
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	MCP           MCPConfig           `yaml:"mcp"`
	LLM           LLMConfig           `yaml:"llm"`
	Storage       StorageConfig       `yaml:"storage"`
	Analytics     AnalyticsConfig     `yaml:"analytics"`
	Observability ObservabilityConfig `yaml:"observability"`
	Toolkits      []ToolkitConfig     `yaml:"toolkits"`
}

// ServerConfig represents HTTP server configuration
type ServerConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	MaxBodySize  string `yaml:"max_body_size"`
}

// MCPConfig represents MCP server configuration
type MCPConfig struct {
	Stdio struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"stdio"`
	HTTP struct {
		Enabled    bool `yaml:"enabled"`
		Port       int  `yaml:"port"`
		SSEEnabled bool `yaml:"sse_enabled"`
	} `yaml:"http"`
}

// LLMConfig represents LLM provider configuration (Cerebras only)
type LLMConfig struct {
	Primary  LLMProviderConfig `yaml:"primary" mapstructure:"primary"`
	Fallback LLMProviderConfig `yaml:"fallback" mapstructure:"fallback"`
}

type LLMProviderConfig struct {
	Provider string `yaml:"provider" mapstructure:"provider"`
	Endpoint string `yaml:"endpoint" mapstructure:"endpoint"`
	APIKey   string `yaml:"api_key" mapstructure:"api_key"`
	Model    string `yaml:"model" mapstructure:"model"`
	Timeout  string `yaml:"timeout" mapstructure:"timeout"`
}

// CerebrasConfig represents Cerebras AI configuration
type CerebrasConfig struct {
	APIKey      string        `yaml:"api_key"`
	Model       string        `yaml:"model"`
	Endpoint    string        `yaml:"endpoint"`
	Temperature float64       `yaml:"temperature"`
	MaxTokens   int           `yaml:"max_tokens"`
	Timeout     time.Duration `yaml:"timeout"`
	Retries     int           `yaml:"retries"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type      string          `yaml:"type" mapstructure:"type"`
	Badger    BadgerConfig    `yaml:"badger" mapstructure:"badger"`
	Typesense TypesenseConfig `yaml:"typesense" mapstructure:"typesense"`
	Search    SearchConfig    `yaml:"search" mapstructure:"search"`
}

// SearchConfig represents search engine configuration
type SearchConfig struct {
	Provider    string            `yaml:"provider" mapstructure:"provider"` // "meilisearch" or "typesense"
	Meilisearch MeilisearchConfig `yaml:"meilisearch" mapstructure:"meilisearch"`
}

// MeilisearchConfig represents Meilisearch search configuration
type MeilisearchConfig struct {
	Enabled      bool           `yaml:"enabled" mapstructure:"enabled"`
	Host         string         `yaml:"host" mapstructure:"host"`                   // "http://localhost:7700"
	APIKey       string         `yaml:"api_key" mapstructure:"api_key"`             // Master key
	IndexName    string         `yaml:"index_name" mapstructure:"index_name"`       // Default: "tools"
	Timeout      string         `yaml:"timeout" mapstructure:"timeout"`             // Default: "5s"
	Embedder     EmbedderConfig `yaml:"embedder" mapstructure:"embedder"`           // Embedder configuration
	HybridSearch HybridConfig   `yaml:"hybrid_search" mapstructure:"hybrid_search"` // Hybrid search settings
}

// EmbedderConfig represents embedder configuration for semantic search
type EmbedderConfig struct {
	Source           string `yaml:"source" mapstructure:"source"`                       // "rest", "openAi", "huggingFace", "ollama", "userProvided"
	URL              string `yaml:"url" mapstructure:"url"`                             // For REST: "https://openrouter.ai/api/v1/embeddings"
	APIKey           string `yaml:"api_key" mapstructure:"api_key"`                     // API key for the embedder
	Model            string `yaml:"model" mapstructure:"model"`                         // Model name: "openai/text-embedding-3-small"
	Dimensions       int    `yaml:"dimensions" mapstructure:"dimensions"`               // Vector dimensions: 1536
	DocumentTemplate string `yaml:"document_template" mapstructure:"document_template"` // Template: "{{doc.name}} {{doc.description}}"
}

// HybridConfig represents hybrid search configuration
type HybridConfig struct {
	Enabled       bool    `yaml:"enabled" mapstructure:"enabled"`               // Enable hybrid search
	SemanticRatio float64 `yaml:"semantic_ratio" mapstructure:"semantic_ratio"` // 0.0 = keyword only, 1.0 = semantic only, 0.5 = balanced
	EmbedderName  string  `yaml:"embedder_name" mapstructure:"embedder_name"`   // Name of embedder to use: "default"
}

// TypesenseConfig represents Typesense search configuration
type TypesenseConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Nodes      []string `yaml:"nodes"` // ["http://localhost:8108"]
	APIKey     string   `yaml:"api_key"`
	Collection string   `yaml:"collection"` // Default: "tools"
	NumTypos   int      `yaml:"num_typos"`  // Default: 2
	Timeout    string   `yaml:"timeout"`    // Default: "5s"
}

// BadgerConfig represents BadgerDB configuration
type BadgerConfig struct {
	Path         string `yaml:"path"`
	Compression  string `yaml:"compression"`
	MaxTableSize string `yaml:"max_table_size"`
}

// AnalyticsConfig represents analytics configuration
type AnalyticsConfig struct {
	Enabled             bool   `yaml:"enabled"`
	RetentionDays       int    `yaml:"retention_days"`
	AggregationInterval string `yaml:"aggregation_interval"`
}

// ObservabilityConfig represents observability configuration
type ObservabilityConfig struct {
	Metrics MetricsConfig `yaml:"metrics"`
	Tracing TracingConfig `yaml:"tracing"`
	Logging LoggingConfig `yaml:"logging"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// TracingConfig represents tracing configuration
type TracingConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// ToolkitConfig represents toolkit configuration from YAML
type ToolkitConfig struct {
	Name      string          `yaml:"name"`
	Toolboxes []ToolboxConfig `yaml:"toolboxes"`
}

// ToolboxConfig represents toolbox configuration from YAML
type ToolboxConfig struct {
	Name        string       `yaml:"name"`
	Version     string       `yaml:"version"`
	Tags        []string     `yaml:"tags"`
	Description string       `yaml:"description"`
	Tools       []ToolConfig `yaml:"tools"`
}

// ToolConfig represents tool configuration from YAML
type ToolConfig struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	InputSchema map[string]interface{} `yaml:"input_schema"`
	MCPServer   string                 `yaml:"mcp_server"`
}

// Analytics Types
// CallEvent is defined above (line 135)

// DailyStats represents aggregated daily statistics
type DailyStats struct {
	Date         time.Time `json:"date"`
	TotalCalls   int64     `json:"total_calls"`
	SuccessCalls int64     `json:"success_calls"`
	FailedCalls  int64     `json:"failed_calls"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	AvgLatencyMs float64   `json:"avg_latency_ms"`
}

// ToolboxStats represents toolbox-specific statistics
type ToolboxStats struct {
	ToolboxID    string  `json:"toolbox_id"`
	ToolboxName  string  `json:"toolbox_name"`
	TotalCalls   int64   `json:"total_calls"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}
