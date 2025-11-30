package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewOllamaProvider tests provider creation
func TestNewOllamaProvider(t *testing.T) {
	provider := NewOllamaProvider("", "", 0)

	if provider == nil {
		t.Fatal("Expected provider to be created")
	}

	if provider.endpoint != "http://localhost:11434" {
		t.Errorf("Expected default endpoint, got %s", provider.endpoint)
	}

	if provider.model != "llama3.2:3b" {
		t.Errorf("Expected default model, got %s", provider.model)
	}

	if provider.timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", provider.timeout)
	}
}

// TestNewOllamaProvider_CustomSettings tests custom configuration
func TestNewOllamaProvider_CustomSettings(t *testing.T) {
	provider := NewOllamaProvider(
		"http://custom:8080",
		"llama3:8b",
		10*time.Second,
	)

	if provider.endpoint != "http://custom:8080" {
		t.Errorf("Expected custom endpoint, got %s", provider.endpoint)
	}

	if provider.model != "llama3:8b" {
		t.Errorf("Expected custom model, got %s", provider.model)
	}

	if provider.timeout != 10*time.Second {
		t.Errorf("Expected 10s timeout, got %v", provider.timeout)
	}
}

// TestGenerateIntent_Success tests successful intent generation
func TestGenerateIntent_Success(t *testing.T) {
	// Mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("Expected /api/generate, got %s", r.URL.Path)
		}

		// Return mock response
		resp := OllamaResponse{
			Model:     "llama3.2:3b",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  `{"action":"get","domain":"weather","entity":"current","filters":{"city":"London"},"confidence":0.95}`,
			Done:      true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	intent, err := provider.GenerateIntent(context.Background(), "What's the weather in London?")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if intent.Action != "get" {
		t.Errorf("Expected action 'get', got '%s'", intent.Action)
	}

	if intent.Domain != "weather" {
		t.Errorf("Expected domain 'weather', got '%s'", intent.Domain)
	}

	if intent.Entity != "current" {
		t.Errorf("Expected entity 'current', got '%s'", intent.Entity)
	}

	if intent.Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", intent.Confidence)
	}

	city, ok := intent.Filters["city"]
	if !ok || city != "London" {
		t.Errorf("Expected city filter 'London', got %v", city)
	}
}

// TestGenerateIntent_GitHubQuery tests GitHub query parsing
func TestGenerateIntent_GitHubQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := OllamaResponse{
			Model:    "llama3.2:3b",
			Response: `{"action":"list","domain":"github","entity":"issues","filters":{"state":"open","label":"bug"},"confidence":0.9}`,
			Done:     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	intent, err := provider.GenerateIntent(context.Background(), "List open GitHub issues with bug label")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if intent.Action != "list" {
		t.Errorf("Expected action 'list', got '%s'", intent.Action)
	}

	if intent.Domain != "github" {
		t.Errorf("Expected domain 'github', got '%s'", intent.Domain)
	}

	state, ok := intent.Filters["state"]
	if !ok || state != "open" {
		t.Errorf("Expected state filter 'open', got %v", state)
	}

	label, ok := intent.Filters["label"]
	if !ok || label != "bug" {
		t.Errorf("Expected label filter 'bug', got %v", label)
	}
}

// TestGenerateIntent_WithExtraText tests parsing when LLM adds extra text
func TestGenerateIntent_WithExtraText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := OllamaResponse{
			Model: "llama3.2:3b",
			Response: `Sure, here's the parsed intent:
{"action":"search","domain":"filesystem","entity":"files","filters":{"modified":"today"},"confidence":0.85}
Hope this helps!`,
			Done: true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	intent, err := provider.GenerateIntent(context.Background(), "Find files modified today")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if intent.Action != "search" {
		t.Errorf("Expected action 'search', got '%s'", intent.Action)
	}

	if intent.Domain != "filesystem" {
		t.Errorf("Expected domain 'filesystem', got '%s'", intent.Domain)
	}
}

// TestGenerateIntent_InvalidJSON tests error handling for invalid JSON
func TestGenerateIntent_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := OllamaResponse{
			Model:    "llama3.2:3b",
			Response: `This is not a valid JSON response`,
			Done:     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	_, err := provider.GenerateIntent(context.Background(), "test query")
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

// TestGenerateIntent_ServerError tests retry logic
func TestGenerateIntent_ServerError(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 1*time.Second)

	_, err := provider.GenerateIntent(context.Background(), "test query")
	if err == nil {
		t.Fatal("Expected error for server error")
	}

	// Should retry 3 times
	if attemptCount != 3 {
		t.Errorf("Expected 3 retry attempts, got %d", attemptCount)
	}
}

// TestHealthCheck_Success tests successful health check
func TestHealthCheck_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("Expected /api/tags, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []string{"llama3.2:3b"},
		})
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	err := provider.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestHealthCheck_Failure tests failed health check
func TestHealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("Expected error for failed health check")
	}
}

// TestHealthCheck_Unreachable tests health check when server is unreachable
func TestHealthCheck_Unreachable(t *testing.T) {
	provider := NewOllamaProvider("http://localhost:99999", "llama3.2:3b", 1*time.Second)

	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("Expected error for unreachable server")
	}
}

// TestGetModel tests model getter
func TestGetModel(t *testing.T) {
	provider := NewOllamaProvider("", "custom-model:7b", 0)

	if provider.GetModel() != "custom-model:7b" {
		t.Errorf("Expected 'custom-model:7b', got '%s'", provider.GetModel())
	}
}

// TestBuildPrompt tests prompt generation
func TestBuildPrompt(t *testing.T) {
	provider := NewOllamaProvider("", "", 0)

	prompt := provider.buildPrompt("What's the weather?")

	if prompt == "" {
		t.Fatal("Expected non-empty prompt")
	}

	// Should contain the query
	if !containsString(prompt, "What's the weather?") {
		t.Error("Prompt should contain the query")
	}

	// Should have instructions
	if !containsString(prompt, "JSON") {
		t.Error("Prompt should mention JSON format")
	}

	// Should have examples
	if !containsString(prompt, "Examples") {
		t.Error("Prompt should include examples")
	}
}

// TestContextCancellation tests context cancellation
func TestContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		json.NewEncoder(w).Encode(OllamaResponse{})
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL, "llama3.2:3b", 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := provider.GenerateIntent(ctx, "test query")
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

