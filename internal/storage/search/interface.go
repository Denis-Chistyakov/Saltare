package search

import (
	"context"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Provider defines the interface for search engines (Typesense, Meilisearch, etc.)
type Provider interface {
	// HealthCheck checks if the search engine is available
	HealthCheck(ctx context.Context) error

	// CreateSchema creates the tools collection/index schema
	CreateSchema(ctx context.Context) error

	// IndexTool indexes a single tool
	IndexTool(ctx context.Context, tool *types.Tool, toolboxID, toolboxName, toolkitID string, tags []string) error

	// IndexToolbox indexes all tools from a toolbox
	IndexToolbox(ctx context.Context, toolbox *types.Toolbox, toolkitID string) error

	// DeleteTool removes a tool from the index
	DeleteTool(ctx context.Context, toolID string) error

	// DeleteToolbox removes all tools of a toolbox from the index
	DeleteToolbox(ctx context.Context, toolboxID string) error

	// Search searches for tools with keyword search
	Search(ctx context.Context, params SearchParams) (*SearchResult, error)

	// HybridSearch performs hybrid search (keyword + semantic) if supported
	// Falls back to regular Search if hybrid is not supported
	HybridSearch(ctx context.Context, params HybridSearchParams) (*SearchResult, error)

	// UpdateCallsCount updates the calls count for a tool
	UpdateCallsCount(ctx context.Context, toolID string, count int32) error

	// GetStats returns collection/index statistics
	GetStats(ctx context.Context) (map[string]interface{}, error)

	// Close closes the client connection
	Close() error

	// Name returns the provider name for logging
	Name() string

	// SupportsHybridSearch returns true if hybrid search is supported
	SupportsHybridSearch() bool
}

// SearchParams represents search parameters
type SearchParams struct {
	Query         string   // Search query
	Tags          []string // Filter by tags (AND logic)
	ToolboxID     string   // Filter by toolbox
	ToolkitID     string   // Filter by toolkit
	MinRating     float64  // Minimum rating filter
	SortBy        string   // Sort field: rating, calls_count, created_at
	SortOrder     string   // asc or desc
	Page          int      // Page number (1-based)
	PageSize      int      // Results per page
	IncludeFacets bool     // Include facet counts
}

// HybridSearchParams extends SearchParams with semantic search options
type HybridSearchParams struct {
	SearchParams
	SemanticRatio float64 // 0.0 = keyword only, 1.0 = semantic only
	Embedder      string  // Embedder name to use
	Vector        []float64 // Optional: pre-computed vector for search
}

// SearchResult represents search results
type SearchResult struct {
	Tools      []ToolDocument         `json:"tools"`
	Total      int                    `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
	TotalPages int                    `json:"total_pages"`
	Facets     map[string][]FacetItem `json:"facets,omitempty"`
	SearchTime int64                  `json:"search_time_ms"`
	Provider   string                 `json:"provider"` // "meilisearch" or "typesense"
	Hybrid     bool                   `json:"hybrid"`   // Was hybrid search used
}

// ToolDocument represents a tool document for search
type ToolDocument struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	ToolboxID   string   `json:"toolbox_id"`
	ToolboxName string   `json:"toolbox_name,omitempty"`
	ToolkitID   string   `json:"toolkit_id"`
	MCPServer   string   `json:"mcp_server,omitempty"`
	CallsCount  int32    `json:"calls_count,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at,omitempty"`
	// Semantic search fields
	SemanticScore float64 `json:"semantic_score,omitempty"` // Score from vector search
	KeywordScore  float64 `json:"keyword_score,omitempty"`  // Score from keyword search
}

// FacetItem represents a facet value with count
type FacetItem struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

