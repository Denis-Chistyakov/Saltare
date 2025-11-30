package semantic

import (
	"context"
	"testing"

	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// TestNewRouter tests router creation
func TestNewRouter(t *testing.T) {
	manager := toolkit.NewManager()
	router := NewRouter(manager)

	if router == nil {
		t.Fatal("Expected router to be created, got nil")
	}

	if router.manager != manager {
		t.Error("Expected router to have manager reference")
	}

	if router.cache == nil {
		t.Error("Expected cache to be initialized")
	}
}

// TestRoute_NoLLM tests that routing fails without LLM
func TestRoute_NoLLM(t *testing.T) {
	manager := toolkit.NewManager()
	router := NewRouter(manager)

	_, err := router.Route(context.Background(), "test query")
	if err == nil {
		t.Fatal("Expected error when no LLM provider")
	}
}

// TestClearCache tests cache clearing
func TestClearCache(t *testing.T) {
	manager := toolkit.NewManager()
	llm := &MockLLMProvider{
		intent: &types.Intent{
			Domain:     "test",
			Confidence: 0.9,
			Filters:    make(map[string]interface{}),
		},
	}
	router := NewRouterWithLLM(manager, llm)

	// Add cached intents
	router.ParseIntent(context.Background(), "query1")
	router.ParseIntent(context.Background(), "query2")

	if router.GetCacheSize() != 2 {
		t.Errorf("Cache size = %v, want 2", router.GetCacheSize())
	}

	router.ClearCache()

	if router.GetCacheSize() != 0 {
		t.Errorf("Cache size after clear = %v, want 0", router.GetCacheSize())
	}
}

// TestMatchTool tests tool matching by domain
func TestMatchTool(t *testing.T) {
	manager := toolkit.NewManager()

	tk := &types.Toolkit{
		Name:   "test",
		Status: "active",
		Toolboxes: []*types.Toolbox{
			{
				Name:    "weather",
				Version: "1.0.0",
				Tags:    []string{"weather", "forecast"},
				Tools: []*types.Tool{
					{
						Name:        "get_current",
						Description: "Get current weather",
						InputSchema: map[string]interface{}{},
						MCPServer:   "http://localhost:8082",
					},
				},
			},
		},
	}
	manager.RegisterToolkit(tk)

	router := NewRouter(manager)

	tests := []struct {
		name     string
		intent   *types.Intent
		wantTool string
		wantErr  bool
	}{
		{
			name:     "match by domain",
			intent:   &types.Intent{Domain: "weather"},
			wantTool: "get_current",
		},
		{
			name:     "match by tag",
			intent:   &types.Intent{Domain: "forecast"},
			wantTool: "get_current",
		},
		{
			name:    "no match",
			intent:  &types.Intent{Domain: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := router.MatchTool(context.Background(), tt.intent)
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Fatalf("MatchTool() error = %v", err)
			}

			if tool.Name != tt.wantTool {
				t.Errorf("Tool name = %v, want %v", tool.Name, tt.wantTool)
			}
		})
	}
}
