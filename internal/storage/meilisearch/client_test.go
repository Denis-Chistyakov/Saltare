package meilisearch

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// MeilisearchTestSuite requires a running Meilisearch instance
// Run with: docker-compose -f docker/docker-compose.meilisearch.yml up -d
type MeilisearchTestSuite struct {
	suite.Suite
	client *Client
	ctx    context.Context
}

func TestMeilisearchTestSuite(t *testing.T) {
	// Skip if MEILISEARCH_TEST is not set (CI/CD)
	if os.Getenv("MEILISEARCH_TEST") == "" {
		t.Skip("Skipping Meilisearch tests. Set MEILISEARCH_TEST=1 to run.")
	}
	suite.Run(t, new(MeilisearchTestSuite))
}

func (s *MeilisearchTestSuite) SetupSuite() {
	s.ctx = context.Background()

	cfg := types.MeilisearchConfig{
		Enabled:   true,
		Host:      "http://localhost:7700",
		APIKey:    "saltare-dev-key-12345",
		IndexName: "tools_test",
		Timeout:   "5s",
		HybridSearch: types.HybridConfig{
			Enabled:       false, // Disable hybrid for basic tests
			SemanticRatio: 0.5,
			EmbedderName:  "default",
		},
	}

	client, err := NewClient(cfg)
	s.Require().NoError(err)
	s.client = client

	// Create schema (ignore error if exists)
	_ = s.client.CreateSchema(s.ctx)
}

func (s *MeilisearchTestSuite) TearDownSuite() {
	if s.client != nil {
		// Clean up test index
		s.client.client.DeleteIndex(s.client.indexName)
		_ = s.client.Close()
	}
}

func (s *MeilisearchTestSuite) SetupTest() {
	// Delete and recreate index to ensure clean state
	s.client.client.DeleteIndex(s.client.indexName)
	time.Sleep(300 * time.Millisecond) // Wait for deletion
	_ = s.client.CreateSchema(s.ctx)
	// Wait for index creation (longer for race detector)
	time.Sleep(500 * time.Millisecond)
}

func (s *MeilisearchTestSuite) TestHealthCheck() {
	err := s.client.HealthCheck(s.ctx)
	s.NoError(err)
}

func (s *MeilisearchTestSuite) TestName() {
	s.Equal("meilisearch", s.client.Name())
}

func (s *MeilisearchTestSuite) TestSupportsHybridSearch() {
	// Without embedder configured, should return false
	s.False(s.client.SupportsHybridSearch())
}

func (s *MeilisearchTestSuite) TestIndexTool() {
	tool := &types.Tool{
		ID:          "test-tool-1",
		Name:        "get_weather",
		Description: "Get current weather for a location",
		MCPServer:   "http://localhost:8082",
		CreatedAt:   time.Now(),
	}

	err := s.client.IndexTool(s.ctx, tool, "weather-toolbox", "Weather", "local-toolkit", []string{"weather", "api"})
	s.NoError(err)

	// Wait for indexing
	time.Sleep(500 * time.Millisecond)

	// Search for it
	result, err := s.client.Search(s.ctx, search.SearchParams{
		Query: "weather",
	})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1)
	s.Equal("get_weather", result.Tools[0].Name)
	s.Equal("meilisearch", result.Provider)
}

func (s *MeilisearchTestSuite) TestIndexToolbox() {
	toolbox := &types.Toolbox{
		ID:   "github-toolbox",
		Name: "GitHub",
		Tags: []string{"git", "api", "code"},
		Tools: []*types.Tool{
			{
				ID:          "gh-list-repos",
				Name:        "list_repos",
				Description: "List GitHub repositories",
				MCPServer:   "http://localhost:8083",
				CreatedAt:   time.Now(),
			},
			{
				ID:          "gh-search-issues",
				Name:        "search_issues",
				Description: "Search GitHub issues",
				MCPServer:   "http://localhost:8083",
				CreatedAt:   time.Now(),
			},
		},
	}

	err := s.client.IndexToolbox(s.ctx, toolbox, "local-toolkit")
	s.NoError(err)

	// Wait for indexing
	time.Sleep(500 * time.Millisecond)

	// Search for all tools in toolbox
	result, err := s.client.Search(s.ctx, search.SearchParams{
		ToolboxID: "github-toolbox",
	})
	s.NoError(err)
	s.Equal(2, result.Total)
}

func (s *MeilisearchTestSuite) TestSearchByQuery() {
	// Index test data
	tools := []*types.Tool{
		{ID: "tool-1", Name: "get_weather", Description: "Get current weather", CreatedAt: time.Now()},
		{ID: "tool-2", Name: "search_code", Description: "Search source code", CreatedAt: time.Now()},
		{ID: "tool-3", Name: "list_files", Description: "List files in directory", CreatedAt: time.Now()},
	}

	for _, tool := range tools {
		err := s.client.IndexTool(s.ctx, tool, "test-tb", "Test", "test-tk", nil)
		s.NoError(err)
	}

	time.Sleep(500 * time.Millisecond)

	// Search for "weather"
	result, err := s.client.Search(s.ctx, search.SearchParams{Query: "weather"})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1)
	s.Equal("get_weather", result.Tools[0].Name)

	// Search for "code"
	result, err = s.client.Search(s.ctx, search.SearchParams{Query: "code"})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1)
}

func (s *MeilisearchTestSuite) TestSearchWithFilters() {
	// Index tools with different tags
	tools := []*types.Tool{
		{ID: "filter-1", Name: "tool1", Description: "First tool", CreatedAt: time.Now()},
		{ID: "filter-2", Name: "tool2", Description: "Second tool", CreatedAt: time.Now()},
	}

	s.client.IndexTool(s.ctx, tools[0], "tb-a", "TB A", "tk-1", []string{"api", "web"})
	s.client.IndexTool(s.ctx, tools[1], "tb-b", "TB B", "tk-2", []string{"cli", "local"})

	time.Sleep(500 * time.Millisecond)

	// Filter by toolkit
	result, err := s.client.Search(s.ctx, search.SearchParams{ToolkitID: "tk-1"})
	s.NoError(err)
	s.Equal(1, result.Total)
	s.Equal("filter-1", result.Tools[0].ID)

	// Filter by tag
	result, err = s.client.Search(s.ctx, search.SearchParams{Tags: []string{"cli"}})
	s.NoError(err)
	s.Equal(1, result.Total)
	s.Equal("filter-2", result.Tools[0].ID)
}

func (s *MeilisearchTestSuite) TestSearchWithFacets() {
	tools := []*types.Tool{
		{ID: "facet-1", Name: "tool1", CreatedAt: time.Now()},
		{ID: "facet-2", Name: "tool2", CreatedAt: time.Now()},
		{ID: "facet-3", Name: "tool3", CreatedAt: time.Now()},
	}

	s.client.IndexTool(s.ctx, tools[0], "tb-1", "TB1", "tk", []string{"api"})
	s.client.IndexTool(s.ctx, tools[1], "tb-1", "TB1", "tk", []string{"api"})
	s.client.IndexTool(s.ctx, tools[2], "tb-2", "TB2", "tk", []string{"cli"})

	time.Sleep(500 * time.Millisecond)

	result, err := s.client.Search(s.ctx, search.SearchParams{
		IncludeFacets: true,
	})
	s.NoError(err)
	s.Equal(3, result.Total)
	s.NotEmpty(result.Facets)
}

func (s *MeilisearchTestSuite) TestSearchPagination() {
	// Index 25 tools
	for i := 1; i <= 25; i++ {
		tool := &types.Tool{
			ID:        fmt.Sprintf("page-tool-%d", i),
			Name:      fmt.Sprintf("tool_%d", i),
			CreatedAt: time.Now(),
		}
		s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)
	}

	time.Sleep(1 * time.Second)

	// Page 1
	result, err := s.client.Search(s.ctx, search.SearchParams{
		Page:     1,
		PageSize: 10,
	})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 25)
	s.Len(result.Tools, 10)
	s.Equal(3, result.TotalPages)

	// Page 2
	result, err = s.client.Search(s.ctx, search.SearchParams{
		Page:     2,
		PageSize: 10,
	})
	s.NoError(err)
	s.Len(result.Tools, 10)
}

func (s *MeilisearchTestSuite) TestDeleteTool() {
	tool := &types.Tool{
		ID:        "delete-test",
		Name:      "to_be_deleted",
		CreatedAt: time.Now(),
	}

	err := s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)
	s.NoError(err)

	time.Sleep(500 * time.Millisecond)

	// Verify exists
	result, _ := s.client.Search(s.ctx, search.SearchParams{Query: "to_be_deleted"})
	s.GreaterOrEqual(result.Total, 1)

	// Delete
	err = s.client.DeleteTool(s.ctx, "delete-test")
	s.NoError(err)

	// Meilisearch deletes are async, wait for task to complete
	time.Sleep(1500 * time.Millisecond)

	// Verify deleted
	result, _ = s.client.Search(s.ctx, search.SearchParams{Query: "to_be_deleted"})
	s.Equal(0, result.Total)
}

func (s *MeilisearchTestSuite) TestDeleteToolbox() {
	toolbox := &types.Toolbox{
		ID:   "delete-toolbox",
		Name: "To Delete",
		Tools: []*types.Tool{
			{ID: "dt-1", Name: "tool1", CreatedAt: time.Now()},
			{ID: "dt-2", Name: "tool2", CreatedAt: time.Now()},
		},
	}

	err := s.client.IndexToolbox(s.ctx, toolbox, "tk")
	s.NoError(err)

	time.Sleep(500 * time.Millisecond)

	// Verify exists
	result, _ := s.client.Search(s.ctx, search.SearchParams{ToolboxID: "delete-toolbox"})
	s.Equal(2, result.Total)

	// Delete toolbox
	err = s.client.DeleteToolbox(s.ctx, "delete-toolbox")
	s.NoError(err)

	// Meilisearch deletes are async, wait for task to complete
	time.Sleep(1500 * time.Millisecond)

	// Verify deleted
	result, _ = s.client.Search(s.ctx, search.SearchParams{ToolboxID: "delete-toolbox"})
	s.Equal(0, result.Total)
}

func (s *MeilisearchTestSuite) TestGetStats() {
	// Index a tool
	tool := &types.Tool{ID: "stats-test", Name: "stats", CreatedAt: time.Now()}
	s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)

	time.Sleep(500 * time.Millisecond)

	stats, err := s.client.GetStats(s.ctx)
	s.NoError(err)
	s.NotNil(stats)
	s.Equal("tools_test", stats["index"])
	s.Equal("meilisearch", stats["provider"])
}

func (s *MeilisearchTestSuite) TestHybridSearchFallback() {
	// Without embedders configured, HybridSearch should fall back to regular Search
	tool := &types.Tool{
		ID:          "hybrid-test",
		Name:        "semantic_tool",
		Description: "Tool for testing semantic search",
		CreatedAt:   time.Now(),
	}

	err := s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)
	s.NoError(err)

	time.Sleep(500 * time.Millisecond)

	result, err := s.client.HybridSearch(s.ctx, search.HybridSearchParams{
		SearchParams: search.SearchParams{
			Query: "semantic",
		},
		SemanticRatio: 0.7,
	})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1)
	s.False(result.Hybrid) // Should be false because embedders are not configured
}

// Unit tests (no Meilisearch required)

func TestNewClient_Disabled(t *testing.T) {
	cfg := types.MeilisearchConfig{
		Enabled: false,
	}

	_, err := NewClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestNewClient_NoHost(t *testing.T) {
	cfg := types.MeilisearchConfig{
		Enabled: true,
		Host:    "",
	}

	_, err := NewClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host is not configured")
}

func TestNewClient_DefaultIndexName(t *testing.T) {
	// This test would fail without a running Meilisearch
	// but we can test that the config is parsed correctly
	cfg := types.MeilisearchConfig{
		Enabled:   true,
		Host:      "http://localhost:7700",
		APIKey:    "test-key",
		IndexName: "", // Should default to "tools"
		Timeout:   "10s",
	}

	// Skip actual connection
	if os.Getenv("MEILISEARCH_TEST") == "" {
		return
	}

	client, err := NewClient(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "tools", client.indexName)
}

func TestHelperFunctions(t *testing.T) {
	doc := map[string]interface{}{
		"string_field": "hello",
		"int_field":    float64(42),
		"float_field":  3.14,
		"array_field":  []interface{}{"a", "b", "c"},
	}

	assert.Equal(t, "hello", getString(doc, "string_field"))
	assert.Equal(t, "", getString(doc, "missing"))

	assert.Equal(t, int64(42), getInt(doc, "int_field"))
	assert.Equal(t, int64(0), getInt(doc, "missing"))

	assert.InDelta(t, 3.14, getFloat(doc, "float_field"), 0.001)
	assert.Equal(t, float64(0), getFloat(doc, "missing"))

	arr := getStringArray(doc, "array_field")
	assert.Len(t, arr, 3)
	assert.Equal(t, []string{"a", "b", "c"}, arr)
	assert.Nil(t, getStringArray(doc, "missing"))
}

