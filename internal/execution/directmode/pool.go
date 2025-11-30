package directmode

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/pkg/mcpclient"
)

// ConnectionPool manages MCP client connections with pooling and health checks
type ConnectionPool struct {
	serverURL      string
	maxConnections int
	idleTimeout    time.Duration
	
	pool        chan *PooledConnection
	active      map[*PooledConnection]time.Time
	mu          sync.RWMutex
	closed      bool
	done        chan struct{} // Signal to stop cleanup goroutine
	
	metrics     *PoolMetrics
}

// PooledConnection wraps an MCP client with metadata
type PooledConnection struct {
	client       *mcpclient.Client
	createdAt    time.Time
	lastUsed     time.Time
	totalCalls   int64
	errorCount   atomic.Int64 // Thread-safe error counter
}

// PoolMetrics tracks connection pool statistics
type PoolMetrics struct {
	mu sync.RWMutex
	
	totalAcquires  int64
	totalReleases  int64
	totalCreated   int64
	totalClosed    int64
	currentActive  int64
	currentIdle    int64
	totalErrors    int64
}

// NewConnectionPool creates a new connection pool for a specific MCP server
func NewConnectionPool(serverURL string, maxConnections int, idleTimeout time.Duration) *ConnectionPool {
	if maxConnections <= 0 {
		maxConnections = 10
	}
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}

	pool := &ConnectionPool{
		serverURL:      serverURL,
		maxConnections: maxConnections,
		idleTimeout:    idleTimeout,
		pool:           make(chan *PooledConnection, maxConnections),
		active:         make(map[*PooledConnection]time.Time),
		done:           make(chan struct{}),
		metrics:        &PoolMetrics{},
	}

	// Start background cleanup goroutine
	go pool.cleanupIdleConnections()

	log.Info().
		Str("server", serverURL).
		Int("max_connections", maxConnections).
		Dur("idle_timeout", idleTimeout).
		Msg("Connection pool created")

	return pool
}

// Acquire gets a connection from the pool or creates a new one
func (p *ConnectionPool) Acquire(ctx context.Context) (*PooledConnection, error) {
	p.metrics.mu.Lock()
	p.metrics.totalAcquires++
	p.metrics.mu.Unlock()

	select {
	case conn := <-p.pool:
		// Got connection from pool
		p.mu.Lock()
		p.active[conn] = time.Now()
		p.metrics.mu.Lock()
		p.metrics.currentActive++
		p.metrics.currentIdle--
		p.metrics.mu.Unlock()
		p.mu.Unlock()

		conn.lastUsed = time.Now()

		// Health check
		if !p.isHealthy(ctx, conn) {
			log.Warn().Str("server", p.serverURL).Msg("Connection unhealthy, creating new one")
			// Remove from active map before closing
			p.mu.Lock()
			delete(p.active, conn)
			p.metrics.mu.Lock()
			p.metrics.currentActive--
			p.metrics.mu.Unlock()
			p.mu.Unlock()
			p.closeConnection(conn)
			return p.createConnection(ctx)
		}

		log.Debug().Str("server", p.serverURL).Msg("Connection acquired from pool")
		return conn, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	default:
		// Pool is empty, try to create new connection
		return p.createConnection(ctx)
	}
}

// Release returns a connection to the pool
func (p *ConnectionPool) Release(conn *PooledConnection) {
	if conn == nil {
		return
	}

	p.metrics.mu.Lock()
	p.metrics.totalReleases++
	p.metrics.mu.Unlock()

	p.mu.Lock()
	delete(p.active, conn)
	p.metrics.mu.Lock()
	p.metrics.currentActive--
	p.metrics.mu.Unlock()
	p.mu.Unlock()

	conn.lastUsed = time.Now()

	// Check if connection is still healthy
	if conn.errorCount.Load() > 5 {
		errorCount := conn.errorCount.Load()
		log.Warn().Str("server", p.serverURL).Int64("errors", errorCount).Msg("Connection has too many errors, closing")
		p.closeConnection(conn)
		return
	}

	select {
	case p.pool <- conn:
		p.metrics.mu.Lock()
		p.metrics.currentIdle++
		p.metrics.mu.Unlock()
		log.Debug().Str("server", p.serverURL).Msg("Connection released to pool")
	default:
		// Pool is full, close connection
		log.Debug().Str("server", p.serverURL).Msg("Pool full, closing connection")
		p.closeConnection(conn)
	}
}

// createConnection creates a new MCP client connection
func (p *ConnectionPool) createConnection(ctx context.Context) (*PooledConnection, error) {
	// Acquire lock first to prevent race condition
	p.mu.Lock()
	activeCount := len(p.active)

	// Check if we've reached max connections
	if activeCount >= p.maxConnections {
		p.mu.Unlock()
		return nil, errors.New("max connections reached")
	}
	p.mu.Unlock()

	// Create new client (without holding lock for I/O operation)
	client := mcpclient.New(p.serverURL)

	// Initialize client with timeout
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Initialize(initCtx); err != nil {
		p.metrics.mu.Lock()
		p.metrics.totalErrors++
		p.metrics.mu.Unlock()
		return nil, err
	}

	conn := &PooledConnection{
		client:     client,
		createdAt:  time.Now(),
		lastUsed:   time.Now(),
		totalCalls: 0,
		errorCount: atomic.Int64{},
	}

	p.mu.Lock()
	p.active[conn] = time.Now()
	p.metrics.mu.Lock()
	p.metrics.totalCreated++
	p.metrics.currentActive++
	p.metrics.mu.Unlock()
	p.mu.Unlock()

	log.Info().
		Str("server", p.serverURL).
		Int("active", activeCount+1).
		Msg("New connection created")

	return conn, nil
}

// isHealthy checks if a connection is still healthy
func (p *ConnectionPool) isHealthy(ctx context.Context, conn *PooledConnection) bool {
	// Simple health check: try to list tools
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := conn.client.ListTools(healthCtx)
	if err != nil {
		conn.errorCount.Add(1)
		return false
	}

	return true
}

// closeConnection closes a connection and cleans up
func (p *ConnectionPool) closeConnection(conn *PooledConnection) {
	if conn == nil || conn.client == nil {
		return
	}

	if err := conn.client.Close(); err != nil {
		log.Error().Err(err).Str("server", p.serverURL).Msg("Failed to close connection")
	}

	p.metrics.mu.Lock()
	p.metrics.totalClosed++
	p.metrics.mu.Unlock()

	log.Debug().Str("server", p.serverURL).Msg("Connection closed")
}

// cleanupIdleConnections periodically removes idle connections
func (p *ConnectionPool) cleanupIdleConnections() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				return
			}

		// Collect connections to check (release lock while checking)
		var toCheck []*PooledConnection
		for {
			select {
			case conn := <-p.pool:
				toCheck = append(toCheck, conn)
			default:
				goto checkDone
			}
		}
	checkDone:
			p.mu.Unlock()

			// Process connections without holding lock
			idleCount := 0
			for _, conn := range toCheck {
				if time.Since(conn.lastUsed) > p.idleTimeout {
					// Connection is idle too long, close it
					p.closeConnection(conn)
					p.metrics.mu.Lock()
					p.metrics.currentIdle--
					p.metrics.mu.Unlock()
					idleCount++
				} else {
					// Return to pool (safe without lock)
					select {
					case p.pool <- conn:
						// Successfully returned
					default:
						// Pool full, close it
						p.closeConnection(conn)
					}
				}
			}

			if idleCount > 0 {
				log.Info().
					Str("server", p.serverURL).
					Int("closed", idleCount).
					Msg("Closed idle connections")
			}

		case <-p.done:
			// Graceful shutdown signal
			return
		}
	}
}

// Close closes all connections in the pool
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	// Signal cleanup goroutine to stop
	close(p.done)

	// Close all active connections
	p.mu.RLock()
	for conn := range p.active {
		p.closeConnection(conn)
	}
	p.mu.RUnlock()

	// Close all pooled connections
	close(p.pool)
	for conn := range p.pool {
		p.closeConnection(conn)
	}

	log.Info().Str("server", p.serverURL).Msg("Connection pool closed")
	return nil
}

// GetMetrics returns current pool metrics
func (p *ConnectionPool) GetMetrics() map[string]interface{} {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()

	return map[string]interface{}{
		"server":          p.serverURL,
		"total_acquires":  p.metrics.totalAcquires,
		"total_releases":  p.metrics.totalReleases,
		"total_created":   p.metrics.totalCreated,
		"total_closed":    p.metrics.totalClosed,
		"current_active":  p.metrics.currentActive,
		"current_idle":    p.metrics.currentIdle,
		"total_errors":    p.metrics.totalErrors,
		"max_connections": p.maxConnections,
	}
}

