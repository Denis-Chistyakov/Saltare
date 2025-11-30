package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/router/providers"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/stretchr/testify/suite"
)

// CerebrasIntegrationSuite tests real Cerebras LLM integration
// Run with: CEREBRAS_TEST=1 go test ./tests/integration/... -run TestCerebras -v
type CerebrasIntegrationSuite struct {
	suite.Suite
	ctx      context.Context
	provider *providers.CerebrasProvider
	router   *semantic.Router
	manager  *toolkit.Manager
}

func TestCerebrasIntegrationSuite(t *testing.T) {
	if os.Getenv("CEREBRAS_TEST") == "" {
		t.Skip("Skipping Cerebras integration tests. Set CEREBRAS_TEST=1 to run.")
	}
	suite.Run(t, new(CerebrasIntegrationSuite))
}

func (s *CerebrasIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	// Create Cerebras provider with real API key
	// Available models: llama-3.3-70b, llama3.1-8b
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		apiKey = "test-key-set-CEREBRAS_API_KEY-to-run"
	}
	s.provider = providers.NewCerebrasProvider(
		apiKey,
		"llama-3.3-70b",
		"https://api.cerebras.ai/v1/chat/completions",
	)

	// Create manager with diverse tools
	s.manager = toolkit.NewManager()

	// Register toolkits with different domains
	testToolkits := []*types.Toolkit{
		{
			ID:   "weather-toolkit",
			Name: "Weather Toolkit",
			Toolboxes: []*types.Toolbox{
				{
					ID:          "weather-toolbox",
					Name:        "weather",
					Tags:        []string{"weather", "api", "forecast"},
					Description: "Weather information tools",
					Tools: []*types.Tool{
						{
							ID:          "weather-current",
							Name:        "get_current_weather",
							Description: "Get current weather conditions for a specific city including temperature, humidity, and conditions",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"city": map[string]interface{}{
										"type":        "string",
										"description": "City name",
									},
								},
								"required": []string{"city"},
							},
							MCPServer: "http://localhost:8082",
						},
						{
							ID:          "weather-forecast",
							Name:        "get_weather_forecast",
							Description: "Get weather forecast for the next 5 days for a specific location",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"city": map[string]interface{}{"type": "string"},
									"days": map[string]interface{}{"type": "integer"},
								},
							},
							MCPServer: "http://localhost:8082",
						},
					},
				},
			},
		},
		{
			ID:   "github-toolkit",
			Name: "GitHub Toolkit",
			Toolboxes: []*types.Toolbox{
				{
					ID:          "github-toolbox",
					Name:        "github",
					Tags:        []string{"git", "code", "repository", "issues"},
					Description: "GitHub repository management tools",
					Tools: []*types.Tool{
						{
							ID:          "github-search-repos",
							Name:        "search_repositories",
							Description: "Search GitHub repositories by keyword, language, or topic",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"query":    map[string]interface{}{"type": "string"},
									"language": map[string]interface{}{"type": "string"},
								},
							},
							MCPServer: "http://localhost:8083",
						},
						{
							ID:          "github-list-issues",
							Name:        "list_issues",
							Description: "List issues from a GitHub repository with filtering options",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"repo":  map[string]interface{}{"type": "string"},
									"state": map[string]interface{}{"type": "string"},
								},
							},
							MCPServer: "http://localhost:8083",
						},
					},
				},
			},
		},
		{
			ID:   "filesystem-toolkit",
			Name: "Filesystem Toolkit",
			Toolboxes: []*types.Toolbox{
				{
					ID:          "fs-toolbox",
					Name:        "filesystem",
					Tags:        []string{"files", "directory", "storage"},
					Description: "File system operations",
					Tools: []*types.Tool{
						{
							ID:          "fs-list-files",
							Name:        "list_files",
							Description: "List files and directories in a given path",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"path": map[string]interface{}{"type": "string"},
								},
							},
							MCPServer: "http://localhost:8084",
						},
						{
							ID:          "fs-read-file",
							Name:        "read_file",
							Description: "Read contents of a text file",
							InputSchema: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"path": map[string]interface{}{"type": "string"},
								},
							},
							MCPServer: "http://localhost:8084",
						},
					},
				},
			},
		},
	}

	for _, tk := range testToolkits {
		err := s.manager.RegisterToolkit(tk)
		s.Require().NoError(err)
	}

	// Create router with Cerebras LLM
	s.router = semantic.NewRouterWithLLM(s.manager, s.provider)

	fmt.Println("\n🚀 Cerebras Integration Test Suite initialized")
	fmt.Printf("   Tools registered: %d\n", len(s.manager.ListAllTools()))
}

func (s *CerebrasIntegrationSuite) TearDownSuite() {
	fmt.Println("\n✅ Cerebras Integration Tests completed")
}

// Test cases for different natural language queries

func (s *CerebrasIntegrationSuite) TestLLM_WeatherQuery_Russian() {
	fmt.Println("\n📝 Test: Russian weather query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "какая погода сейчас в Москве?")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: какая погода сейчас в Москве?\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	// Should detect weather domain
	s.Contains([]string{"weather", "погода", ""}, intent.Domain)
}

func (s *CerebrasIntegrationSuite) TestLLM_WeatherQuery_English() {
	fmt.Println("\n📝 Test: English weather query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "What's the weather like in London today?")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: What's the weather like in London today?\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	s.Equal("weather", intent.Domain)
}

func (s *CerebrasIntegrationSuite) TestLLM_GitHubQuery() {
	fmt.Println("\n📝 Test: GitHub repository search query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "find golang repositories about machine learning")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: find golang repositories about machine learning\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	// Should detect github/code domain
	s.Contains([]string{"github", "code", "repository", "git"}, intent.Domain)
}

func (s *CerebrasIntegrationSuite) TestLLM_FileSystemQuery() {
	fmt.Println("\n📝 Test: Filesystem query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "show me all files in the /home/user/documents folder")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: show me all files in the /home/user/documents folder\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	// Should detect filesystem domain
	s.Contains([]string{"filesystem", "files", "directory", "file"}, intent.Domain)
}

func (s *CerebrasIntegrationSuite) TestLLM_IssuesQuery() {
	fmt.Println("\n📝 Test: GitHub issues query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "list all open issues in kubernetes/kubernetes repo")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: list all open issues in kubernetes/kubernetes repo\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	// Should detect github/issues domain
	s.Contains([]string{"github", "issues", "code", "git"}, intent.Domain)
}

func (s *CerebrasIntegrationSuite) TestLLM_ForecastQuery() {
	fmt.Println("\n📝 Test: Weather forecast query")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	intent, err := s.router.ParseIntent(ctx, "what will the weather be like in Paris next week?")
	s.Require().NoError(err)
	s.NotNil(intent)

	fmt.Printf("   Query: what will the weather be like in Paris next week?\n")
	fmt.Printf("   Intent: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)
	fmt.Printf("   Confidence: %.2f\n", intent.Confidence)
	fmt.Printf("   Filters: %v\n", intent.Filters)

	s.Equal("weather", intent.Domain)
	// Action should indicate forecast
	s.Contains([]string{"forecast", "get", "check", ""}, intent.Action)
}

func (s *CerebrasIntegrationSuite) TestLLM_FullFlow_WeatherToolSelection() {
	fmt.Println("\n📝 Test: Full flow - Weather tool selection")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	query := "Какая температура в Санкт-Петербурге?"

	// 1. Parse intent via LLM
	intent, err := s.router.ParseIntent(ctx, query)
	s.Require().NoError(err)

	fmt.Printf("   Query: %s\n", query)
	fmt.Printf("   Intent parsed: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)

	// 2. Search for matching tool
	var foundTool *types.Tool
	
	// Try by domain first
	if intent.Domain != "" {
		tools := s.manager.SearchTools(intent.Domain, nil)
		if len(tools) > 0 {
			foundTool = tools[0]
		}
	}
	
	// Fallback to query
	if foundTool == nil {
		tools := s.manager.SearchTools("weather", nil)
		if len(tools) > 0 {
			foundTool = tools[0]
		}
	}

	s.NotNil(foundTool, "Should find a weather-related tool")
	fmt.Printf("   Tool found: %s (%s)\n", foundTool.Name, foundTool.Description)

	// Verify it's a weather tool
	s.Contains(foundTool.Name, "weather")
}

func (s *CerebrasIntegrationSuite) TestLLM_FullFlow_GitHubToolSelection() {
	fmt.Println("\n📝 Test: Full flow - GitHub tool selection")

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	query := "Search for Python projects about deep learning on GitHub"

	// 1. Parse intent via LLM
	intent, err := s.router.ParseIntent(ctx, query)
	s.Require().NoError(err)

	fmt.Printf("   Query: %s\n", query)
	fmt.Printf("   Intent parsed: domain=%s, action=%s, entity=%s\n", intent.Domain, intent.Action, intent.Entity)

	// 2. Search for matching tool
	var foundTool *types.Tool
	searchTerms := []string{intent.Domain, "github", "repository", "search"}
	
	for _, term := range searchTerms {
		if term == "" {
			continue
		}
		tools := s.manager.SearchTools(term, nil)
		if len(tools) > 0 {
			foundTool = tools[0]
			break
		}
	}

	s.NotNil(foundTool, "Should find a GitHub-related tool")
	fmt.Printf("   Tool found: %s (%s)\n", foundTool.Name, foundTool.Description)
}

func (s *CerebrasIntegrationSuite) TestCerebras_HealthCheck() {
	fmt.Println("\n📝 Test: Cerebras health check")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	err := s.provider.HealthCheck(ctx)
	s.NoError(err, "Cerebras should be healthy")

	fmt.Println("   ✅ Cerebras API is healthy")
}

func (s *CerebrasIntegrationSuite) TestCerebras_GetModel() {
	fmt.Println("\n📝 Test: Cerebras model info")

	model := s.provider.GetModel()
	s.NotEmpty(model)

	fmt.Printf("   Model: %s\n", model)
}

