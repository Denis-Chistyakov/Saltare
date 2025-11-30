package semantic

import (
	"sync"
)

// Package semantic provides semantic routing for tool discovery and intent matching.
// Uses LLM providers for intelligent query parsing and parameter extraction.
import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// LLMProvider interface for intent generation
type LLMProvider interface {
	GenerateIntent(ctx context.Context, query string) (*types.Intent, error)
	GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error)
	ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error)
	HealthCheck(ctx context.Context) error
}

// Router handles semantic routing and intent matching
type Router struct {
	manager     *toolkit.Manager
	llmProvider LLMProvider              // LLM provider for intent parsing (required)
	cache       map[string]*types.Intent // In-memory cache
	mu          sync.RWMutex
}

// NewRouter creates a new semantic router (LLM required for smart queries)
func NewRouter(manager *toolkit.Manager) *Router {
	return &Router{
		manager:     manager,
		llmProvider: nil,
		cache:       make(map[string]*types.Intent),
	}
}

// NewRouterWithLLM creates a new semantic router with LLM provider
func NewRouterWithLLM(manager *toolkit.Manager, llmProvider LLMProvider) *Router {
	return &Router{
		manager:     manager,
		llmProvider: llmProvider,
		cache:       make(map[string]*types.Intent),
	}
}

// Route finds the best tool for a given query using LLM
func (r *Router) Route(ctx context.Context, query string) (*types.Tool, error) {
	// LLM is required for semantic routing
	if r.llmProvider == nil {
		return nil, fmt.Errorf("LLM provider not configured - semantic routing unavailable")
	}

	// 1. Parse intent from query via LLM
	intent, err := r.ParseIntent(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse intent: %w", err)
	}

	// 2. Match tool based on intent
	tool, err := r.MatchTool(ctx, intent)
	if err != nil {
		return nil, fmt.Errorf("failed to match tool: %w", err)
	}

	log.Info().
		Str("query", query).
		Str("tool", tool.Name).
		Str("domain", intent.Domain).
		Float64("confidence", intent.Confidence).
		Msg("Tool matched via LLM")

	return tool, nil
}

// ParseIntent extracts intent from natural language query using LLM
func (r *Router) ParseIntent(ctx context.Context, query string) (*types.Intent, error) {
	if r.llmProvider == nil {
		return nil, fmt.Errorf("LLM provider not configured")
	}

	// Check in-memory cache
	r.mu.RLock()
	cached, ok := r.cache[query]
	r.mu.RUnlock()
	if ok {
		log.Debug().Str("query", query).Str("source", "cache").Msg("Intent cache hit")
		return cached, nil
	}

	// Get available tools context for LLM
	availableTools := r.getToolsContext()

	// Use LLM to parse intent with tools context
	intent, err := r.llmProvider.GenerateIntentWithContext(ctx, query, availableTools)
	if err != nil {
		return nil, fmt.Errorf("LLM intent parsing failed: %w", err)
	}

	// Store raw query for later use
	intent.RawQuery = query

	log.Debug().
		Str("query", query).
		Str("domain", intent.Domain).
		Str("action", intent.Action).
		Interface("filters", intent.Filters).
		Float64("confidence", intent.Confidence).
		Msg("Intent parsed via LLM")

	// Cache successful parse
	r.mu.Lock()
	r.cache[query] = intent
	r.mu.Unlock()

	return intent, nil
}

// getToolsContext builds a summary of available tools for LLM context
func (r *Router) getToolsContext() string {
	toolboxes := r.manager.ListToolboxes()
	if len(toolboxes) == 0 {
		return "No tools available"
	}

	var context strings.Builder
	context.WriteString("Available toolboxes and tools:\n")
	
	for _, tb := range toolboxes {
		context.WriteString(fmt.Sprintf("- %s: %s\n", tb.Name, tb.Description))
		for _, tool := range tb.Tools {
			context.WriteString(fmt.Sprintf("  • %s: %s\n", tool.Name, tool.Description))
		}
	}
	
	return context.String()
}

// ExtractParametersFromQuery uses LLM to extract tool parameters from natural language
func (r *Router) ExtractParametersFromQuery(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	if r.llmProvider == nil {
		return nil, fmt.Errorf("LLM provider not configured for parameter extraction")
	}

	params, err := r.llmProvider.ExtractParameters(ctx, query, toolSchema)
	if err != nil {
		return nil, fmt.Errorf("LLM parameter extraction failed: %w", err)
	}

	return params, nil
}

// MatchTool finds the best matching tool for an intent
func (r *Router) MatchTool(ctx context.Context, intent *types.Intent) (*types.Tool, error) {
	allTools := r.manager.ListAllTools()
	if len(allTools) == 0 {
		return nil, fmt.Errorf("no tools available")
	}

	// Match by domain from LLM intent
	if intent.Domain != "" {
		domainLower := strings.ToLower(intent.Domain)
		
		// 1. First try to match by toolbox name/tags
		toolboxes := r.manager.ListToolboxes()
		for _, tb := range toolboxes {
			tbNameLower := strings.ToLower(tb.Name)
			
			// Check if toolbox name matches domain
			if tbNameLower == domainLower || strings.Contains(tbNameLower, domainLower) || strings.Contains(domainLower, tbNameLower) {
				if len(tb.Tools) > 0 {
					log.Debug().
						Str("toolbox", tb.Name).
						Str("domain", intent.Domain).
						Msg("Matched toolbox by domain")
					return tb.Tools[0], nil
				}
			}
			
			// Check toolbox tags
			for _, tag := range tb.Tags {
				if strings.ToLower(tag) == domainLower || strings.Contains(domainLower, strings.ToLower(tag)) {
					if len(tb.Tools) > 0 {
						log.Debug().
							Str("toolbox", tb.Name).
							Str("tag", tag).
							Msg("Matched toolbox by tag")
						return tb.Tools[0], nil
					}
				}
			}
		}
		
		// 2. Try to match by tool name or description
		for _, tool := range allTools {
			toolNameLower := strings.ToLower(tool.Name)
			toolDescLower := strings.ToLower(tool.Description)
			
			// Check tool name
			if strings.Contains(toolNameLower, domainLower) || strings.Contains(domainLower, toolNameLower) {
				log.Debug().
					Str("tool", tool.Name).
					Str("domain", intent.Domain).
					Msg("Matched tool by name")
				return tool, nil
			}
			
			// Check tool description
			if strings.Contains(toolDescLower, domainLower) {
				log.Debug().
					Str("tool", tool.Name).
					Str("domain", intent.Domain).
					Msg("Matched tool by description")
				return tool, nil
			}
		}
		
		// 3. Use SearchTools for semantic search
		searchResults := r.manager.SearchTools(intent.Domain, []string{})
		if len(searchResults) > 0 {
			log.Debug().
				Str("tool", searchResults[0].Name).
				Str("query", intent.Domain).
				Msg("Matched tool by search")
			return searchResults[0], nil
		}
	}

	// No match found
	return nil, fmt.Errorf("no tool found for query: %s", intent.RawQuery)
}

// ClearCache clears the intent cache
func (r *Router) ClearCache() {
	r.mu.Lock()
	r.cache = make(map[string]*types.Intent)
	r.mu.Unlock()
	log.Info().Msg("Intent cache cleared")
}

// GetCacheSize returns the current cache size
func (r *Router) GetCacheSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}
