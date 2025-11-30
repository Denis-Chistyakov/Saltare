package providers

// Package providers implements LLM provider for Cerebras Cloud.
// Supports fast inference for intent parsing and parameter extraction.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/rs/zerolog/log"
)

// CerebrasProvider implements LLM-based intent parsing using Cerebras AI
type CerebrasProvider struct {
	client   *http.Client
	config   types.CerebrasConfig
	endpoint string
}

// NewCerebrasProvider creates a new Cerebras provider
func NewCerebrasProvider(apiKey, model, endpoint string) *CerebrasProvider {
	if endpoint == "" {
		endpoint = "https://api.cerebras.ai/v1/chat/completions"
	}
	if model == "" {
		model = "llama3.1-70b"
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableKeepAlives:   false,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	return &CerebrasProvider{
		client:   client,
		endpoint: endpoint,
		config: types.CerebrasConfig{
			APIKey:      apiKey,
			Model:       model,
			Endpoint:    endpoint,
			Temperature: 0.1,
			MaxTokens:   200,
			Timeout:     30 * time.Second,
			Retries:     3,
		},
	}
}

// GenerateIntent sends a query to Cerebras and parses the intent from the response
func (c *CerebrasProvider) GenerateIntent(ctx context.Context, query string) (*types.Intent, error) {
	return c.GenerateIntentWithContext(ctx, query, "")
}

// GenerateIntentWithContext sends a query to Cerebras with available tools context
func (c *CerebrasProvider) GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error) {
	systemPrompt, userPrompt := c.buildPromptWithContext(query, toolsContext)

	requestBody := map[string]interface{}{
		"model": c.config.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": c.config.Temperature,
		"max_tokens":  c.config.MaxTokens,
		"stream":      false,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var respBody []byte
	for i := 0; i < c.config.Retries; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewBuffer(bodyJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

		resp, err := c.client.Do(req)
		if err != nil {
			log.Warn().Err(err).Int("attempt", i+1).Msg("Cerebras request failed, retrying...")
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		defer resp.Body.Close()

		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			log.Error().Int("status", resp.StatusCode).Str("response", string(respBody)).Msg("Cerebras API error")
			if resp.StatusCode >= 500 { // Retry on server errors
				time.Sleep(time.Duration(i+1) * time.Second)
				continue
			}
			return nil, fmt.Errorf("Cerebras API error [%d]: %s", resp.StatusCode, string(respBody))
		}
		break // Success
	}

	var cerebrasResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &cerebrasResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if cerebrasResponse.Error != nil {
		return nil, fmt.Errorf("Cerebras API error: %s", cerebrasResponse.Error.Message)
	}

	if len(cerebrasResponse.Choices) == 0 {
		return nil, fmt.Errorf("no choices in Cerebras response")
	}

	content := cerebrasResponse.Choices[0].Message.Content

	// Extract JSON from response
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd < jsonStart {
		return nil, fmt.Errorf("could not extract JSON from response: %s", content)
	}
	jsonString := content[jsonStart : jsonEnd+1]

	var intent types.Intent
	if err := json.Unmarshal([]byte(jsonString), &intent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal intent JSON: %w", err)
	}

	return &intent, nil
}

// HealthCheck pings the Cerebras API to check availability
func (c *CerebrasProvider) HealthCheck(ctx context.Context) error {
	// Cerebras doesn't have a dedicated health endpoint, so we make a minimal request
	requestBody := map[string]interface{}{
		"model": c.config.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal health check request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Cerebras server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cerebras health check failed with status: %d", resp.StatusCode)
	}

	return nil
}

// GetModel returns the configured Cerebras model
func (c *CerebrasProvider) GetModel() string {
	return c.config.Model
}

// buildPrompt constructs the prompt for Cerebras to extract intent (legacy)
func (c *CerebrasProvider) buildPrompt(query string) (string, string) {
	return c.buildPromptWithContext(query, "")
}

// buildPromptWithContext constructs the prompt with available tools context
func (c *CerebrasProvider) buildPromptWithContext(query string, toolsContext string) (string, string) {
	systemPrompt := `You are an intent parser. Your task is to extract the action, domain, entity, and any relevant filters from a user's natural language query. Respond ONLY with a JSON object.

The domain should match one of the available toolboxes. Use the toolbox/tool names and descriptions to determine the correct domain.`

	if toolsContext != "" {
		systemPrompt += "\n\n" + toolsContext
	}

	systemPrompt += `

Example queries and responses:

User: What's the weather in San Francisco?
Response: {"action": "get", "domain": "weather", "entity": "current", "filters": {"city": "San Francisco"}}

User: What is 2 plus 2?
Response: {"action": "calculate", "domain": "math", "entity": "add", "filters": {"a": 2, "b": 2}}

User: Calculate 15 + 7
Response: {"action": "calculate", "domain": "math", "entity": "add", "filters": {"a": 15, "b": 7}}

Rules:
- Match domain to available toolbox names (e.g., "weather", "math", "github")
- Extract relevant parameters and put them in filters
- If you cannot determine a specific field, omit it or use an empty string/object
- Ensure the response is valid JSON`

	userPrompt := query

	return systemPrompt, userPrompt
}

// ExtractParameters uses LLM to extract tool parameters from natural language query
// This eliminates the need for hardcoded translation dictionaries
func (c *CerebrasProvider) ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	// Build schema-aware prompt
	schemaJSON, _ := json.Marshal(toolSchema)

	systemPrompt := `You are a parameter extraction expert. Given a natural language query and a tool's input schema, extract the parameters.
Respond ONLY with a JSON object containing the extracted parameters.

Rules:
- Extract values that match the schema properties
- Use property descriptions as hints
- Handle multiple languages (English, Russian, etc.)
- Return empty object {} if no parameters found
- DO NOT include extra fields not in schema`

	userPrompt := fmt.Sprintf(`Query: %s

Tool Input Schema:
%s

Extract the parameters as JSON:`, query, string(schemaJSON))

	requestBody := map[string]interface{}{
		"model": c.config.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  200,
		"temperature": 0.1, // Low temperature for deterministic extraction
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cerebras request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Cerebras API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode Cerebras response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in Cerebras response")
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)

	// Extract JSON from response
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart == -1 || jsonEnd == -1 {
		log.Warn().Str("content", content).Msg("No JSON found in parameter extraction response")
		return map[string]interface{}{}, nil // Return empty params
	}
	jsonString := content[jsonStart : jsonEnd+1]

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(jsonString), &params); err != nil {
		log.Warn().Err(err).Str("json", jsonString).Msg("Failed to parse extracted parameters")
		return map[string]interface{}{}, nil // Return empty params on error
	}

	log.Debug().Interface("params", params).Str("query", query).Msg("Parameters extracted via LLM")
	return params, nil
}
