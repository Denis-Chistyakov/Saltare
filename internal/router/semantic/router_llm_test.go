package semantic

import (
	"context"
	"fmt"
	"testing"

	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// MockLLMProvider for testing
type MockLLMProvider struct {
	intent *types.Intent
	err    error
}

func (m *MockLLMProvider) GenerateIntent(ctx context.Context, query string) (*types.Intent, error) {
	return m.GenerateIntentWithContext(ctx, query, "")
}

func (m *MockLLMProvider) GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.intent, nil
}

func (m *MockLLMProvider) ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockLLMProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// TestNewRouterWithLLM tests router creation with LLM provider
func TestNewRouterWithLLM(t *testing.T) {
	manager := toolkit.NewManager()
	mockLLM := &MockLLMProvider{}

	router := NewRouterWithLLM(manager, mockLLM)

	if router == nil {
		t.Fatal("Expected router to be created")
	}

	if router.llmProvider == nil {
		t.Error("Expected LLM provider to be set")
	}
}

// TestParseIntent_WithLLM tests LLM-powered intent parsing
func TestParseIntent_WithLLM(t *testing.T) {
	manager := toolkit.NewManager()

	// Mock LLM response
	mockLLM := &MockLLMProvider{
		intent: &types.Intent{
			Action:     "get",
			Domain:     "weather",
			Entity:     "current",
			Filters:    map[string]interface{}{"city": "Paris"},
			Confidence: 0.98,
			RawQuery:   "weather in Paris",
		},
	}

	router := NewRouterWithLLM(manager, mockLLM)

	intent, err := router.ParseIntent(context.Background(), "weather in Paris")
	if err != nil {
		t.Fatalf("ParseIntent() error = %v", err)
	}

	if intent.Action != "get" {
		t.Errorf("Action = %v, want get", intent.Action)
	}

	if intent.Domain != "weather" {
		t.Errorf("Domain = %v, want weather", intent.Domain)
	}

	if intent.Confidence != 0.98 {
		t.Errorf("Confidence = %v, want 0.98", intent.Confidence)
	}
}

// TestParseIntent_LLMCaching tests that LLM results are cached
func TestParseIntent_LLMCaching(t *testing.T) {
	manager := toolkit.NewManager()

	callCount := 0
	intent := &types.Intent{
		Action:     "list",
		Domain:     "github",
		Entity:     "issues",
		Confidence: 0.95,
		Filters:    map[string]interface{}{},
		RawQuery:   "list github issues",
	}

	mockLLM := &MockLLMProvider{intent: intent}

	// Create wrapper to count calls
	wrapperLLM := &countingLLMProvider{
		provider:  mockLLM,
		callCount: &callCount,
	}

	router := NewRouterWithLLM(manager, wrapperLLM)

	query := "list github issues"

	// First call - should use LLM
	_, err := router.ParseIntent(context.Background(), query)
	if err != nil {
		t.Fatalf("First ParseIntent() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", callCount)
	}

	// Second call - should use cache
	_, err = router.ParseIntent(context.Background(), query)
	if err != nil {
		t.Fatalf("Second ParseIntent() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected still 1 LLM call (cached), got %d", callCount)
	}
}

// countingLLMProvider wraps an LLM provider to count calls
type countingLLMProvider struct {
	provider  LLMProvider
	callCount *int
}

func (c *countingLLMProvider) GenerateIntent(ctx context.Context, query string) (*types.Intent, error) {
	return c.GenerateIntentWithContext(ctx, query, "")
}

func (c *countingLLMProvider) GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error) {
	*c.callCount++
	return c.provider.GenerateIntentWithContext(ctx, query, toolsContext)
}

func (c *countingLLMProvider) ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	return c.provider.ExtractParameters(ctx, query, toolSchema)
}

func (c *countingLLMProvider) HealthCheck(ctx context.Context) error {
	return c.provider.HealthCheck(ctx)
}

