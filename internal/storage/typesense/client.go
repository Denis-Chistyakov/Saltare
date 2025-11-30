package typesense

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/typesense/typesense-go/v2/typesense"
	"github.com/typesense/typesense-go/v2/typesense/api"
	"github.com/typesense/typesense-go/v2/typesense/api/pointer"
)

// Ensure Client implements search.Provider
var _ search.Provider = (*Client)(nil)

// Client represents a Typesense search client
type Client struct {
	client     *typesense.Client
	config     types.TypesenseConfig
	collection string
}

// NewClient creates a new Typesense client
func NewClient(cfg types.TypesenseConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("typesense is disabled")
	}

	if len(cfg.Nodes) == 0 {
		return nil, fmt.Errorf("no typesense nodes configured")
	}

	// Parse timeout
	timeout := 5 * time.Second
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err == nil {
			timeout = parsed
		}
	}

	// Default collection name
	collection := cfg.Collection
	if collection == "" {
		collection = "tools"
	}

	// Create client
	client := typesense.NewClient(
		typesense.WithServer(cfg.Nodes[0]),
		typesense.WithAPIKey(cfg.APIKey),
		typesense.WithConnectionTimeout(timeout),
	)

	c := &Client{
		client:     client,
		config:     cfg,
		collection: collection,
	}

	log.Info().
		Strs("nodes", cfg.Nodes).
		Str("collection", collection).
		Msg("Typesense client initialized")

	return c, nil
}

// HealthCheck checks if Typesense is available
func (c *Client) HealthCheck(ctx context.Context) error {
	health, err := c.client.Health(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("typesense health check failed: %w", err)
	}
	if !health {
		return fmt.Errorf("typesense is unhealthy")
	}
	return nil
}

// CreateSchema creates the tools collection schema
func (c *Client) CreateSchema(ctx context.Context) error {
	schema := &api.CollectionSchema{
		Name: c.collection,
		Fields: []api.Field{
			// Core fields
			{Name: "id", Type: "string"},
			{Name: "name", Type: "string", Sort: pointer.True()},
			{Name: "description", Type: "string", Optional: pointer.True()},

			// Searchable fields
			{Name: "tags", Type: "string[]", Facet: pointer.True(), Optional: pointer.True()},

			// Metadata
			{Name: "toolbox_id", Type: "string", Facet: pointer.True()},
			{Name: "toolbox_name", Type: "string", Optional: pointer.True()},
			{Name: "toolkit_id", Type: "string", Facet: pointer.True()},
			{Name: "mcp_server", Type: "string", Optional: pointer.True()},

			// Stats (for sorting)
			{Name: "calls_count", Type: "int32", Sort: pointer.True(), Optional: pointer.True()},
			{Name: "rating", Type: "float", Facet: pointer.True(), Sort: pointer.True(), Optional: pointer.True()},

			// Timestamps
			{Name: "created_at", Type: "int64", Sort: pointer.True()},
			{Name: "updated_at", Type: "int64", Sort: pointer.True(), Optional: pointer.True()},
		},
		DefaultSortingField: pointer.String("created_at"),
	}

	_, err := c.client.Collections().Create(ctx, schema)
	if err != nil {
		// Check if collection already exists
		if _, getErr := c.client.Collection(c.collection).Retrieve(ctx); getErr == nil {
			log.Debug().Str("collection", c.collection).Msg("Collection already exists")
			return nil
		}
		return fmt.Errorf("failed to create collection: %w", err)
	}

	log.Info().Str("collection", c.collection).Msg("Collection created")
	return nil
}

// ToolDocument represents a tool document for Typesense
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
}

// IndexTool indexes a single tool
func (c *Client) IndexTool(ctx context.Context, tool *types.Tool, toolboxID, toolboxName, toolkitID string, tags []string) error {
	doc := ToolDocument{
		ID:          tool.ID,
		Name:        tool.Name,
		Description: tool.Description,
		Tags:        tags,
		ToolboxID:   toolboxID,
		ToolboxName: toolboxName,
		ToolkitID:   toolkitID,
		MCPServer:   tool.MCPServer,
		CreatedAt:   tool.CreatedAt.Unix(),
	}

	_, err := c.client.Collection(c.collection).Documents().Upsert(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to index tool %s: %w", tool.ID, err)
	}

	log.Debug().Str("tool_id", tool.ID).Str("name", tool.Name).Msg("Tool indexed")
	return nil
}

// IndexToolbox indexes all tools from a toolbox
func (c *Client) IndexToolbox(ctx context.Context, toolbox *types.Toolbox, toolkitID string) error {
	for _, tool := range toolbox.Tools {
		if err := c.IndexTool(ctx, tool, toolbox.ID, toolbox.Name, toolkitID, toolbox.Tags); err != nil {
			return err
		}
	}

	log.Info().
		Str("toolbox_id", toolbox.ID).
		Int("tools_count", len(toolbox.Tools)).
		Msg("Toolbox indexed")

	return nil
}

// DeleteTool removes a tool from the index
func (c *Client) DeleteTool(ctx context.Context, toolID string) error {
	_, err := c.client.Collection(c.collection).Document(toolID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete tool %s: %w", toolID, err)
	}

	log.Debug().Str("tool_id", toolID).Msg("Tool deleted from index")
	return nil
}

// DeleteToolbox removes all tools of a toolbox from the index
func (c *Client) DeleteToolbox(ctx context.Context, toolboxID string) error {
	filter := fmt.Sprintf("toolbox_id:=%s", toolboxID)
	_, err := c.client.Collection(c.collection).Documents().Delete(ctx, &api.DeleteDocumentsParams{
		FilterBy: &filter,
	})
	if err != nil {
		return fmt.Errorf("failed to delete toolbox tools %s: %w", toolboxID, err)
	}

	log.Info().Str("toolbox_id", toolboxID).Msg("Toolbox deleted from index")
	return nil
}

// InternalSearchParams represents internal search parameters (for backwards compatibility)
type InternalSearchParams struct {
	Query       string   // Search query
	Tags        []string // Filter by tags (AND logic)
	ToolboxID   string   // Filter by toolbox
	ToolkitID   string   // Filter by toolkit
	MinRating   float64  // Minimum rating filter
	SortBy      string   // Sort field: rating, calls_count, created_at
	SortOrder   string   // asc or desc
	Page        int      // Page number (1-based)
	PageSize    int      // Results per page
	IncludeFacets bool   // Include facet counts
}

// InternalSearchResult represents internal search results (for backwards compatibility)
type InternalSearchResult struct {
	Tools      []ToolDocument         `json:"tools"`
	Total      int                    `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
	TotalPages int                    `json:"total_pages"`
	Facets     map[string][]InternalFacetItem `json:"facets,omitempty"`
	SearchTime int64                  `json:"search_time_ms"`
}

// InternalFacetItem represents a facet value with count (for backwards compatibility)
type InternalFacetItem struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Deprecated type aliases for backwards compatibility
type SearchParams = InternalSearchParams
type SearchResult = InternalSearchResult
type FacetItem = InternalFacetItem

// Search implements the search.Provider interface
func (c *Client) Search(ctx context.Context, params search.SearchParams) (*search.SearchResult, error) {
	internalParams := InternalSearchParams{
		Query:         params.Query,
		Tags:          params.Tags,
		ToolboxID:     params.ToolboxID,
		ToolkitID:     params.ToolkitID,
		MinRating:     params.MinRating,
		SortBy:        params.SortBy,
		SortOrder:     params.SortOrder,
		Page:          params.Page,
		PageSize:      params.PageSize,
		IncludeFacets: params.IncludeFacets,
	}

	result, err := c.searchInternal(ctx, internalParams)
	if err != nil {
		return nil, err
	}

	// Convert to interface result
	tools := make([]search.ToolDocument, len(result.Tools))
	for i, t := range result.Tools {
		tools[i] = search.ToolDocument{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			Tags:        t.Tags,
			ToolboxID:   t.ToolboxID,
			ToolboxName: t.ToolboxName,
			ToolkitID:   t.ToolkitID,
			MCPServer:   t.MCPServer,
			CallsCount:  t.CallsCount,
			Rating:      t.Rating,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		}
	}

	facets := make(map[string][]search.FacetItem)
	for k, v := range result.Facets {
		items := make([]search.FacetItem, len(v))
		for i, item := range v {
			items[i] = search.FacetItem{
				Value: item.Value,
				Count: item.Count,
			}
		}
		facets[k] = items
	}

	return &search.SearchResult{
		Tools:      tools,
		Total:      result.Total,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		Facets:     facets,
		SearchTime: result.SearchTime,
		Provider:   "typesense",
		Hybrid:     false,
	}, nil
}

// searchInternal performs the actual search (internal implementation)
func (c *Client) searchInternal(ctx context.Context, params InternalSearchParams) (*InternalSearchResult, error) {
	// Defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	if params.Query == "" {
		params.Query = "*" // Match all
	}

	// Build filter
	var filters []string
	if params.ToolboxID != "" {
		filters = append(filters, fmt.Sprintf("toolbox_id:=%s", params.ToolboxID))
	}
	if params.ToolkitID != "" {
		filters = append(filters, fmt.Sprintf("toolkit_id:=%s", params.ToolkitID))
	}
	if params.MinRating > 0 {
		filters = append(filters, fmt.Sprintf("rating:>=%f", params.MinRating))
	}
	if len(params.Tags) > 0 {
		for _, tag := range params.Tags {
			filters = append(filters, fmt.Sprintf("tags:=%s", tag))
		}
	}

	filterBy := ""
	if len(filters) > 0 {
		filterBy = filters[0]
		for _, f := range filters[1:] {
			filterBy += " && " + f
		}
	}

	// Build sort
	sortBy := "created_at:desc"
	if params.SortBy != "" {
		order := "desc"
		if params.SortOrder == "asc" {
			order = "asc"
		}
		sortBy = fmt.Sprintf("%s:%s", params.SortBy, order)
	}

	// Facets
	facetBy := ""
	if params.IncludeFacets {
		facetBy = "tags,toolbox_id,toolkit_id"
	}

	// Number of typos
	numTypos := c.config.NumTypos
	if numTypos == 0 {
		numTypos = 2
	}

	// Build search params
	queryBy := "name,description,tags"
	numTyposStr := fmt.Sprintf("%d", numTypos)
	searchParams := &api.SearchCollectionParams{
		Q:        &params.Query,
		QueryBy:  &queryBy,
		FilterBy: pointer.String(filterBy),
		SortBy:   pointer.String(sortBy),
		Page:     pointer.Int(params.Page),
		PerPage:  pointer.Int(params.PageSize),
		NumTypos: &numTyposStr,
	}
	if facetBy != "" {
		searchParams.FacetBy = pointer.String(facetBy)
	}

	start := time.Now()
	result, err := c.client.Collection(c.collection).Documents().Search(ctx, searchParams)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	searchTime := time.Since(start).Milliseconds()

	// Parse results
	tools := make([]ToolDocument, 0)
	if result.Hits != nil {
		for _, hit := range *result.Hits {
			if hit.Document != nil {
				doc := *hit.Document
				tool := ToolDocument{
					ID:          getString(doc, "id"),
					Name:        getString(doc, "name"),
					Description: getString(doc, "description"),
					ToolboxID:   getString(doc, "toolbox_id"),
					ToolboxName: getString(doc, "toolbox_name"),
					ToolkitID:   getString(doc, "toolkit_id"),
					MCPServer:   getString(doc, "mcp_server"),
					Tags:        getStringArray(doc, "tags"),
					CallsCount:  int32(getInt(doc, "calls_count")),
					Rating:      getFloat(doc, "rating"),
					CreatedAt:   getInt(doc, "created_at"),
				}
				tools = append(tools, tool)
			}
		}
	}

	total := 0
	if result.Found != nil {
		total = *result.Found
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize

	// Parse facets
	facets := make(map[string][]InternalFacetItem)
	if result.FacetCounts != nil {
		for _, fc := range *result.FacetCounts {
			if fc.FieldName != nil && fc.Counts != nil {
				items := make([]InternalFacetItem, 0)
				for _, count := range *fc.Counts {
					if count.Value != nil && count.Count != nil {
						items = append(items, InternalFacetItem{
							Value: *count.Value,
							Count: *count.Count,
						})
					}
				}
				facets[*fc.FieldName] = items
			}
		}
	}

	log.Debug().
		Str("query", params.Query).
		Int("total", total).
		Int64("search_time_ms", searchTime).
		Msg("Search completed")

	return &InternalSearchResult{
		Tools:      tools,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
		Facets:     facets,
		SearchTime: searchTime,
	}, nil
}

// UpdateCallsCount updates the calls count for a tool
func (c *Client) UpdateCallsCount(ctx context.Context, toolID string, count int32) error {
	update := map[string]interface{}{
		"calls_count": count,
		"updated_at":  time.Now().Unix(),
	}

	_, err := c.client.Collection(c.collection).Document(toolID).Update(ctx, update)
	if err != nil {
		return fmt.Errorf("failed to update calls count: %w", err)
	}

	return nil
}

// GetStats returns collection statistics
func (c *Client) GetStats(ctx context.Context) (map[string]interface{}, error) {
	collection, err := c.client.Collection(c.collection).Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection stats: %w", err)
	}

	stats := map[string]interface{}{
		"collection": c.collection,
	}
	if collection.NumDocuments != nil {
		stats["num_documents"] = *collection.NumDocuments
	}

	return stats, nil
}

// Close closes the client (no-op for Typesense, but good practice)
func (c *Client) Close() error {
	log.Info().Msg("Typesense client closed")
	return nil
}

// Name returns the provider name
func (c *Client) Name() string {
	return "typesense"
}

// SupportsHybridSearch returns false for Typesense (no native hybrid search)
func (c *Client) SupportsHybridSearch() bool {
	return false
}

// HybridSearch falls back to regular search for Typesense (no native hybrid support)
func (c *Client) HybridSearch(ctx context.Context, params search.HybridSearchParams) (*search.SearchResult, error) {
	// Typesense doesn't support hybrid search, fall back to regular search
	return c.Search(ctx, params.SearchParams)
}

// Helper functions
func getString(doc map[string]interface{}, key string) string {
	if v, ok := doc[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringArray(doc map[string]interface{}, key string) []string {
	if v, ok := doc[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

func getInt(doc map[string]interface{}, key string) int64 {
	if v, ok := doc[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

func getFloat(doc map[string]interface{}, key string) float64 {
	if v, ok := doc[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}
