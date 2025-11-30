package search

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// ProviderType represents the search provider type
type ProviderType string

const (
	ProviderTypesense    ProviderType = "typesense"
	ProviderMeilisearch  ProviderType = "meilisearch"
)

// ProviderFactory creates search providers
type ProviderFactory struct {
	typesenseConfig    types.TypesenseConfig
	meilisearchConfig  types.MeilisearchConfig
	defaultProvider    ProviderType
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory(storageConfig types.StorageConfig) *ProviderFactory {
	// Determine default provider
	defaultProvider := ProviderTypesense
	if storageConfig.Search.Provider != "" {
		switch storageConfig.Search.Provider {
		case "meilisearch":
			defaultProvider = ProviderMeilisearch
		case "typesense":
			defaultProvider = ProviderTypesense
		default:
			log.Warn().
				Str("provider", storageConfig.Search.Provider).
				Msg("Unknown search provider, falling back to typesense")
		}
	}

	return &ProviderFactory{
		typesenseConfig:   storageConfig.Typesense,
		meilisearchConfig: storageConfig.Search.Meilisearch,
		defaultProvider:   defaultProvider,
	}
}

// CreateProvider creates a search provider based on configuration
// This is called by external code that imports meilisearch and typesense packages
func (f *ProviderFactory) GetConfig() (ProviderType, types.TypesenseConfig, types.MeilisearchConfig) {
	return f.defaultProvider, f.typesenseConfig, f.meilisearchConfig
}

// DefaultProvider returns the configured default provider type
func (f *ProviderFactory) DefaultProvider() ProviderType {
	return f.defaultProvider
}

// TypesenseConfig returns the Typesense configuration
func (f *ProviderFactory) TypesenseConfig() types.TypesenseConfig {
	return f.typesenseConfig
}

// MeilisearchConfig returns the Meilisearch configuration
func (f *ProviderFactory) MeilisearchConfig() types.MeilisearchConfig {
	return f.meilisearchConfig
}

// IsTypesenseEnabled returns true if Typesense is enabled
func (f *ProviderFactory) IsTypesenseEnabled() bool {
	return f.typesenseConfig.Enabled
}

// IsMeilisearchEnabled returns true if Meilisearch is enabled
func (f *ProviderFactory) IsMeilisearchEnabled() bool {
	return f.meilisearchConfig.Enabled
}

// Validate validates the configuration
func (f *ProviderFactory) Validate() error {
	switch f.defaultProvider {
	case ProviderMeilisearch:
		if !f.meilisearchConfig.Enabled {
			return fmt.Errorf("meilisearch is selected as provider but is not enabled")
		}
		if f.meilisearchConfig.Host == "" {
			return fmt.Errorf("meilisearch host is required")
		}
	case ProviderTypesense:
		if !f.typesenseConfig.Enabled {
			return fmt.Errorf("typesense is selected as provider but is not enabled")
		}
		if len(f.typesenseConfig.Nodes) == 0 {
			return fmt.Errorf("typesense nodes are required")
		}
	}
	return nil
}

// LogConfig logs the current search configuration
func (f *ProviderFactory) LogConfig() {
	log.Info().
		Str("default_provider", string(f.defaultProvider)).
		Bool("typesense_enabled", f.typesenseConfig.Enabled).
		Bool("meilisearch_enabled", f.meilisearchConfig.Enabled).
		Bool("meilisearch_hybrid", f.meilisearchConfig.HybridSearch.Enabled).
		Msg("Search provider configuration")
}

