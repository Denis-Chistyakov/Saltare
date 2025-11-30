package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewCerebrasProvider(t *testing.T) {
	provider := NewCerebrasProvider("test-api-key", "llama3.1-70b", "")
	assert.NotNil(t, provider)
	assert.Equal(t, "test-api-key", provider.config.APIKey)
	assert.Equal(t, "llama3.1-70b", provider.config.Model)
	assert.Equal(t, "https://api.cerebras.ai/v1/chat/completions", provider.endpoint)
}

func TestNewCerebrasProvider_Defaults(t *testing.T) {
	provider := NewCerebrasProvider("test-key", "", "")
	assert.Equal(t, "llama3.1-70b", provider.config.Model)
	assert.Equal(t, "https://api.cerebras.ai/v1/chat/completions", provider.endpoint)
}

func TestCerebrasGenerateIntent_Success(t *testing.T) {
	// Mock Cerebras API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer test-api-key")

		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		assert.NoError(t, err)
		assert.Contains(t, reqBody, "model")
		assert.Contains(t, reqBody, "messages")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"action": "get", "domain": "weather", "entity": "current", "filters": {"city": "London"}}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-api-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	intent, err := provider.GenerateIntent(context.Background(), "what's the weather in London")
	assert.NoError(t, err)
	assert.NotNil(t, intent)
	assert.Equal(t, "get", intent.Action)
	assert.Equal(t, "weather", intent.Domain)
	assert.Equal(t, "current", intent.Entity)
	assert.Equal(t, "London", intent.Filters["city"])
}

func TestCerebrasGenerateIntent_GitHubQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"action": "search", "domain": "github", "entity": "issues", "filters": {"state": "open", "project": "saltare"}}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	intent, err := provider.GenerateIntent(context.Background(), "Find open GitHub issues for saltare")
	assert.NoError(t, err)
	assert.NotNil(t, intent)
	assert.Equal(t, "search", intent.Action)
	assert.Equal(t, "github", intent.Domain)
	assert.Equal(t, "issues", intent.Entity)
}

func TestCerebrasGenerateIntent_WithExtraText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `Sure! Here's the intent: {"action": "get", "domain": "weather", "entity": "current"} Hope that helps!`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	intent, err := provider.GenerateIntent(context.Background(), "weather")
	assert.NoError(t, err)
	assert.NotNil(t, intent)
	assert.Equal(t, "get", intent.Action)
}

func TestCerebrasGenerateIntent_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "This is not valid JSON",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	_, err := provider.GenerateIntent(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not extract JSON")
}

func TestCerebrasGenerateIntent_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "Internal server error"}}`))
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")
	provider.config.Retries = 1 // Reduce retries for faster test

	_, err := provider.GenerateIntent(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Cerebras API error")
}

func TestCerebrasGenerateIntent_RateLimitRetry(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: rate limit
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": {"message": "Rate limit exceeded"}}`))
		} else {
			// Second call: success
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `{"action": "get", "domain": "test"}`,
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	// Should NOT retry on 429 (client error), so it will fail
	_, err := provider.GenerateIntent(context.Background(), "test")
	assert.Error(t, err)
	assert.Equal(t, 1, callCount) // Only one call, no retry on 429
}

func TestCerebrasHealthCheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "pong"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	err := provider.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestCerebrasHealthCheck_Failure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("invalid-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	err := provider.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed")
}

func TestCerebrasHealthCheck_Unreachable(t *testing.T) {
	provider := NewCerebrasProvider("test-key", "llama3.1-70b", "http://localhost:99999")

	err := provider.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

func TestCerebrasGetModel(t *testing.T) {
	provider := NewCerebrasProvider("test-key", "llama3.1-8b", "")
	assert.Equal(t, "llama3.1-8b", provider.GetModel())
}

func TestCerebrasBuildPrompt(t *testing.T) {
	provider := NewCerebrasProvider("test-key", "llama3.1-70b", "")

	systemPrompt, userPrompt := provider.buildPrompt("what's the weather in Tokyo")

	assert.Contains(t, systemPrompt, "intent parser")
	assert.Contains(t, systemPrompt, "JSON")
	assert.Equal(t, "what's the weather in Tokyo", userPrompt)
}

func TestCerebrasContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Simulate slow response
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"action": "test"}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := provider.GenerateIntent(ctx, "test")
	assert.Error(t, err)
}

// BenchmarkCerebrasGenerateIntent benchmarks intent generation
func BenchmarkCerebrasGenerateIntent(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"action": "get", "domain": "weather"}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewCerebrasProvider("test-key", "llama3.1-70b", ts.URL+"/v1/chat/completions")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.GenerateIntent(context.Background(), "test query")
	}
}

