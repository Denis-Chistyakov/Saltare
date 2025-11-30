package badger

// Package badger provides embedded key-value storage using BadgerDB.
// Handles toolkit persistence with async writes and background garbage collection.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// DB represents BadgerDB storage
type DB struct {
	db   *badgerdb.DB
	path string
}

// NewDB creates a new BadgerDB instance
func NewDB(path string) (*DB, error) {
	opts := badgerdb.DefaultOptions(path)
	opts.Logger = nil // Disable badger's internal logging (use our zerolog)
	
	// Performance tuning for Beta (single node)
	opts.ValueLogFileSize = 64 << 20 // 64MB value log files
	opts.NumVersionsToKeep = 1       // Keep only latest version
	opts.CompactL0OnClose = true     // Compact on close

	db, err := badgerdb.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	log.Info().
		Str("path", path).
		Msg("BadgerDB initialized")

	return &DB{
		db:   db,
		path: path,
	}, nil
}

// Close closes the database
func (d *DB) Close() error {
	log.Info().Msg("Closing BadgerDB")
	return d.db.Close()
}

// DB returns the underlying BadgerDB instance
// Used by other packages that need direct access (e.g., jobs package)
func (d *DB) DB() *badgerdb.DB {
	return d.db
}

// ═══════════════════════════════════════════════════════════════════════════════
// TOOLKIT OPERATIONS (full hierarchy: toolkit → toolboxes → tools)
// ═══════════════════════════════════════════════════════════════════════════════

// SaveToolkit saves a complete toolkit to the database
func (d *DB) SaveToolkit(ctx context.Context, toolkit *types.Toolkit) error {
	key := []byte("toolkit:" + toolkit.ID)
	
	data, err := json.Marshal(toolkit)
	if err != nil {
		return fmt.Errorf("failed to marshal toolkit: %w", err)
	}

	err = d.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(key, data)
	})

	if err != nil {
		return fmt.Errorf("failed to save toolkit: %w", err)
	}

	log.Debug().
		Str("id", toolkit.ID).
		Str("name", toolkit.Name).
		Int("toolboxes", len(toolkit.Toolboxes)).
		Msg("Toolkit saved to BadgerDB")

	return nil
}

// GetToolkit retrieves a toolkit by ID
func (d *DB) GetToolkit(ctx context.Context, id string) (*types.Toolkit, error) {
	key := []byte("toolkit:" + id)
	
	var toolkit types.Toolkit
	err := d.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &toolkit)
		})
	})

	if err == badgerdb.ErrKeyNotFound {
		return nil, fmt.Errorf("toolkit not found: %s", id)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get toolkit: %w", err)
	}

	return &toolkit, nil
}

// ListToolkits returns all toolkits from the database
func (d *DB) ListToolkits(ctx context.Context) ([]*types.Toolkit, error) {
	var toolkits []*types.Toolkit
	prefix := []byte("toolkit:")

	err := d.db.View(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			
			err := item.Value(func(val []byte) error {
				var toolkit types.Toolkit
				if err := json.Unmarshal(val, &toolkit); err != nil {
					return err
				}
				toolkits = append(toolkits, &toolkit)
				return nil
			})

			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list toolkits: %w", err)
	}

	log.Debug().
		Int("count", len(toolkits)).
		Msg("Listed toolkits from BadgerDB")

	return toolkits, nil
}

// DeleteToolkit deletes a toolkit from the database
func (d *DB) DeleteToolkit(ctx context.Context, id string) error {
	key := []byte("toolkit:" + id)

	err := d.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(key)
	})

	if err != nil {
		return fmt.Errorf("failed to delete toolkit: %w", err)
	}

	log.Debug().
		Str("id", id).
		Msg("Toolkit deleted from BadgerDB")

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// TOOLBOX OPERATIONS (for backward compatibility / individual operations)
// ═══════════════════════════════════════════════════════════════════════════════

// SaveToolbox saves a toolbox to the database
func (d *DB) SaveToolbox(ctx context.Context, toolbox *types.Toolbox) error {
	key := []byte("toolbox:" + toolbox.ID)
	
	data, err := json.Marshal(toolbox)
	if err != nil {
		return fmt.Errorf("failed to marshal toolbox: %w", err)
	}

	err = d.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(key, data)
	})

	if err != nil {
		return fmt.Errorf("failed to save toolbox: %w", err)
	}

	log.Debug().
		Str("id", toolbox.ID).
		Str("name", toolbox.Name).
		Msg("Toolbox saved")

	return nil
}

// GetToolbox retrieves a toolbox by ID
func (d *DB) GetToolbox(ctx context.Context, id string) (*types.Toolbox, error) {
	key := []byte("toolbox:" + id)
	
	var toolbox types.Toolbox
	err := d.db.View(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &toolbox)
		})
	})

	if err == badgerdb.ErrKeyNotFound {
		return nil, fmt.Errorf("toolbox not found: %s", id)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get toolbox: %w", err)
	}

	return &toolbox, nil
}

// ListToolboxes returns all toolboxes
func (d *DB) ListToolboxes(ctx context.Context) ([]*types.Toolbox, error) {
	var toolboxes []*types.Toolbox
	prefix := []byte("toolbox:")

	err := d.db.View(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			
			err := item.Value(func(val []byte) error {
				var toolbox types.Toolbox
				if err := json.Unmarshal(val, &toolbox); err != nil {
					return err
				}
				toolboxes = append(toolboxes, &toolbox)
				return nil
			})

			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list toolboxes: %w", err)
	}

	log.Debug().
		Int("count", len(toolboxes)).
		Msg("Listed toolboxes")

	return toolboxes, nil
}

// DeleteToolbox deletes a toolbox
func (d *DB) DeleteToolbox(ctx context.Context, id string) error {
	key := []byte("toolbox:" + id)

	err := d.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Delete(key)
	})

	if err != nil {
		return fmt.Errorf("failed to delete toolbox: %w", err)
	}

	return nil
}

// SaveCallEvent saves an analytics event
func (d *DB) SaveCallEvent(ctx context.Context, event *types.CallEvent) error {
	// Format: event:YYYY-MM-DD:timestamp:id
	key := []byte(fmt.Sprintf("event:%s:%d:%s", 
		time.Now().Format("2006-01-02"),
		time.Now().Unix(),
		event.ID,
	))

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	err = d.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(key, data)
	})

	if err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Silent logging (dormant feature)
	log.Trace().
		Str("id", event.ID).
		Str("tool", event.ToolID).
		Msg("Call event saved (dormant)")

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// UTILITIES
// ═══════════════════════════════════════════════════════════════════════════════

// GetStats returns database statistics
func (d *DB) GetStats() map[string]interface{} {
	lsm, vlog := d.db.Size()
	
	return map[string]interface{}{
		"path":       d.path,
		"lsm_size":   lsm,
		"vlog_size":  vlog,
		"total_size": lsm + vlog,
	}
}

// RunGC triggers garbage collection
func (d *DB) RunGC() error {
	log.Debug().Msg("Running BadgerDB GC")
	
	err := d.db.RunValueLogGC(0.5)
	if err != nil && err != badgerdb.ErrNoRewrite {
		return fmt.Errorf("gc failed: %w", err)
	}

	return nil
}

// Backup creates a backup of the database (Phase 4)
func (d *DB) Backup(ctx context.Context, path string) error {
	return fmt.Errorf("backup not implemented yet")
}

// Restore restores database from backup (Phase 4)
func (d *DB) Restore(ctx context.Context, path string) error {
	return fmt.Errorf("restore not implemented yet")
}
