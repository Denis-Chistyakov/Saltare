package toolkit

// Package toolkit provides the central registry for managing toolkits, toolboxes, and tools.
// It handles in-memory storage, persistence via BadgerDB, search indexing via Typesense,
// and provides thread-safe CRUD operations.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// SearchIndexer interface for search engine integration (Typesense)
type SearchIndexer interface {
	IndexToolbox(ctx context.Context, toolbox *types.Toolbox, toolkitID string) error
	DeleteToolbox(ctx context.Context, toolboxID string) error
}

// Storage interface for persistence (BadgerDB, etc.)
type Storage interface {
	SaveToolkit(ctx context.Context, toolkit *types.Toolkit) error
	GetToolkit(ctx context.Context, id string) (*types.Toolkit, error)
	ListToolkits(ctx context.Context) ([]*types.Toolkit, error)
	DeleteToolkit(ctx context.Context, id string) error
}

// Manager manages toolkits, toolboxes, and tools registry
// In-memory map is the primary store for fast access
// Storage (BadgerDB) is for persistence
// Typesense is for search indexing
type Manager struct {
	toolkits map[string]*types.Toolkit
	indexer  SearchIndexer // Optional: Typesense integration
	storage  Storage       // Optional: Persistence (BadgerDB)
	mu       sync.RWMutex

	// Statistics
	totalToolboxes int
	totalTools     int
}

// NewManager creates a new toolkit manager
func NewManager() *Manager {
	log.Info().Msg("Toolkit manager initialized")

	return &Manager{
		toolkits: make(map[string]*types.Toolkit),
	}
}

// SetSearchIndexer sets the search indexer (Typesense) for the manager
func (m *Manager) SetSearchIndexer(indexer SearchIndexer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexer = indexer
	log.Info().Msg("Search indexer configured for toolkit manager")
}

// SetStorage sets the storage backend (BadgerDB) for persistence
func (m *Manager) SetStorage(storage Storage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storage = storage
	log.Info().Msg("Storage backend configured for toolkit manager")
}

// LoadFromStorage loads all toolkits from persistent storage into memory
// Call this after SetStorage() during startup
func (m *Manager) LoadFromStorage(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.storage == nil {
		return fmt.Errorf("storage not configured")
	}

	toolkits, err := m.storage.ListToolkits(ctx)
	if err != nil {
		return fmt.Errorf("failed to load toolkits from storage: %w", err)
	}

	loaded := 0
	for _, tk := range toolkits {
		m.toolkits[tk.ID] = tk
		loaded++
	}

	m.updateStats()

	log.Info().
		Int("toolkits", loaded).
		Int("toolboxes", m.totalToolboxes).
		Int("tools", m.totalTools).
		Msg("Loaded toolkits from storage")

	return nil
}

// RegisterToolkit registers a new toolkit with global limit enforcement
func (m *Manager) RegisterToolkit(toolkit *types.Toolkit) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if toolkit.ID == "" {
		toolkit.ID = uuid.New().String()
	}

	// Count new toolboxes and tools
	newToolboxCount := len(toolkit.Toolboxes)
	newToolCount := 0
	for _, tb := range toolkit.Toolboxes {
		newToolCount += len(tb.Tools)
	}

	// Set timestamps
	now := time.Now()
	toolkit.CreatedAt = now
	toolkit.UpdatedAt = now

	// Generate IDs for toolboxes and tools
	for _, tb := range toolkit.Toolboxes {
		if tb.ID == "" {
			tb.ID = uuid.New().String()
		}
		tb.CreatedAt = now
		tb.UpdatedAt = now

		for _, tool := range tb.Tools {
			if tool.ID == "" {
				tool.ID = uuid.New().String()
			}
			tool.CreatedAt = now
		}
	}

	m.toolkits[toolkit.ID] = toolkit
	m.updateStats()

	// Persist to storage (async, non-blocking)
	if m.storage != nil {
		go func() {
			ctx := context.Background()
			if err := m.storage.SaveToolkit(ctx, toolkit); err != nil {
				log.Error().Err(err).Str("toolkit_id", toolkit.ID).Msg("Failed to persist toolkit")
			}
		}()
	}

	// Index toolboxes in search engine (async, non-blocking)
	if m.indexer != nil {
		go func() {
			ctx := context.Background()
			for _, tb := range toolkit.Toolboxes {
				if err := m.indexer.IndexToolbox(ctx, tb, toolkit.ID); err != nil {
					log.Error().Err(err).Str("toolbox_id", tb.ID).Msg("Failed to index toolbox")
				}
			}
		}()
	}

	log.Info().
		Str("toolkit_id", toolkit.ID).
		Int("toolboxes", newToolboxCount).
		Int("tools", newToolCount).
		Msg("Toolkit registered")

	return nil
}

// UnregisterToolkit removes a toolkit
func (m *Manager) UnregisterToolkit(toolkitID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	toolkit, exists := m.toolkits[toolkitID]
	if !exists {
		return fmt.Errorf("toolkit not found: %s", toolkitID)
	}

	// Count removed resources
	removedToolboxes := len(toolkit.Toolboxes)
	removedTools := 0
	toolboxIDs := make([]string, 0, len(toolkit.Toolboxes))
	for _, tb := range toolkit.Toolboxes {
		removedTools += len(tb.Tools)
		toolboxIDs = append(toolboxIDs, tb.ID)
	}

	delete(m.toolkits, toolkitID)
	m.updateStats()

	// Delete from storage (async, non-blocking)
	if m.storage != nil {
		go func() {
			ctx := context.Background()
			if err := m.storage.DeleteToolkit(ctx, toolkitID); err != nil {
				log.Error().Err(err).Str("toolkit_id", toolkitID).Msg("Failed to delete toolkit from storage")
			}
		}()
	}

	// Remove from search index (async, non-blocking)
	if m.indexer != nil {
		go func() {
			ctx := context.Background()
			for _, tbID := range toolboxIDs {
				if err := m.indexer.DeleteToolbox(ctx, tbID); err != nil {
					log.Error().Err(err).Str("toolbox_id", tbID).Msg("Failed to delete toolbox from index")
				}
			}
		}()
	}

	log.Info().
		Str("toolkit_id", toolkitID).
		Int("removed_toolboxes", removedToolboxes).
		Int("removed_tools", removedTools).
		Msg("Toolkit unregistered")

	return nil
}

// GetToolkit retrieves a toolkit by ID
func (m *Manager) GetToolkit(toolkitID string) (*types.Toolkit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolkit, exists := m.toolkits[toolkitID]
	if !exists {
		return nil, fmt.Errorf("toolkit not found: %s", toolkitID)
	}

	return toolkit, nil
}

// ListToolkits returns all registered toolkits
func (m *Manager) ListToolkits() []*types.Toolkit {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolkits := make([]*types.Toolkit, 0, len(m.toolkits))
	for _, tk := range m.toolkits {
		toolkits = append(toolkits, tk)
	}

	return toolkits
}

// ListAllTools returns all tools across all toolkits
func (m *Manager) ListAllTools() []*types.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make([]*types.Tool, 0)
	for _, tk := range m.toolkits {
		for _, tb := range tk.Toolboxes {
			tools = append(tools, tb.Tools...)
		}
	}

	return tools
}

// ListToolboxes returns all toolboxes across all toolkits
func (m *Manager) ListToolboxes() []*types.Toolbox {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolboxes := make([]*types.Toolbox, 0)
	for _, tk := range m.toolkits {
		toolboxes = append(toolboxes, tk.Toolboxes...)
	}

	return toolboxes
}

// SearchTools searches tools by query and filters
func (m *Manager) SearchTools(query string, tags []string) []*types.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*types.Tool
	queryLower := strings.ToLower(query)

	for _, tk := range m.toolkits {
		for _, tb := range tk.Toolboxes {
			// Filter by tags if specified
			if len(tags) > 0 && !hasAllTags(tb.Tags, tags) {
				continue
			}

			for _, tool := range tb.Tools {
				// Match by name or description
				if query == "" ||
					strings.Contains(strings.ToLower(tool.Name), queryLower) ||
					strings.Contains(strings.ToLower(tool.Description), queryLower) {
					results = append(results, tool)
				}
			}
		}
	}

	return results
}

// GetTool retrieves a tool by ID
func (m *Manager) GetTool(toolID string) (*types.Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, tk := range m.toolkits {
		for _, tb := range tk.Toolboxes {
			for _, tool := range tb.Tools {
				if tool.ID == toolID {
					return tool, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("tool not found: %s", toolID)
}

// GetToolByName retrieves a tool by name (format: toolbox.tool)
func (m *Manager) GetToolByName(name string) (*types.Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid tool name format: %s (expected toolbox.tool)", name)
	}

	toolboxName := parts[0]
	toolName := parts[1]

	for _, tk := range m.toolkits {
		for _, tb := range tk.Toolboxes {
			if tb.Name == toolboxName {
				for _, tool := range tb.Tools {
					if tool.Name == toolName {
						return tool, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("tool not found: %s", name)
}

// GetStats returns current usage statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"toolkits":  len(m.toolkits),
		"toolboxes": m.totalToolboxes,
		"tools":     m.totalTools,
	}
}

// updateStats updates internal statistics (must be called with lock held)
func (m *Manager) updateStats() {
	m.totalToolboxes = 0
	m.totalTools = 0

	for _, tk := range m.toolkits {
		m.totalToolboxes += len(tk.Toolboxes)
		for _, tb := range tk.Toolboxes {
			m.totalTools += len(tb.Tools)
		}
	}
}

// hasAllTags checks if a slice contains all required tags
func hasAllTags(haystack, needles []string) bool {
	if len(needles) == 0 {
		return true
	}

	haystackMap := make(map[string]bool)
	for _, tag := range haystack {
		haystackMap[tag] = true
	}

	for _, needle := range needles {
		if !haystackMap[needle] {
			return false
		}
	}

	return true
}

