package providers

// Package providers implements LLM provider for Ollama local models.
// Supports intent parsing and parameter extraction.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// OllamaProvider implements LLM-based intent parsing using Ollama
type OllamaProvider struct {
	client   *http.Client
	endpoint string
	model    string
	timeout  time.Duration
	apiKey   string // For OpenRouter or other Ollama-compatible APIs
}

// OllamaRequest represents an Ollama API request
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// OllamaResponse represents an Ollama API response
type OllamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(endpoint, model string, timeout time.Duration) *OllamaProvider {
	return NewOllamaProviderWithKey(endpoint, model, timeout, "")
}

func NewOllamaProviderWithKey(endpoint, model string, timeout time.Duration, apiKey string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3.2:3b" // Default to small fast model
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &OllamaProvider{
		client: &http.Client{
			Timeout: timeout,
		},
		endpoint: endpoint,
		model:    model,
		timeout:  timeout,
		apiKey:   apiKey,
	}
}

// setHeaders sets common headers for requests (including auth if apiKey is set)
func (p *OllamaProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}

// GenerateIntent generates an intent from a natural language query using LLM
func (p *OllamaProvider) GenerateIntent(ctx context.Context, query string) (*types.Intent, error) {
	return p.GenerateIntentWithContext(ctx, query, "")
}

func (p *OllamaProvider) GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error) {
	startTime := time.Now()

	// Build prompt with context
	prompt := p.buildPromptWithContext(query, toolsContext)

	// Try with retry
	var lastErr error
	maxRetries := 3
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		intent, err := p.generateWithRetry(ctx, prompt, query)
		if err == nil {
			log.Debug().
				Str("query", query).
				Dur("duration", time.Since(startTime)).
				Int("attempt", attempt).
				Msg("Intent generated via Ollama")
			return intent, nil
		}

		lastErr = err
		log.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("max_retries", maxRetries).
			Msg("Ollama request failed, retrying")

		// Exponential backoff
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// generateWithRetry performs a single generation attempt
func (p *OllamaProvider) generateWithRetry(ctx context.Context, prompt, originalQuery string) (*types.Intent, error) {
	// Create request
	reqBody := OllamaRequest{
		Model:  p.model,
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.1,  // Low temperature for consistent intent parsing
			"num_predict": 200,  // Max tokens
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract intent from LLM response
	intent, err := p.parseIntentFromResponse(ollamaResp.Response, originalQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse intent: %w", err)
	}

	return intent, nil
}

// buildPrompt creates the LLM prompt for intent parsing
func (p *OllamaProvider) buildPrompt(query string) string {
	return p.buildPromptWithContext(query, "")
}

func (p *OllamaProvider) buildPromptWithContext(query string, toolsContext string) string {
	promptBase := `You are an intent parser for a tool execution system. Extract structured information from the user's query.`
	
	if toolsContext != "" {
		promptBase += "\n\n" + toolsContext
	}
	
	return fmt.Sprintf(`%s

User Query: "%s"

Respond ONLY with a JSON object in this exact format:
{
  "action": "get|list|search|create|update|delete",
  "domain": "weather|github|slack|filesystem|etc",
  "entity": "current|issues|messages|files|etc",
  "filters": {},
  "confidence": 0.0-1.0
}

Rules:
- action: the verb (get, list, search, create, update, delete)
- domain: should match available toolbox names (e.g., "weather", "math")
- entity: the resource type
- filters: any constraints or parameters (empty object if none)
- confidence: your confidence in this parsing (0.0 = low, 1.0 = high)

Examples:
Query: "What's the weather in London?"
{"action":"get","domain":"weather","entity":"current","filters":{"city":"London"},"confidence":0.95}

Query: "List open GitHub issues with bug label"
{"action":"list","domain":"github","entity":"issues","filters":{"state":"open","label":"bug"},"confidence":0.9}

Query: "Find files modified today"
{"action":"search","domain":"filesystem","entity":"files","filters":{"modified":"today"},"confidence":0.85}

Now parse the user query above. Respond with ONLY the JSON, no other text.`, promptBase, query)
}

// parseIntentFromResponse extracts Intent from LLM response
func (p *OllamaProvider) parseIntentFromResponse(response, originalQuery string) (*types.Intent, error) {
	// Find JSON in response (LLM might add extra text)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no JSON found in response: %s", response)
	}

	jsonStr := response[start : end+1]

	// Parse JSON
	var intent types.Intent
	if err := json.Unmarshal([]byte(jsonStr), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w (response: %s)", err, jsonStr)
	}

	// Set raw query
	intent.RawQuery = originalQuery

	// Validate
	if intent.Confidence < 0 || intent.Confidence > 1 {
		intent.Confidence = 0.5 // Default to medium confidence
	}

	log.Debug().
		Str("query", originalQuery).
		Str("action", intent.Action).
		Str("domain", intent.Domain).
		Float64("confidence", intent.Confidence).
		Msg("Intent parsed from Ollama")

	return &intent, nil
}

// HealthCheck checks if Ollama is available
func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check failed with status %d", resp.StatusCode)
	}

	log.Debug().
		Str("endpoint", p.endpoint).
		Str("model", p.model).
		Msg("Ollama health check passed")

	return nil
}

// GetModel returns the configured model name
func (p *OllamaProvider) GetModel() string {
	return p.model
}

// ExtractParameters uses LLM to extract tool parameters from natural language query
func (p *OllamaProvider) ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	// Build schema-aware prompt
	schemaJSON, _ := json.Marshal(toolSchema)
	
	prompt := fmt.Sprintf(`Extract parameters from this query for a tool.

Query: %s

Tool Input Schema:
%s

Rules:
- Extract values matching schema properties
- Handle multiple languages (English, Russian, etc.)
- Return JSON object with extracted parameters
- Return empty object {} if no parameters found
- DO NOT include extra fields

Respond with ONLY JSON, no other text.`, query, string(schemaJSON))

	reqBody := OllamaRequest{
		Model:  p.model,
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.1, // Low temperature for deterministic extraction
			"num_predict": 200,
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/generate", bytes.NewBuffer(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	// Extract JSON from response
	content := strings.TrimSpace(ollamaResp.Response)
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	
	if jsonStart == -1 || jsonEnd == -1 {
		log.Warn().Str("content", content).Msg("No JSON found in parameter extraction response")
		return map[string]interface{}{}, nil
	}
	jsonString := content[jsonStart : jsonEnd+1]

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(jsonString), &params); err != nil {
		log.Warn().Err(err).Str("json", jsonString).Msg("Failed to parse extracted parameters")
		return map[string]interface{}{}, nil
	}

	log.Debug().Interface("params", params).Str("query", query).Msg("Parameters extracted via Ollama")
	return params, nil
}

