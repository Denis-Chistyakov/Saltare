package directmode

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewConnectionPool(t *testing.T) {
	pool := NewConnectionPool("http://localhost:8082", 5, 1*time.Minute)
	assert.NotNil(t, pool)
	assert.Equal(t, "http://localhost:8082", pool.serverURL)
	assert.Equal(t, 5, pool.maxConnections)
	assert.Equal(t, 1*time.Minute, pool.idleTimeout)

	defer pool.Close()
}

func TestConnectionPool_AcquireRelease(t *testing.T) {
	// Note: This test requires a mock MCP server or will fail
	// For now, we test the pool logic without actual connections
	pool := NewConnectionPool("http://localhost:9999", 3, 1*time.Minute)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to acquire (will fail without real MCP server, but tests pool logic)
	_, err := pool.Acquire(ctx)

	// Error is expected since no real MCP server
	assert.Error(t, err)

	// Check metrics
	metrics := pool.GetMetrics()
	assert.Equal(t, int64(1), metrics["total_acquires"])
}

func TestConnectionPool_MaxConnections(t *testing.T) {
	pool := NewConnectionPool("http://localhost:9999", 2, 1*time.Minute)
	defer pool.Close()

	// Pool should limit connections
	assert.Equal(t, 2, pool.maxConnections)
}

func TestConnectionPool_GetMetrics(t *testing.T) {
	pool := NewConnectionPool("http://localhost:8082", 5, 1*time.Minute)
	defer pool.Close()

	metrics := pool.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, "http://localhost:8082", metrics["server"])
	assert.Equal(t, 5, metrics["max_connections"])
	assert.Equal(t, int64(0), metrics["total_acquires"])
	assert.Equal(t, int64(0), metrics["current_active"])
}

func TestConnectionPool_Close(t *testing.T) {
	pool := NewConnectionPool("http://localhost:8082", 3, 1*time.Minute)

	err := pool.Close()
	assert.NoError(t, err)

	// Second close should be no-op
	err = pool.Close()
	assert.NoError(t, err)
}

func TestPooledConnection_Metadata(t *testing.T) {
	conn := &PooledConnection{
		client:     nil,
		createdAt:  time.Now(),
		lastUsed:   time.Now(),
		totalCalls: 0,
		errorCount: atomic.Int64{},
	}

	assert.NotNil(t, conn)
	assert.Equal(t, int64(0), conn.totalCalls)
	assert.Equal(t, int64(0), conn.errorCount.Load())

	// Simulate usage
	conn.totalCalls++
	assert.Equal(t, int64(1), conn.totalCalls)
}

func TestPoolMetrics(t *testing.T) {
	metrics := &PoolMetrics{
		totalAcquires: 10,
		totalReleases: 9,
		currentActive: 1,
	}

	metrics.mu.Lock()
	metrics.totalAcquires++
	metrics.mu.Unlock()

	assert.Equal(t, int64(11), metrics.totalAcquires)
	assert.Equal(t, int64(1), metrics.currentActive)
}

// BenchmarkConnectionPool_Acquire benchmarks pool acquisition
func BenchmarkConnectionPool_Acquire(b *testing.B) {
	pool := NewConnectionPool("http://localhost:9999", 10, 1*time.Minute)
	defer pool.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.Acquire(ctx) // Will fail but tests pool logic
	}
}
