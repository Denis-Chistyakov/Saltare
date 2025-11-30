package typesense

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

// TypesenseTestSuite requires a running Typesense instance
// Run with: docker-compose -f docker/docker-compose.typesense.yml up -d
type TypesenseTestSuite struct {
	suite.Suite
	client *Client
	ctx    context.Context
}

func TestTypesenseTestSuite(t *testing.T) {
	// Skip if TYPESENSE_TEST is not set (CI/CD)
	if os.Getenv("TYPESENSE_TEST") == "" {
		t.Skip("Skipping Typesense tests. Set TYPESENSE_TEST=1 to run.")
	}
	suite.Run(t, new(TypesenseTestSuite))
}

func (s *TypesenseTestSuite) SetupSuite() {
	s.ctx = context.Background()

	cfg := types.TypesenseConfig{
		Enabled:    true,
		Nodes:      []string{"http://localhost:8108"},
		APIKey:     "saltare-dev-key-12345",
		Collection: "tools_test",
		NumTypos:   2,
		Timeout:    "5s",
	}

	client, err := NewClient(cfg)
	s.Require().NoError(err)
	s.client = client

	// Create schema (ignore error if exists)
	_ = s.client.CreateSchema(s.ctx)
}

func (s *TypesenseTestSuite) TearDownSuite() {
	if s.client != nil {
		// Clean up test collection
		_, _ = s.client.client.Collection(s.client.collection).Delete(s.ctx)
		_ = s.client.Close()
	}
}

func (s *TypesenseTestSuite) SetupTest() {
	// Drop and recreate collection to ensure clean state
	_, _ = s.client.client.Collection(s.client.collection).Delete(s.ctx)
	_ = s.client.CreateSchema(s.ctx)
	// Wait for schema creation
	time.Sleep(100 * time.Millisecond)
}

func (s *TypesenseTestSuite) TestHealthCheck() {
	err := s.client.HealthCheck(s.ctx)
	s.NoError(err)
}

func (s *TypesenseTestSuite) TestIndexTool() {
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
	time.Sleep(100 * time.Millisecond)

	// Search for it
	result, err := s.client.Search(s.ctx, search.SearchParams{
		Query: "weather",
	})
	s.NoError(err)
	s.Equal(1, result.Total)
	s.Equal("get_weather", result.Tools[0].Name)
}

func (s *TypesenseTestSuite) TestIndexToolbox() {
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
	time.Sleep(100 * time.Millisecond)

	// Search for all
	result, err := s.client.Search(s.ctx, search.SearchParams{
		ToolboxID: "github-toolbox",
	})
	s.NoError(err)
	s.Equal(2, result.Total)
}

func (s *TypesenseTestSuite) TestSearchByQuery() {
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

	time.Sleep(200 * time.Millisecond)

	// Search for "weather"
	result, err := s.client.Search(s.ctx, search.SearchParams{Query: "weather"})
	s.NoError(err)
	s.Equal(1, result.Total)
	s.Equal("get_weather", result.Tools[0].Name)

	// Search for "code"
	result, err = s.client.Search(s.ctx, search.SearchParams{Query: "code"})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1)
}

func (s *TypesenseTestSuite) TestSearchWithTypoTolerance() {
	tool := &types.Tool{
		ID:          "typo-test",
		Name:        "github_search",
		Description: "Search GitHub repositories and issues",
		CreatedAt:   time.Now(),
	}

	err := s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", []string{"github"})
	s.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Search with typo: "githb" instead of "github"
	result, err := s.client.Search(s.ctx, search.SearchParams{Query: "githb"})
	s.NoError(err)
	s.GreaterOrEqual(result.Total, 1, "Typo tolerance should find 'github' from 'githb'")
}

func (s *TypesenseTestSuite) TestSearchWithFilters() {
	// Index tools with different tags
	tools := []*types.Tool{
		{ID: "filter-1", Name: "tool1", CreatedAt: time.Now()},
		{ID: "filter-2", Name: "tool2", CreatedAt: time.Now()},
	}

	s.client.IndexTool(s.ctx, tools[0], "tb-a", "TB A", "tk-1", []string{"api", "web"})
	s.client.IndexTool(s.ctx, tools[1], "tb-b", "TB B", "tk-2", []string{"cli", "local"})

	time.Sleep(200 * time.Millisecond)

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

func (s *TypesenseTestSuite) TestSearchWithFacets() {
	tools := []*types.Tool{
		{ID: "facet-1", Name: "tool1", CreatedAt: time.Now()},
		{ID: "facet-2", Name: "tool2", CreatedAt: time.Now()},
		{ID: "facet-3", Name: "tool3", CreatedAt: time.Now()},
	}

	s.client.IndexTool(s.ctx, tools[0], "tb-1", "TB1", "tk", []string{"api"})
	s.client.IndexTool(s.ctx, tools[1], "tb-1", "TB1", "tk", []string{"api"})
	s.client.IndexTool(s.ctx, tools[2], "tb-2", "TB2", "tk", []string{"cli"})

	time.Sleep(200 * time.Millisecond)

	result, err := s.client.Search(s.ctx, search.SearchParams{
		Query:         "*",
		IncludeFacets: true,
	})
	s.NoError(err)
	s.Equal(3, result.Total)
	s.NotEmpty(result.Facets)

	// Check toolbox facets
	if tbFacets, ok := result.Facets["toolbox_id"]; ok {
		s.Len(tbFacets, 2) // tb-1 and tb-2
	}
}

func (s *TypesenseTestSuite) TestSearchPagination() {
	// Index 25 tools
	for i := 1; i <= 25; i++ {
		tool := &types.Tool{
			ID:        fmt.Sprintf("page-tool-%d", i),
			Name:      fmt.Sprintf("tool_%d", i),
			CreatedAt: time.Now(),
		}
		s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)
	}

	time.Sleep(300 * time.Millisecond)

	// Page 1
	result, err := s.client.Search(s.ctx, search.SearchParams{
		Query:    "*",
		Page:     1,
		PageSize: 10,
	})
	s.NoError(err)
	s.Equal(25, result.Total)
	s.Len(result.Tools, 10)
	s.Equal(3, result.TotalPages)

	// Page 2
	result, err = s.client.Search(s.ctx, search.SearchParams{
		Query:    "*",
		Page:     2,
		PageSize: 10,
	})
	s.NoError(err)
	s.Len(result.Tools, 10)

	// Page 3
	result, err = s.client.Search(s.ctx, search.SearchParams{
		Query:    "*",
		Page:     3,
		PageSize: 10,
	})
	s.NoError(err)
	s.Len(result.Tools, 5)
}

func (s *TypesenseTestSuite) TestDeleteTool() {
	tool := &types.Tool{
		ID:        "delete-test",
		Name:      "to_be_deleted",
		CreatedAt: time.Now(),
	}

	err := s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)
	s.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Verify exists
	result, _ := s.client.Search(s.ctx, search.SearchParams{Query: "to_be_deleted"})
	s.Equal(1, result.Total)

	// Delete
	err = s.client.DeleteTool(s.ctx, "delete-test")
	s.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Verify deleted
	result, _ = s.client.Search(s.ctx, search.SearchParams{Query: "to_be_deleted"})
	s.Equal(0, result.Total)
}

func (s *TypesenseTestSuite) TestDeleteToolbox() {
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

	time.Sleep(100 * time.Millisecond)

	// Verify exists
	result, _ := s.client.Search(s.ctx, search.SearchParams{ToolboxID: "delete-toolbox"})
	s.Equal(2, result.Total)

	// Delete toolbox
	err = s.client.DeleteToolbox(s.ctx, "delete-toolbox")
	s.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Verify deleted
	result, _ = s.client.Search(s.ctx, search.SearchParams{ToolboxID: "delete-toolbox"})
	s.Equal(0, result.Total)
}

func (s *TypesenseTestSuite) TestGetStats() {
	// Index a tool
	tool := &types.Tool{ID: "stats-test", Name: "stats", CreatedAt: time.Now()}
	s.client.IndexTool(s.ctx, tool, "tb", "TB", "tk", nil)

	time.Sleep(100 * time.Millisecond)

	stats, err := s.client.GetStats(s.ctx)
	s.NoError(err)
	s.NotNil(stats)
	s.Equal("tools_test", stats["collection"])
}

// Unit tests (no Typesense required)

func TestNewClient_Disabled(t *testing.T) {
	cfg := types.TypesenseConfig{
		Enabled: false,
	}

	_, err := NewClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestNewClient_NoNodes(t *testing.T) {
	cfg := types.TypesenseConfig{
		Enabled: true,
		Nodes:   []string{},
	}

	_, err := NewClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no typesense nodes")
}

func TestSearchParams_Defaults(t *testing.T) {
	// Test that default values are applied
	params := search.SearchParams{}
	
	// These would be applied in Search()
	assert.Equal(t, 0, params.Page)      // Will become 1
	assert.Equal(t, 0, params.PageSize)  // Will become 20
	assert.Equal(t, "", params.Query)    // Will become "*"
}

func TestToolDocument_Fields(t *testing.T) {
	doc := ToolDocument{
		ID:          "test-id",
		Name:        "test_tool",
		Description: "A test tool",
		Tags:        []string{"test", "example"},
		ToolboxID:   "toolbox-1",
		ToolkitID:   "toolkit-1",
	}

	assert.Equal(t, "test-id", doc.ID)
	assert.Equal(t, "test_tool", doc.Name)
	assert.Len(t, doc.Tags, 2)
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

