package jobs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
	"github.com/Denis-Chistyakov/Saltare/pkg/types"
)

// JobQueue manages the async job processing
type JobQueue struct {
	storage  JobStorage
	executor *execution.ExecutorRegistry
	manager  *toolkit.Manager
	router   *semantic.Router
	search   search.Provider // Optional: for smart tool discovery (Meilisearch or Typesense)
	config   *JobConfig

	jobs   chan *Job      // Job channel
	events chan *JobEvent // Event channel for SSE
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	activeWorkers int32 // Number of currently active workers
	queueDepth    int32 // Current queue depth

	subscribers map[string][]chan *JobEvent // job_id → subscribers
	subMu       sync.RWMutex

	running atomic.Bool
}

// SetSearchClient sets the search provider for smart tool discovery
// Accepts either Meilisearch or Typesense client implementing search.Provider
func (q *JobQueue) SetSearchClient(client search.Provider) {
	q.search = client
}

// NewJobQueue creates a new job queue
func NewJobQueue(
	storage JobStorage,
	executor *execution.ExecutorRegistry,
	manager *toolkit.Manager,
	router *semantic.Router,
	config *JobConfig,
) *JobQueue {
	if config == nil {
		config = DefaultJobConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &JobQueue{
		storage:     storage,
		executor:    executor,
		manager:     manager,
		router:      router,
		config:      config,
		jobs:        make(chan *Job, config.QueueSize),
		events:      make(chan *JobEvent, config.QueueSize),
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[string][]chan *JobEvent),
	}
}

// Start starts the job queue workers
func (q *JobQueue) Start() error {
	if q.running.Load() {
		return fmt.Errorf("job queue already running")
	}

	q.running.Store(true)

	// Start workers
	for i := 0; i < q.config.NumWorkers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	// Start event broadcaster
	q.wg.Add(1)
	go q.eventBroadcaster()

	// Start cleanup routine
	q.wg.Add(1)
	go q.cleanupRoutine()

	// Note: Auto-delete is handled in JobManager.GetJob after client retrieves result

	// Load pending jobs from storage
	go q.loadPendingJobs()

	log.Info().
		Int("workers", q.config.NumWorkers).
		Int("queue_size", q.config.QueueSize).
		Msg("Job queue started")

	return nil
}

// Stop stops the job queue gracefully
func (q *JobQueue) Stop() error {
	if !q.running.Load() {
		return nil
	}

	log.Info().Msg("Stopping job queue...")
	q.running.Store(false)
	q.cancel()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("Job queue stopped gracefully")
	case <-time.After(30 * time.Second):
		log.Warn().Msg("Job queue stop timed out")
	}

	return nil
}

// Submit submits a job for execution
func (q *JobQueue) Submit(job *Job) error {
	if !q.running.Load() {
		return fmt.Errorf("job queue not running")
	}

	// Save job to storage
	if err := q.storage.Save(q.ctx, job); err != nil {
		return fmt.Errorf("failed to save job: %w", err)
	}

	// Try to enqueue
	select {
	case q.jobs <- job:
		atomic.AddInt32(&q.queueDepth, 1)
		q.emitEvent(EventJobCreated, job)
		log.Debug().
			Str("job_id", job.ID).
			Str("tool", job.ToolName).
			Msg("Job submitted")
		return nil
	default:
		// Queue full - job is saved, will be picked up by loadPendingJobs
		log.Warn().
			Str("job_id", job.ID).
			Msg("Job queue full, job saved for later processing")
		return nil
	}
}

// Cancel cancels a job
func (q *JobQueue) Cancel(jobID string) error {
	job, err := q.storage.Get(q.ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status.IsTerminal() {
		return fmt.Errorf("cannot cancel job in terminal state: %s", job.Status)
	}

	job.SetCancelled()
	if err := q.storage.Save(q.ctx, job); err != nil {
		return fmt.Errorf("failed to save cancelled job: %w", err)
	}

	q.emitEvent(EventJobCancelled, job)
	return nil
}

// Subscribe subscribes to job events
func (q *JobQueue) Subscribe(jobID string) (<-chan *JobEvent, func()) {
	ch := make(chan *JobEvent, 10)

	q.subMu.Lock()
	q.subscribers[jobID] = append(q.subscribers[jobID], ch)
	q.subMu.Unlock()

	unsubscribe := func() {
		q.subMu.Lock()
		defer q.subMu.Unlock()

		subs := q.subscribers[jobID]
		for i, sub := range subs {
			if sub == ch {
				q.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}

	return ch, unsubscribe
}

// GetStats returns queue statistics
func (q *JobQueue) GetStats() *JobStats {
	stats, _ := q.storage.GetStats(q.ctx)
	if stats == nil {
		stats = &JobStats{}
	}

	stats.QueueDepth = int(atomic.LoadInt32(&q.queueDepth))
	stats.ActiveWorkers = int(atomic.LoadInt32(&q.activeWorkers))

	return stats
}

// worker is a single job processing worker
func (q *JobQueue) worker(id int) {
	defer q.wg.Done()

	log.Debug().Int("worker_id", id).Msg("Worker started")

	for {
		select {
		case <-q.ctx.Done():
			log.Debug().Int("worker_id", id).Msg("Worker stopped")
			return

		case job, ok := <-q.jobs:
			if !ok {
				return
			}

			atomic.AddInt32(&q.queueDepth, -1)
			atomic.AddInt32(&q.activeWorkers, 1)

			q.processJob(job)

			atomic.AddInt32(&q.activeWorkers, -1)
		}
	}
}

// processJob processes a single job
func (q *JobQueue) processJob(job *Job) {
	// Check if job was cancelled while in queue
	currentJob, err := q.storage.Get(q.ctx, job.ID)
	if err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("Failed to get job")
		return
	}
	if currentJob.Status == JobCancelled {
		log.Debug().Str("job_id", job.ID).Msg("Job was cancelled, skipping")
		return
	}

	// Update status to running
	job.SetRunning()
	if err := q.storage.Save(q.ctx, job); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("Failed to update job to running")
		return
	}
	q.emitEvent(EventJobStarted, job)

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(q.ctx, q.config.JobTimeout)
	defer cancel()

	// Find the tool
	var tool *types.Tool
	if job.ToolID != "" {
		tool, err = q.manager.GetTool(job.ToolID)
	} else if job.ToolName != "" {
		tool, err = q.manager.GetToolByName(job.ToolName)
	} else if job.Query != "" {
		// Smart mode: use Semantic Router + Typesense for tool discovery
		log.Info().Str("query", job.Query).Msg("Async smart call: parsing query")

		// Use semantic router to parse intent
		intent, parseErr := q.router.ParseIntent(execCtx, job.Query)
		if parseErr != nil {
			log.Warn().Err(parseErr).Str("query", job.Query).Msg("Failed to parse intent")
		}

		// Try search provider (Meilisearch/Typesense) for smart search
		if q.search != nil && intent != nil {
			searchResult, searchErr := q.search.Search(execCtx, search.SearchParams{
				Query:    intent.Entity + " " + intent.Action,
				PageSize: 1,
			})
			if searchErr == nil && len(searchResult.Tools) > 0 {
				foundDoc := searchResult.Tools[0]
				tool, _ = q.manager.GetTool(foundDoc.ID)
				if tool != nil {
					log.Debug().
						Str("query", job.Query).
						Str("found_tool", foundDoc.Name).
						Float64("confidence", intent.Confidence).
						Str("provider", q.search.Name()).
						Msg("Async: Tool found via search provider")
				}
			}
		}

		// Fallback to local search
		if tool == nil && intent != nil {
			tools := q.manager.SearchTools(intent.Action+" "+intent.Entity, nil)
			if len(tools) > 0 {
				tool = tools[0]
				log.Debug().
					Str("query", job.Query).
					Str("found_tool", tool.Name).
					Msg("Async: Tool found via local search")
			}
		}

		// Last resort: keyword matching
		if tool == nil {
			allTools := q.manager.ListAllTools()
			for _, t := range allTools {
				if containsKeyword(t.Name, job.Query) || containsKeyword(t.Description, job.Query) {
					tool = t
					log.Debug().
						Str("query", job.Query).
						Str("found_tool", tool.Name).
						Msg("Async: Tool found via keyword matching")
					break
				}
			}
		}

		if tool != nil {
			job.ToolName = tool.Name
			job.ToolID = tool.ID
		}
	}

	if err != nil || tool == nil {
		job.SetFailed(fmt.Errorf("tool not found: %s", job.ToolName))
		q.storage.Save(q.ctx, job)
		q.emitEvent(EventJobFailed, job)
		// Auto-delete is handled by eventBroadcaster → deleteWorker
		return
	}

	// Execute the tool
	result, err := q.executor.Execute(execCtx, execution.DirectMode, tool, job.Args)
	if err != nil {
		job.SetFailed(err)
		q.storage.Save(q.ctx, job)
		q.emitEvent(EventJobFailed, job)
		// Auto-delete is handled by eventBroadcaster → deleteWorker (respects KeepFailedJobs)

		log.Error().
			Err(err).
			Str("job_id", job.ID).
			Str("tool", tool.Name).
			Msg("Job execution failed")
		return
	}

	// Update job with result
	job.SetCompleted(result)
	if err := q.storage.Save(q.ctx, job); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("Failed to save completed job")
		return
	}
	q.emitEvent(EventJobCompleted, job)
	// Auto-delete is handled by eventBroadcaster → deleteWorker

	log.Info().
		Str("job_id", job.ID).
		Str("tool", tool.Name).
		Dur("duration", job.Duration()).
		Msg("Job completed successfully")
}

// emitEvent sends a job event
func (q *JobQueue) emitEvent(eventType JobEventType, job *Job) {
	event := NewJobEvent(eventType, job)

	select {
	case q.events <- event:
	default:
		log.Warn().Str("job_id", job.ID).Msg("Event channel full, dropping event")
	}
}

// eventBroadcaster broadcasts events to subscribers
func (q *JobQueue) eventBroadcaster() {
	defer q.wg.Done()

	for {
		select {
		case <-q.ctx.Done():
			return

		case event, ok := <-q.events:
			if !ok {
				return
			}

			q.subMu.RLock()
			subs := q.subscribers[event.JobID]
			q.subMu.RUnlock()

			// Deliver event to all subscribers
			for _, ch := range subs {
				select {
				case ch <- event:
				default:
					// Subscriber channel full
				}
			}

			// Handle terminal events
			if event.Status.IsTerminal() {
				// Cleanup subscribers
				go func(jobID string) {
					time.Sleep(5 * time.Second) // Give time for final events
					q.subMu.Lock()
					delete(q.subscribers, jobID)
					q.subMu.Unlock()
				}(event.JobID)

				// Note: Auto-delete is handled in GetJob after client retrieves result
			}
		}
	}
}

// cleanupRoutine periodically cleans up old jobs
func (q *JobQueue) cleanupRoutine() {
	defer q.wg.Done()

	ticker := time.NewTicker(q.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return

		case <-ticker.C:
			deleted, err := q.storage.Cleanup(q.ctx, q.config.MaxJobAge)
			if err != nil {
				log.Error().Err(err).Msg("Job cleanup failed")
			} else if deleted > 0 {
				log.Debug().Int("deleted", deleted).Msg("Cleaned up old jobs")
			}
		}
	}
}

// loadPendingJobs loads pending jobs from storage on startup
func (q *JobQueue) loadPendingJobs() {
	jobs, err := q.storage.GetPending(q.ctx, q.config.QueueSize)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load pending jobs")
		return
	}

	loaded := 0
loadLoop:
	for _, job := range jobs {
		select {
		case q.jobs <- job:
			atomic.AddInt32(&q.queueDepth, 1)
			loaded++
		default:
			// Queue full, stop loading
			break loadLoop
		}
		if loaded >= q.config.QueueSize {
			break loadLoop
		}
	}

	if len(jobs) > 0 {
		log.Info().Int("count", len(jobs)).Msg("Loaded pending jobs from storage")
	}
}

// containsKeyword checks if text contains any word from query (case-insensitive)
func containsKeyword(text, query string) bool {
	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)

	// Check if query is directly in text
	if strings.Contains(textLower, queryLower) {
		return true
	}

	// Check individual words
	words := strings.Fields(queryLower)
	for _, word := range words {
		if len(word) > 3 && strings.Contains(textLower, word) {
			return true
		}
	}
	return false
}
