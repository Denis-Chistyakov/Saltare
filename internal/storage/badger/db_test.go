package badger

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

func TestNewDB(t *testing.T) {
	tmpDir := t.TempDir()
	
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Fatal("Expected DB to be created")
	}

	if db.path != tmpDir {
		t.Errorf("Expected path %s, got %s", tmpDir, db.path)
	}
}

func TestSaveAndGetToolbox(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	
	toolbox := &types.Toolbox{
		ID:          "test-toolbox-1",
		Name:        "Test Toolbox",
		Version:     "1.0.0",
		Description: "A test toolbox",
		Tags:        []string{"test", "mock"},
		Tools: []*types.Tool{
			{
				ID:          "tool-1",
				Name:        "test_tool",
				Description: "A test tool",
				InputSchema: map[string]interface{}{},
				MCPServer:   "http://localhost:8082",
			},
		},
	}

	// Save toolbox
	err = db.SaveToolbox(ctx, toolbox)
	if err != nil {
		t.Fatalf("Failed to save toolbox: %v", err)
	}

	// Get toolbox
	retrieved, err := db.GetToolbox(ctx, "test-toolbox-1")
	if err != nil {
		t.Fatalf("Failed to get toolbox: %v", err)
	}

	if retrieved.ID != toolbox.ID {
		t.Errorf("Expected ID %s, got %s", toolbox.ID, retrieved.ID)
	}

	if retrieved.Name != toolbox.Name {
		t.Errorf("Expected name %s, got %s", toolbox.Name, retrieved.Name)
	}

	if len(retrieved.Tools) != len(toolbox.Tools) {
		t.Errorf("Expected %d tools, got %d", len(toolbox.Tools), len(retrieved.Tools))
	}
}

func TestGetToolbox_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	
	_, err = db.GetToolbox(ctx, "non-existent")
	if err == nil {
		t.Fatal("Expected error for non-existent toolbox")
	}
}

func TestListToolboxes(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Save multiple toolboxes
	toolboxes := []*types.Toolbox{
		{
			ID:      "toolbox-1",
			Name:    "Toolbox 1",
			Version: "1.0.0",
			Tools:   []*types.Tool{},
		},
		{
			ID:      "toolbox-2",
			Name:    "Toolbox 2",
			Version: "2.0.0",
			Tools:   []*types.Tool{},
		},
		{
			ID:      "toolbox-3",
			Name:    "Toolbox 3",
			Version: "3.0.0",
			Tools:   []*types.Tool{},
		},
	}

	for _, tb := range toolboxes {
		if err := db.SaveToolbox(ctx, tb); err != nil {
			t.Fatalf("Failed to save toolbox: %v", err)
		}
	}

	// List all toolboxes
	list, err := db.ListToolboxes(ctx)
	if err != nil {
		t.Fatalf("Failed to list toolboxes: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 toolboxes, got %d", len(list))
	}
}

func TestDeleteToolbox(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	toolbox := &types.Toolbox{
		ID:      "delete-test",
		Name:    "Delete Test",
		Version: "1.0.0",
		Tools:   []*types.Tool{},
	}

	// Save
	if err := db.SaveToolbox(ctx, toolbox); err != nil {
		t.Fatalf("Failed to save toolbox: %v", err)
	}

	// Verify exists
	_, err = db.GetToolbox(ctx, "delete-test")
	if err != nil {
		t.Fatal("Toolbox should exist")
	}

	// Delete
	if err := db.DeleteToolbox(ctx, "delete-test"); err != nil {
		t.Fatalf("Failed to delete toolbox: %v", err)
	}

	// Verify deleted
	_, err = db.GetToolbox(ctx, "delete-test")
	if err == nil {
		t.Fatal("Toolbox should be deleted")
	}
}

func TestSaveCallEvent(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	event := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		UserID:     "user-456",
		Timestamp:  time.Now(),
		Duration:   150 * time.Millisecond,
		TokensUsed: 250,
		Success:    true,
	}

	// Save event (dormant feature - just test it doesn't error)
	err = db.SaveCallEvent(ctx, event)
	if err != nil {
		t.Fatalf("Failed to save call event: %v", err)
	}
}

func TestGetStats(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	stats := db.GetStats()
	
	if stats["path"] != tmpDir {
		t.Errorf("Expected path %s, got %v", tmpDir, stats["path"])
	}

	if _, ok := stats["lsm_size"]; !ok {
		t.Error("Expected lsm_size in stats")
	}

	if _, ok := stats["vlog_size"]; !ok {
		t.Error("Expected vlog_size in stats")
	}

	if _, ok := stats["total_size"]; !ok {
		t.Error("Expected total_size in stats")
	}
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	
	// First session: save data
	{
		db, err := NewDB(tmpDir)
		if err != nil {
			t.Fatalf("Failed to create DB: %v", err)
		}

		toolbox := &types.Toolbox{
			ID:      "persist-test",
			Name:    "Persistence Test",
			Version: "1.0.0",
			Tools:   []*types.Tool{},
		}

		if err := db.SaveToolbox(context.Background(), toolbox); err != nil {
			t.Fatalf("Failed to save toolbox: %v", err)
		}

		db.Close()
	}

	// Second session: verify data persisted
	{
		db, err := NewDB(tmpDir)
		if err != nil {
			t.Fatalf("Failed to reopen DB: %v", err)
		}
		defer db.Close()

		toolbox, err := db.GetToolbox(context.Background(), "persist-test")
		if err != nil {
			t.Fatalf("Failed to get persisted toolbox: %v", err)
		}

		if toolbox.Name != "Persistence Test" {
			t.Errorf("Data not persisted correctly, got name: %s", toolbox.Name)
		}
	}
}

func TestRunGC(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// Run GC (should not error even if no cleanup needed)
	err = db.RunGC()
	if err != nil {
		t.Errorf("GC failed: %v", err)
	}
}

func BenchmarkSaveToolbox(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "badger-bench")
	defer os.RemoveAll(tmpDir)

	db, _ := NewDB(tmpDir)
	defer db.Close()

	toolbox := &types.Toolbox{
		ID:      "bench-toolbox",
		Name:    "Benchmark Toolbox",
		Version: "1.0.0",
		Tools:   []*types.Tool{},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.SaveToolbox(ctx, toolbox)
	}
}

func BenchmarkGetToolbox(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "badger-bench")
	defer os.RemoveAll(tmpDir)

	db, _ := NewDB(tmpDir)
	defer db.Close()

	toolbox := &types.Toolbox{
		ID:      "bench-toolbox",
		Name:    "Benchmark Toolbox",
		Version: "1.0.0",
		Tools:   []*types.Tool{},
	}

	ctx := context.Background()
	db.SaveToolbox(ctx, toolbox)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.GetToolbox(ctx, "bench-toolbox")
	}
}

