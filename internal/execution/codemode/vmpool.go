package codemode

import (
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/rs/zerolog/log"
)

// VMPool manages a pool of Goja VMs for reuse
type VMPool struct {
	pool    chan *goja.Runtime
	size    int
	mu      sync.Mutex
	metrics *PoolMetrics
}

// PoolMetrics tracks VM pool usage
type PoolMetrics struct {
	totalAcquires   uint64
	totalReleases   uint64
	totalCreated    uint64
	currentActive   int
	avgWaitTime     time.Duration
	totalWaitTime   time.Duration
	mu              sync.RWMutex
}

// NewVMPool creates a new VM pool with the specified size
func NewVMPool(size int) *VMPool {
	pool := &VMPool{
		pool:    make(chan *goja.Runtime, size),
		size:    size,
		metrics: &PoolMetrics{},
	}

	// Pre-warm the pool
	for i := 0; i < size; i++ {
		vm := pool.createVM()
		pool.pool <- vm
		pool.metrics.totalCreated++
	}

	log.Info().
		Int("size", size).
		Msg("VM pool initialized")

	return pool
}

// createVM creates a new Goja runtime with security constraints
func (p *VMPool) createVM() *goja.Runtime {
	vm := goja.New()

	// Disable dangerous globals
	vm.Set("eval", goja.Undefined())
	vm.Set("Function", goja.Undefined())

	// Set max call stack depth (prevents infinite recursion)
	vm.SetMaxCallStackSize(1000)

	// Add console.log для debugging
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, arg := range call.Arguments {
			args[i] = arg.Export()
		}
		log.Debug().Interface("args", args).Msg("console.log")
		return goja.Undefined()
	})
	vm.Set("console", console)

	// Add Promise polyfill для async/await support
	// Note: Goja doesn't have native Promise support, so we use a simple wrapper
	vm.RunString(`
		globalThis.Promise = function(executor) {
			let result, error;
			let resolved = false;
			let rejected = false;

			executor(
				function(value) {
					result = value;
					resolved = true;
				},
				function(err) {
					error = err;
					rejected = true;
				}
			);

			return {
				then: function(onFulfilled) {
					if (resolved) return { value: onFulfilled(result) };
					if (rejected) throw error;
					return this;
				},
				catch: function(onRejected) {
					if (rejected) return onRejected(error);
					return this;
				}
			};
		};
	`)

	log.Debug().Msg("VM created and initialized")

	return vm
}

// Acquire gets a VM from the pool (creates a new one if pool is empty)
func (p *VMPool) Acquire() (*goja.Runtime, error) {
	startWait := time.Now()

	p.metrics.mu.Lock()
	p.metrics.totalAcquires++
	p.metrics.mu.Unlock()

	select {
	case vm := <-p.pool:
		// Got VM from pool
		waitTime := time.Since(startWait)
		
		p.metrics.mu.Lock()
		p.metrics.currentActive++
		p.metrics.totalWaitTime += waitTime
		p.metrics.avgWaitTime = time.Duration(int64(p.metrics.totalWaitTime) / int64(p.metrics.totalAcquires))
		p.metrics.mu.Unlock()

		log.Debug().
			Dur("wait_time", waitTime).
			Msg("VM acquired from pool")

		return vm, nil

	case <-time.After(5 * time.Second):
		// Pool is exhausted, create emergency VM
		log.Warn().Msg("VM pool exhausted, creating emergency VM")
		
		p.metrics.mu.Lock()
		p.metrics.totalCreated++
		p.metrics.currentActive++
		p.metrics.mu.Unlock()

		vm := p.createVM()
		return vm, nil
	}
}

// Release returns a VM to the pool
func (p *VMPool) Release(vm *goja.Runtime) {
	p.metrics.mu.Lock()
	p.metrics.totalReleases++
	p.metrics.currentActive--
	p.metrics.mu.Unlock()

	// Clear VM state before returning to pool
	vm.ClearInterrupt()

	select {
	case p.pool <- vm:
		// Successfully returned to pool
		log.Debug().Msg("VM released to pool")
	default:
		// Pool is full, discard VM
		log.Debug().Msg("VM discarded (pool full)")
	}
}

// GetMetrics returns current pool metrics
func (p *VMPool) GetMetrics() PoolMetrics {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()

	return PoolMetrics{
		totalAcquires:  p.metrics.totalAcquires,
		totalReleases:  p.metrics.totalReleases,
		totalCreated:   p.metrics.totalCreated,
		currentActive:  p.metrics.currentActive,
		avgWaitTime:    p.metrics.avgWaitTime,
	}
}

// GetStats returns human-readable stats
func (p *VMPool) GetStats() map[string]interface{} {
	metrics := p.GetMetrics()

	return map[string]interface{}{
		"pool_size":      p.size,
		"total_acquires": metrics.totalAcquires,
		"total_releases": metrics.totalReleases,
		"total_created":  metrics.totalCreated,
		"current_active": metrics.currentActive,
		"avg_wait_time":  metrics.avgWaitTime.String(),
		"utilization":    fmt.Sprintf("%.2f%%", float64(metrics.currentActive)/float64(p.size)*100),
	}
}

// Close drains the pool and releases resources
func (p *VMPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	close(p.pool)
	
	// Drain remaining VMs
	count := 0
	for range p.pool {
		count++
	}

	log.Info().
		Int("drained", count).
		Msg("VM pool closed")
}

