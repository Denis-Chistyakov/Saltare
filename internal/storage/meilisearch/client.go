package meilisearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/rs/zerolog/log"

	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Client represents a Meilisearch search client
type Client struct {
	client    meilisearch.ServiceManager
	config    types.MeilisearchConfig
	indexName string
}

// Ensure Client implements search.Provider
var _ search.Provider = (*Client)(nil)

// NewClient creates a new Meilisearch client
func NewClient(cfg types.MeilisearchConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("meilisearch is disabled")
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("meilisearch host is not configured")
	}

	// Default index name
	indexName := cfg.IndexName
	if indexName == "" {
		indexName = "tools"
	}

	// Create client with options
	client := meilisearch.New(
		cfg.Host,
		meilisearch.WithAPIKey(cfg.APIKey),
	)

	c := &Client{
		client:    client,
		config:    cfg,
		indexName: indexName,
	}

	log.Info().
		Str("host", cfg.Host).
		Str("index", indexName).
		Bool("hybrid_enabled", cfg.HybridSearch.Enabled).
		Msg("Meilisearch client initialized")

	return c, nil
}

// Name returns the provider name
func (c *Client) Name() string {
	return "meilisearch"
}

// SupportsHybridSearch returns true if hybrid search is configured
func (c *Client) SupportsHybridSearch() bool {
	return c.config.HybridSearch.Enabled && c.config.Embedder.Source != ""
}

// HealthCheck checks if Meilisearch is available
func (c *Client) HealthCheck(ctx context.Context) error {
	health, err := c.client.Health()
	if err != nil {
		return fmt.Errorf("meilisearch health check failed: %w", err)
	}
	if health.Status != "available" {
		return fmt.Errorf("meilisearch is unhealthy: %s", health.Status)
	}
	return nil
}

// CreateSchema creates the tools index schema with embedders
func (c *Client) CreateSchema(ctx context.Context) error {
	// Create index if not exists
	_, err := c.client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        c.indexName,
		PrimaryKey: "id",
	})
	if err != nil {
		// Check if index already exists (not a real error)
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create index: %w", err)
		}
		log.Debug().Str("index", c.indexName).Msg("Index already exists")
	}

	index := c.client.Index(c.indexName)

	// Configure searchable attributes (takes *[]string)
	searchableAttrs := []string{"name", "description", "tags"}
	_, err = index.UpdateSearchableAttributes(&searchableAttrs)
	if err != nil {
		return fmt.Errorf("failed to update searchable attributes: %w", err)
	}

	// Configure filterable attributes (takes *[]interface{})
	filterableAttrs := toInterfaceSlice("toolbox_id", "toolkit_id", "tags", "rating", "mcp_server")
	_, err = index.UpdateFilterableAttributes(&filterableAttrs)
	if err != nil {
		return fmt.Errorf("failed to update filterable attributes: %w", err)
	}

	// Configure sortable attributes (takes *[]string)
	sortableAttrs := []string{"created_at", "updated_at", "calls_count", "rating", "name"}
	_, err = index.UpdateSortableAttributes(&sortableAttrs)
	if err != nil {
		return fmt.Errorf("failed to update sortable attributes: %w", err)
	}

	// Configure embedders for hybrid search if enabled
	if c.config.Embedder.Source != "" {
		if err := c.configureEmbedders(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to configure embedders, hybrid search will not be available")
		}
	}

	log.Info().Str("index", c.indexName).Msg("Meilisearch schema created/updated")
	return nil
}

// configureEmbedders sets up the embedder configuration for hybrid search
func (c *Client) configureEmbedders(ctx context.Context) error {
	embedderName := c.config.HybridSearch.EmbedderName
	if embedderName == "" {
		embedderName = "default"
	}

	documentTemplate := c.config.Embedder.DocumentTemplate
	if documentTemplate == "" {
		documentTemplate = "{{doc.name}} {{doc.description}}"
	}

	var embedderConfig map[string]meilisearch.Embedder

	switch c.config.Embedder.Source {
	case "rest":
		// REST embedder for OpenRouter or any REST API
		embedderConfig = c.buildRESTEmbedderConfig(embedderName, documentTemplate)
	case "openAi":
		embedderConfig = map[string]meilisearch.Embedder{
			embedderName: {
				Source:           "openAi",
				APIKey:           c.config.Embedder.APIKey,
				Model:            c.config.Embedder.Model,
				DocumentTemplate: documentTemplate,
			},
		}
	case "huggingFace":
		embedderConfig = map[string]meilisearch.Embedder{
			embedderName: {
				Source:           "huggingFace",
				Model:            c.config.Embedder.Model,
				DocumentTemplate: documentTemplate,
			},
		}
	case "ollama":
		embedderConfig = map[string]meilisearch.Embedder{
			embedderName: {
				Source:           "ollama",
				URL:              c.config.Embedder.URL,
				Model:            c.config.Embedder.Model,
				DocumentTemplate: documentTemplate,
			},
		}
	case "userProvided":
		dimensions := c.config.Embedder.Dimensions
		if dimensions == 0 {
			dimensions = 1536
		}
		embedderConfig = map[string]meilisearch.Embedder{
			embedderName: {
				Source:     "userProvided",
				Dimensions: dimensions,
			},
		}
	default:
		return fmt.Errorf("unknown embedder source: %s", c.config.Embedder.Source)
	}

	index := c.client.Index(c.indexName)
	_, err := index.UpdateEmbedders(embedderConfig)
	if err != nil {
		return fmt.Errorf("failed to update embedders: %w", err)
	}

	log.Info().
		Str("embedder", embedderName).
		Str("source", c.config.Embedder.Source).
		Msg("Embedders configured for hybrid search")

	return nil
}

// buildRESTEmbedderConfig builds REST embedder config for OpenRouter-compatible APIs
func (c *Client) buildRESTEmbedderConfig(embedderName, documentTemplate string) map[string]meilisearch.Embedder {
	// OpenRouter/OpenAI compatible format
	return map[string]meilisearch.Embedder{
		embedderName: {
			Source: "rest",
			URL:    c.config.Embedder.URL,
			APIKey: c.config.Embedder.APIKey,
			Request: map[string]interface{}{
				"model": c.config.Embedder.Model,
				"input": []string{"{{text}}", "{{..}}"},
			},
			Response: map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{
						"embedding": "{{embedding}}",
					},
					"{{..}}",
				},
			},
			DocumentTemplate: documentTemplate,
		},
	}
}

// IndexTool indexes a single tool
func (c *Client) IndexTool(ctx context.Context, tool *types.Tool, toolboxID, toolboxName, toolkitID string, tags []string) error {
	doc := map[string]interface{}{
		"id":           tool.ID,
		"name":         tool.Name,
		"description":  tool.Description,
		"tags":         tags,
		"toolbox_id":   toolboxID,
		"toolbox_name": toolboxName,
		"toolkit_id":   toolkitID,
		"mcp_server":   tool.MCPServer,
		"calls_count":  0,
		"rating":       0.0,
		"created_at":   tool.CreatedAt.Unix(),
		"updated_at":   time.Now().Unix(),
	}

	index := c.client.Index(c.indexName)
	primaryKey := "id"
	_, err := index.AddDocuments([]map[string]interface{}{doc}, &primaryKey)
	if err != nil {
		return fmt.Errorf("failed to index tool %s: %w", tool.ID, err)
	}

	log.Debug().Str("tool_id", tool.ID).Str("name", tool.Name).Msg("Tool indexed in Meilisearch")
	return nil
}

// IndexToolbox indexes all tools from a toolbox
func (c *Client) IndexToolbox(ctx context.Context, toolbox *types.Toolbox, toolkitID string) error {
	if len(toolbox.Tools) == 0 {
		return nil
	}

	docs := make([]map[string]interface{}, 0, len(toolbox.Tools))
	for _, tool := range toolbox.Tools {
		doc := map[string]interface{}{
			"id":           tool.ID,
			"name":         tool.Name,
			"description":  tool.Description,
			"tags":         toolbox.Tags,
			"toolbox_id":   toolbox.ID,
			"toolbox_name": toolbox.Name,
			"toolkit_id":   toolkitID,
			"mcp_server":   tool.MCPServer,
			"calls_count":  0,
			"rating":       toolbox.Rating,
			"created_at":   tool.CreatedAt.Unix(),
			"updated_at":   time.Now().Unix(),
		}
		docs = append(docs, doc)
	}

	index := c.client.Index(c.indexName)
	primaryKey := "id"
	_, err := index.AddDocuments(docs, &primaryKey)
	if err != nil {
		return fmt.Errorf("failed to index toolbox %s: %w", toolbox.ID, err)
	}

	log.Info().
		Str("toolbox_id", toolbox.ID).
		Int("tools_count", len(toolbox.Tools)).
		Msg("Toolbox indexed in Meilisearch")

	return nil
}

// DeleteTool removes a tool from the index
func (c *Client) DeleteTool(ctx context.Context, toolID string) error {
	index := c.client.Index(c.indexName)
	_, err := index.DeleteDocument(toolID)
	if err != nil {
		return fmt.Errorf("failed to delete tool %s: %w", toolID, err)
	}

	log.Debug().Str("tool_id", toolID).Msg("Tool deleted from Meilisearch")
	return nil
}

// DeleteToolbox removes all tools of a toolbox from the index
func (c *Client) DeleteToolbox(ctx context.Context, toolboxID string) error {
	index := c.client.Index(c.indexName)
	filter := fmt.Sprintf("toolbox_id = '%s'", toolboxID)
	_, err := index.DeleteDocumentsByFilter(filter)
	if err != nil {
		return fmt.Errorf("failed to delete toolbox tools %s: %w", toolboxID, err)
	}

	log.Info().Str("toolbox_id", toolboxID).Msg("Toolbox deleted from Meilisearch")
	return nil
}

// Search searches for tools (keyword search)
func (c *Client) Search(ctx context.Context, params search.SearchParams) (*search.SearchResult, error) {
	return c.doSearch(ctx, params, nil)
}

// HybridSearch performs hybrid search (keyword + semantic)
func (c *Client) HybridSearch(ctx context.Context, params search.HybridSearchParams) (*search.SearchResult, error) {
	if !c.SupportsHybridSearch() {
		// Fall back to regular search if hybrid is not supported
		return c.Search(ctx, params.SearchParams)
	}

	hybridParams := &meilisearch.SearchRequestHybrid{
		SemanticRatio: params.SemanticRatio,
		Embedder:      params.Embedder,
	}

	if hybridParams.Embedder == "" {
		hybridParams.Embedder = c.config.HybridSearch.EmbedderName
		if hybridParams.Embedder == "" {
			hybridParams.Embedder = "default"
		}
	}

	if hybridParams.SemanticRatio == 0 {
		hybridParams.SemanticRatio = c.config.HybridSearch.SemanticRatio
		if hybridParams.SemanticRatio == 0 {
			hybridParams.SemanticRatio = 0.5 // Default balanced
		}
	}

	return c.doSearch(ctx, params.SearchParams, hybridParams)
}

// doSearch performs the actual search
func (c *Client) doSearch(ctx context.Context, params search.SearchParams, hybrid *meilisearch.SearchRequestHybrid) (*search.SearchResult, error) {
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

	query := params.Query

	// Build filter
	var filters []string
	if params.ToolboxID != "" {
		filters = append(filters, fmt.Sprintf("toolbox_id = '%s'", params.ToolboxID))
	}
	if params.ToolkitID != "" {
		filters = append(filters, fmt.Sprintf("toolkit_id = '%s'", params.ToolkitID))
	}
	if params.MinRating > 0 {
		filters = append(filters, fmt.Sprintf("rating >= %f", params.MinRating))
	}
	if len(params.Tags) > 0 {
		for _, tag := range params.Tags {
			filters = append(filters, fmt.Sprintf("tags = '%s'", tag))
		}
	}

	filterStr := ""
	if len(filters) > 0 {
		filterStr = strings.Join(filters, " AND ")
	}

	// Build sort
	var sort []string
	if params.SortBy != "" {
		order := "desc"
		if params.SortOrder == "asc" {
			order = "asc"
		}
		sort = append(sort, fmt.Sprintf("%s:%s", params.SortBy, order))
	} else {
		sort = append(sort, "created_at:desc")
	}

	// Build facets
	var facets []string
	if params.IncludeFacets {
		facets = []string{"tags", "toolbox_id", "toolkit_id"}
	}

	// Calculate offset from page
	offset := int64((params.Page - 1) * params.PageSize)

	// Build search request
	searchReq := &meilisearch.SearchRequest{
		Limit:                int64(params.PageSize),
		Offset:               offset,
		AttributesToRetrieve: []string{"*"},
		Sort:                 sort,
	}

	if filterStr != "" {
		searchReq.Filter = filterStr
	}

	if len(facets) > 0 {
		searchReq.Facets = facets
	}

	if hybrid != nil {
		searchReq.Hybrid = hybrid
	}

	start := time.Now()
	index := c.client.Index(c.indexName)
	result, err := index.Search(query, searchReq)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	searchTime := time.Since(start).Milliseconds()

	// Parse results - Hit is map[string]json.RawMessage
	tools := make([]search.ToolDocument, 0)
	for _, hit := range result.Hits {
		// Decode hit into a map
		var hitMap map[string]interface{}
		if err := hit.DecodeInto(&hitMap); err != nil {
			log.Warn().Err(err).Msg("Failed to decode hit")
			continue
		}

		tool := search.ToolDocument{
			ID:          getString(hitMap, "id"),
			Name:        getString(hitMap, "name"),
			Description: getString(hitMap, "description"),
			ToolboxID:   getString(hitMap, "toolbox_id"),
			ToolboxName: getString(hitMap, "toolbox_name"),
			ToolkitID:   getString(hitMap, "toolkit_id"),
			MCPServer:   getString(hitMap, "mcp_server"),
			Tags:        getStringArray(hitMap, "tags"),
			CallsCount:  int32(getInt(hitMap, "calls_count")),
			Rating:      getFloat(hitMap, "rating"),
			CreatedAt:   getInt(hitMap, "created_at"),
			UpdatedAt:   getInt(hitMap, "updated_at"),
		}

		// Extract semantic score if available
		if semantic, ok := hitMap["_semanticScore"].(float64); ok {
			tool.SemanticScore = semantic
		}

		tools = append(tools, tool)
	}

	total := int(result.EstimatedTotalHits)
	totalPages := (total + params.PageSize - 1) / params.PageSize

	// Parse facets
	facetResults := make(map[string][]search.FacetItem)
	if result.FacetDistribution != nil {
		// FacetDistribution is json.RawMessage
		var facetDist map[string]map[string]int64
		if err := json.Unmarshal(result.FacetDistribution, &facetDist); err == nil {
			for facetName, facetValues := range facetDist {
				items := make([]search.FacetItem, 0)
				for value, count := range facetValues {
					items = append(items, search.FacetItem{
						Value: value,
						Count: int(count),
					})
				}
				facetResults[facetName] = items
			}
		}
	}

	log.Debug().
		Str("query", params.Query).
		Int("total", total).
		Int64("search_time_ms", searchTime).
		Bool("hybrid", hybrid != nil).
		Msg("Meilisearch search completed")

	return &search.SearchResult{
		Tools:      tools,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
		Facets:     facetResults,
		SearchTime: searchTime,
		Provider:   "meilisearch",
		Hybrid:     hybrid != nil,
	}, nil
}

// UpdateCallsCount updates the calls count for a tool
func (c *Client) UpdateCallsCount(ctx context.Context, toolID string, count int32) error {
	doc := map[string]interface{}{
		"id":          toolID,
		"calls_count": count,
		"updated_at":  time.Now().Unix(),
	}

	index := c.client.Index(c.indexName)
	primaryKey := "id"
	_, err := index.UpdateDocuments([]map[string]interface{}{doc}, &primaryKey)
	if err != nil {
		return fmt.Errorf("failed to update calls count: %w", err)
	}

	return nil
}

// GetStats returns index statistics
func (c *Client) GetStats(ctx context.Context) (map[string]interface{}, error) {
	index := c.client.Index(c.indexName)
	stats, err := index.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get index stats: %w", err)
	}

	return map[string]interface{}{
		"index":          c.indexName,
		"provider":       "meilisearch",
		"num_documents":  stats.NumberOfDocuments,
		"is_indexing":    stats.IsIndexing,
		"hybrid_enabled": c.SupportsHybridSearch(),
	}, nil
}

// Close closes the client (no-op for Meilisearch, but implements interface)
func (c *Client) Close() error {
	log.Info().Msg("Meilisearch client closed")
	return nil
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

// toInterfaceSlice converts strings to []interface{} for Meilisearch SDK
func toInterfaceSlice(strs ...string) []interface{} {
	result := make([]interface{}, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}
