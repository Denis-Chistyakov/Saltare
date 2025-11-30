package providers

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// FallbackProvider tries primary LLM, falls back to secondary
type FallbackProvider struct {
	primary   LLMProvider
	fallback  LLMProvider
	useFallback bool
}

// LLMProvider interface (for type safety)
type LLMProvider interface {
	GenerateIntent(ctx context.Context, query string) (*types.Intent, error)
	GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error)
	ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error)
	HealthCheck(ctx context.Context) error
}

// NewFallbackProvider creates a provider with primary/fallback
func NewFallbackProvider(primary, fallback LLMProvider) *FallbackProvider {
	return &FallbackProvider{
		primary:  primary,
		fallback: fallback,
	}
}

// GenerateIntent tries primary, falls back to secondary
func (f *FallbackProvider) GenerateIntent(ctx context.Context, query string) (*types.Intent, error) {
	return f.GenerateIntentWithContext(ctx, query, "")
}

// GenerateIntentWithContext tries primary, falls back to secondary
func (f *FallbackProvider) GenerateIntentWithContext(ctx context.Context, query string, toolsContext string) (*types.Intent, error) {
	// Try primary first
	if !f.useFallback {
		intent, err := f.primary.GenerateIntentWithContext(ctx, query, toolsContext)
		if err == nil {
			return intent, nil
		}
		
		log.Warn().Err(err).Msg("Primary LLM failed, switching to fallback")
		f.useFallback = true
	}
	
	// Use fallback
	if f.fallback != nil {
		log.Info().Msg("Using fallback LLM provider")
		return f.fallback.GenerateIntentWithContext(ctx, query, toolsContext)
	}
	
	return nil, fmt.Errorf("all LLM providers failed")
}

// ExtractParameters tries primary, falls back to secondary
func (f *FallbackProvider) ExtractParameters(ctx context.Context, query string, toolSchema map[string]interface{}) (map[string]interface{}, error) {
	// Try primary first
	if !f.useFallback {
		params, err := f.primary.ExtractParameters(ctx, query, toolSchema)
		if err == nil {
			return params, nil
		}
		
		log.Warn().Err(err).Msg("Primary LLM parameter extraction failed, using fallback")
	}
	
	// Use fallback
	if f.fallback != nil {
		return f.fallback.ExtractParameters(ctx, query, toolSchema)
	}
	
	return nil, fmt.Errorf("all LLM providers failed")
}

// HealthCheck checks primary provider
func (f *FallbackProvider) HealthCheck(ctx context.Context) error {
	if f.useFallback && f.fallback != nil {
		return f.fallback.HealthCheck(ctx)
	}
	return f.primary.HealthCheck(ctx)
}

