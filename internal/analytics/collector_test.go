package analytics

import (
	"testing"
	"time"

	"github.com/Denis-Chistyakov/Saltare/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestNewCollector(t *testing.T) {
	collector := NewCollector(true, false)
	assert.NotNil(t, collector)
	assert.True(t, collector.enabled)
	assert.False(t, collector.federation)
	assert.NotNil(t, collector.stats)
}

func TestNewCollector_Disabled(t *testing.T) {
	collector := NewCollector(false, false)
	assert.NotNil(t, collector)
	assert.False(t, collector.enabled)
}

func TestRecordCall(t *testing.T) {
	collector := NewCollector(true, false)

	event := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		UserID:     "user-456",
		Timestamp:  time.Now(),
		Duration:   100 * time.Millisecond,
		TokensUsed: 50,
		Success:    true,
		Metadata: map[string]interface{}{
			"toolbox_id": "toolbox-789",
		},
	}

	collector.RecordCall(event)

	stats := collector.GetStats()
	assert.Equal(t, int64(1), stats.TotalCalls)
	assert.Equal(t, int64(50), stats.TotalTokens)
	assert.Equal(t, int64(0), stats.TotalErrors)
	assert.Equal(t, float64(100), stats.AvgLatencyMs)
	assert.Equal(t, int64(1), stats.CallsByTool["tool-123"])
	assert.Equal(t, int64(1), stats.CallsByToolbox["toolbox-789"])
}

func TestRecordCall_MultipleCalls(t *testing.T) {
	collector := NewCollector(true, false)

	// First call
	event1 := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		Duration:   100 * time.Millisecond,
		TokensUsed: 50,
		Success:    true,
		Metadata:   map[string]interface{}{"toolbox_id": "toolbox-1"},
	}
	collector.RecordCall(event1)

	// Second call
	event2 := &types.CallEvent{
		ID:         "event-2",
		ToolID:     "tool-123",
		Duration:   200 * time.Millisecond,
		TokensUsed: 100,
		Success:    true,
		Metadata:   map[string]interface{}{"toolbox_id": "toolbox-1"},
	}
	collector.RecordCall(event2)

	stats := collector.GetStats()
	assert.Equal(t, int64(2), stats.TotalCalls)
	assert.Equal(t, int64(150), stats.TotalTokens)
	assert.Equal(t, float64(150), stats.AvgLatencyMs) // (100+200)/2
	assert.Equal(t, int64(2), stats.CallsByTool["tool-123"])
}

func TestRecordCall_WithError(t *testing.T) {
	collector := NewCollector(true, false)

	event := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		Duration:   50 * time.Millisecond,
		TokensUsed: 0,
		Success:    false,
		Error:      "execution failed",
		Metadata:   map[string]interface{}{},
	}

	collector.RecordCall(event)

	stats := collector.GetStats()
	assert.Equal(t, int64(1), stats.TotalCalls)
	assert.Equal(t, int64(1), stats.TotalErrors)
	assert.Equal(t, int64(0), stats.TotalTokens)
}

func TestRecordCall_Disabled(t *testing.T) {
	collector := NewCollector(false, false)

	event := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		Duration:   100 * time.Millisecond,
		TokensUsed: 50,
		Success:    true,
	}

	collector.RecordCall(event)

	stats := collector.GetStats()
	assert.Equal(t, int64(0), stats.TotalCalls) // Should not record if disabled
}

func TestStartCall_EndCall(t *testing.T) {
	collector := NewCollector(true, false)

	collector.StartCall()
	// Active calls gauge should increase (tested via Prometheus metrics)

	collector.EndCall()
	// Active calls gauge should decrease
}

func TestGetStats(t *testing.T) {
	collector := NewCollector(true, false)

	// Record some events
	for i := 0; i < 5; i++ {
		event := &types.CallEvent{
			ID:         "event-" + string(rune(i)),
			ToolID:     "tool-123",
			Duration:   100 * time.Millisecond,
			TokensUsed: 10,
			Success:    true,
			Metadata:   map[string]interface{}{"toolbox_id": "toolbox-1"},
		}
		collector.RecordCall(event)
	}

	stats := collector.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(5), stats.TotalCalls)
	assert.Equal(t, int64(50), stats.TotalTokens)
	assert.NotNil(t, stats.CallsByTool)
	assert.NotNil(t, stats.CallsByToolbox)
	assert.NotNil(t, stats.TopTools)
}

func TestComputeTopTools(t *testing.T) {
	collector := NewCollector(true, false)

	// Record calls for different tools
	for i := 0; i < 10; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:       "event-1",
			ToolID:   "tool-A",
			Duration: 10 * time.Millisecond,
			Success:  true,
		})
	}

	for i := 0; i < 5; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:       "event-2",
			ToolID:   "tool-B",
			Duration: 10 * time.Millisecond,
			Success:  true,
		})
	}

	for i := 0; i < 15; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:       "event-3",
			ToolID:   "tool-C",
			Duration: 10 * time.Millisecond,
			Success:  true,
		})
	}

	stats := collector.GetStats()
	assert.Len(t, stats.TopTools, 3)
	assert.Equal(t, "tool-C", stats.TopTools[0].ToolID) // Most calls
	assert.Equal(t, int64(15), stats.TopTools[0].Calls)
	assert.Equal(t, "tool-A", stats.TopTools[1].ToolID)
	assert.Equal(t, int64(10), stats.TopTools[1].Calls)
}

func TestComputeTopToolboxes(t *testing.T) {
	collector := NewCollector(true, false)

	// Record calls for different toolboxes
	for i := 0; i < 7; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:       "event-1",
			ToolID:   "tool-1",
			Duration: 10 * time.Millisecond,
			Success:  true,
			Metadata: map[string]interface{}{"toolbox_id": "toolbox-A"},
		})
	}

	for i := 0; i < 12; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:       "event-2",
			ToolID:   "tool-2",
			Duration: 10 * time.Millisecond,
			Success:  true,
			Metadata: map[string]interface{}{"toolbox_id": "toolbox-B"},
		})
	}

	stats := collector.GetStats()
	assert.Len(t, stats.TopToolboxes, 2)
	assert.Equal(t, "toolbox-B", stats.TopToolboxes[0].ToolboxID)
	assert.Equal(t, int64(12), stats.TopToolboxes[0].Calls)
}

func TestReset(t *testing.T) {
	collector := NewCollector(true, false)

	// Record some events
	for i := 0; i < 5; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:         "event-" + string(rune(i)),
			ToolID:     "tool-123",
			Duration:   100 * time.Millisecond,
			TokensUsed: 10,
			Success:    true,
		})
	}

	stats := collector.GetStats()
	assert.Equal(t, int64(5), stats.TotalCalls)

	// Reset
	collector.Reset()

	stats = collector.GetStats()
	assert.Equal(t, int64(0), stats.TotalCalls)
	assert.Equal(t, int64(0), stats.TotalTokens)
	assert.Empty(t, stats.CallsByTool)
}

func TestThreadSafety(t *testing.T) {
	collector := NewCollector(true, false)

	// Simulate concurrent calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				event := &types.CallEvent{
					ID:         "event-" + string(rune(id)) + "-" + string(rune(j)),
					ToolID:     "tool-123",
					Duration:   10 * time.Millisecond,
					TokensUsed: 1,
					Success:    true,
				}
				collector.RecordCall(event)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	stats := collector.GetStats()
	assert.Equal(t, int64(1000), stats.TotalCalls)
	assert.Equal(t, int64(1000), stats.TotalTokens)
}

// BenchmarkRecordCall benchmarks call recording
func BenchmarkRecordCall(b *testing.B) {
	collector := NewCollector(true, false)
	event := &types.CallEvent{
		ID:         "event-1",
		ToolID:     "tool-123",
		Duration:   100 * time.Millisecond,
		TokensUsed: 50,
		Success:    true,
		Metadata:   map[string]interface{}{"toolbox_id": "toolbox-1"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordCall(event)
	}
}

// BenchmarkGetStats benchmarks stats retrieval
func BenchmarkGetStats(b *testing.B) {
	collector := NewCollector(true, false)

	// Pre-populate with events
	for i := 0; i < 1000; i++ {
		collector.RecordCall(&types.CallEvent{
			ID:         "event-" + string(rune(i)),
			ToolID:     "tool-123",
			Duration:   100 * time.Millisecond,
			TokensUsed: 10,
			Success:    true,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.GetStats()
	}
}

