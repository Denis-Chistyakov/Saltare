package analytics

// Package analytics provides basic analytics collection for tool usage tracking.
// Collects call events, counters, and metrics for monitoring and observability.

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// Collector handles analytics collection and metrics
type Collector struct {
	// Prometheus metrics
	totalCalls       *prometheus.CounterVec
	totalTokens      *prometheus.CounterVec
	callLatency      *prometheus.HistogramVec
	errorRate        *prometheus.CounterVec
	activeCalls      prometheus.Gauge
	
	// In-memory stats (for dormant mode)
	stats      *Stats
	mu         sync.RWMutex
	enabled    bool
	federation bool
}

// Stats holds aggregated statistics
type Stats struct {
	TotalCalls     int64             `json:"total_calls"`
	TotalTokens    int64             `json:"total_tokens"`
	TotalErrors    int64             `json:"total_errors"`
	AvgLatencyMs   float64           `json:"avg_latency_ms"`
	TopTools       []ToolStats       `json:"top_tools"`
	TopToolboxes   []ToolboxStats    `json:"top_toolboxes"`
	CallsByTool    map[string]int64  `json:"calls_by_tool"`
	CallsByToolbox map[string]int64  `json:"calls_by_toolbox"`
	TokensByTool   map[string]int64  `json:"tokens_by_tool"`
	lastUpdate     time.Time
}

// ToolStats represents tool-level statistics
type ToolStats struct {
	ToolID string `json:"tool_id"`
	Calls  int64  `json:"calls"`
}

// ToolboxStats represents toolbox-level statistics
type ToolboxStats struct {
	ToolboxID string `json:"toolbox_id"`
	Calls     int64  `json:"calls"`
}

// NewCollector creates a new analytics collector
func NewCollector(enabled bool, federation bool) *Collector {
	collector := &Collector{
		enabled:    enabled,
		federation: federation,
		stats: &Stats{
			CallsByTool:    make(map[string]int64),
			CallsByToolbox: make(map[string]int64),
			TokensByTool:   make(map[string]int64),
			TopTools:       []ToolStats{},
			TopToolboxes:   []ToolboxStats{},
			lastUpdate:     time.Now(),
		},
	}

	if enabled {
		// Initialize Prometheus metrics (use promauto which handles registration)
		// These are safe for multiple collector instances
		collector.totalCalls = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "saltare_tool_calls_total",
				Help: "Total number of tool calls",
			},
			[]string{"tool_id", "toolbox_id", "status"},
		)

		collector.totalTokens = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "saltare_tokens_total",
				Help: "Total number of tokens used",
			},
			[]string{"tool_id"},
		)

		collector.callLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "saltare_call_latency_seconds",
				Help:    "Tool call latency in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"tool_id", "toolbox_id"},
		)

		collector.errorRate = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "saltare_errors_total",
				Help: "Total number of errors",
			},
			[]string{"tool_id", "error_type"},
		)

		collector.activeCalls = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "saltare_active_calls",
				Help: "Number of currently active calls",
			},
		)

		// Register metrics (ignore duplicate errors for tests)
		_ = prometheus.DefaultRegisterer.Register(collector.totalCalls)
		_ = prometheus.DefaultRegisterer.Register(collector.totalTokens)
		_ = prometheus.DefaultRegisterer.Register(collector.callLatency)
		_ = prometheus.DefaultRegisterer.Register(collector.errorRate)
		_ = prometheus.DefaultRegisterer.Register(collector.activeCalls)

		log.Info().
			Bool("enabled", enabled).
			Bool("federation", federation).
			Msg("Analytics collector initialized")
	} else {
		log.Info().Msg("Analytics collector disabled")
	}

	return collector
}

// RecordCall records a tool call event
func (c *Collector) RecordCall(event *types.CallEvent) {
	if !c.enabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Extract toolbox from metadata if available
	toolboxID := "unknown"
	if tbID, ok := event.Metadata["toolbox_id"].(string); ok {
		toolboxID = tbID
	}

	// Calculate latency in ms from Duration
	latencyMs := event.Duration.Milliseconds()

	// Update in-memory stats
	c.stats.TotalCalls++
	c.stats.CallsByTool[event.ToolID]++
	c.stats.CallsByToolbox[toolboxID]++
	c.stats.TokensByTool[event.ToolID] += int64(event.TokensUsed)
	c.stats.TotalTokens += int64(event.TokensUsed)

	if !event.Success {
		c.stats.TotalErrors++
	}

	// Update average latency (running average)
	if c.stats.TotalCalls > 0 {
		c.stats.AvgLatencyMs = (c.stats.AvgLatencyMs*float64(c.stats.TotalCalls-1) + float64(latencyMs)) / float64(c.stats.TotalCalls)
	}

	// Update Prometheus metrics
	status := "success"
	if !event.Success {
		status = "error"
	}

	c.totalCalls.WithLabelValues(event.ToolID, toolboxID, status).Inc()
	c.totalTokens.WithLabelValues(event.ToolID).Add(float64(event.TokensUsed))
	c.callLatency.WithLabelValues(event.ToolID, toolboxID).Observe(float64(latencyMs) / 1000.0)

	if !event.Success {
		errorType := "unknown"
		if event.Error != "" {
			errorType = "execution_error"
		}
		c.errorRate.WithLabelValues(event.ToolID, errorType).Inc()
	}

	log.Debug().
		Str("tool_id", event.ToolID).
		Str("toolbox_id", toolboxID).
		Int64("latency_ms", latencyMs).
		Bool("success", event.Success).
		Msg("Call event recorded")
}

// StartCall increments active calls counter
func (c *Collector) StartCall() {
	if !c.enabled {
		return
	}
	c.activeCalls.Inc()
}

// EndCall decrements active calls counter
func (c *Collector) EndCall() {
	if !c.enabled {
		return
	}
	c.activeCalls.Dec()
}

// GetStats returns current statistics
func (c *Collector) GetStats() *Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Deep copy to avoid race conditions
	statsCopy := &Stats{
		TotalCalls:     c.stats.TotalCalls,
		TotalTokens:    c.stats.TotalTokens,
		TotalErrors:    c.stats.TotalErrors,
		AvgLatencyMs:   c.stats.AvgLatencyMs,
		CallsByTool:    make(map[string]int64),
		CallsByToolbox: make(map[string]int64),
		TokensByTool:   make(map[string]int64),
		lastUpdate:     c.stats.lastUpdate,
	}

	for k, v := range c.stats.CallsByTool {
		statsCopy.CallsByTool[k] = v
	}
	for k, v := range c.stats.CallsByToolbox {
		statsCopy.CallsByToolbox[k] = v
	}
	for k, v := range c.stats.TokensByTool {
		statsCopy.TokensByTool[k] = v
	}

	// Compute top tools
	statsCopy.TopTools = c.computeTopTools()
	statsCopy.TopToolboxes = c.computeTopToolboxes()

	return statsCopy
}

// computeTopTools returns top 10 tools by call count
func (c *Collector) computeTopTools() []ToolStats {
	var tools []ToolStats
	for toolID, calls := range c.stats.CallsByTool {
		tools = append(tools, ToolStats{ToolID: toolID, Calls: calls})
	}

	// Sort by calls (descending)
	for i := 0; i < len(tools); i++ {
		for j := i + 1; j < len(tools); j++ {
			if tools[j].Calls > tools[i].Calls {
				tools[i], tools[j] = tools[j], tools[i]
			}
		}
	}

	// Return top 10
	if len(tools) > 10 {
		tools = tools[:10]
	}

	return tools
}

// computeTopToolboxes returns top 10 toolboxes by call count
func (c *Collector) computeTopToolboxes() []ToolboxStats {
	var toolboxes []ToolboxStats
	for toolboxID, calls := range c.stats.CallsByToolbox {
		toolboxes = append(toolboxes, ToolboxStats{ToolboxID: toolboxID, Calls: calls})
	}

	// Sort by calls (descending)
	for i := 0; i < len(toolboxes); i++ {
		for j := i + 1; j < len(toolboxes); j++ {
			if toolboxes[j].Calls > toolboxes[i].Calls {
				toolboxes[i], toolboxes[j] = toolboxes[j], toolboxes[i]
			}
		}
	}

	// Return top 10
	if len(toolboxes) > 10 {
		toolboxes = toolboxes[:10]
	}

	return toolboxes
}

// Reset resets all statistics
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats = &Stats{
		CallsByTool:    make(map[string]int64),
		CallsByToolbox: make(map[string]int64),
		TokensByTool:   make(map[string]int64),
		TopTools:       []ToolStats{},
		TopToolboxes:   []ToolboxStats{},
		lastUpdate:     time.Now(),
	}

	log.Info().Msg("Analytics statistics reset")
}

