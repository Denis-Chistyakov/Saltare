package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
	"github.com/Denis-Chistyakov/Saltare/internal/execution"
	"github.com/Denis-Chistyakov/Saltare/internal/router/semantic"
	"github.com/Denis-Chistyakov/Saltare/internal/storage/search"
	"github.com/Denis-Chistyakov/Saltare/internal/toolkit"
)

// JobManager provides the public API for job management
type JobManager struct {
	queue   *JobQueue
	storage JobStorage
	config  *JobConfig
}

// NewJobManager creates a new job manager
func NewJobManager(
	db *badger.DB,
	executor *execution.ExecutorRegistry,
	manager *toolkit.Manager,
	router *semantic.Router,
	config *JobConfig,
) *JobManager {
	if config == nil {
		config = DefaultJobConfig()
	}

	storage := NewBadgerJobStorage(db)
	queue := NewJobQueue(storage, executor, manager, router, config)

	return &JobManager{
		queue:   queue,
		storage: storage,
		config:  config,
	}
}

// SetSearchClient sets the search provider for smart tool discovery in async jobs
func (m *JobManager) SetSearchClient(client search.Provider) {
	m.queue.SetSearchClient(client)
	log.Info().Msg("Job manager: search provider enabled for async smart calls")
}

// Start starts the job manager
func (m *JobManager) Start() error {
	log.Info().Msg("Starting job manager")
	return m.queue.Start()
}

// Stop stops the job manager gracefully
func (m *JobManager) Stop() error {
	log.Info().Msg("Stopping job manager")
	return m.queue.Stop()
}

// CreateJobRequest contains parameters for creating a job
type CreateJobRequest struct {
	// ToolName is the name of the tool to execute (for direct calls)
	ToolName string `json:"tool_name,omitempty"`

	// ToolID is the ID of the tool (for direct calls)
	ToolID string `json:"tool_id,omitempty"`

	// Args are the arguments for the tool
	Args map[string]interface{} `json:"args,omitempty"`

	// Query is a natural language query (for smart calls)
	Query string `json:"query,omitempty"`

	// TTL is the time-to-live for the job (optional, uses default if not set)
	TTL time.Duration `json:"ttl,omitempty"`

	// Metadata is additional metadata to attach to the job
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CreateJob creates a new async job
func (m *JobManager) CreateJob(ctx context.Context, req *CreateJobRequest) (*Job, error) {
	if req.ToolName == "" && req.ToolID == "" && req.Query == "" {
		return nil, fmt.Errorf("either tool_name, tool_id, or query must be provided")
	}

	var job *Job

	if req.Query != "" {
		// Smart call - use query
		job = NewJobWithQuery(req.Query)
		job.Args = req.Args
	} else {
		// Direct call - use tool name/id
		job = NewJob(req.ToolName, req.Args)
		job.ToolID = req.ToolID
	}

	// Set optional fields
	if req.TTL > 0 {
		job.TTL = req.TTL
	}
	if req.Metadata != nil {
		job.Metadata = req.Metadata
	}

	// Submit job
	if err := m.queue.Submit(job); err != nil {
		return nil, fmt.Errorf("failed to submit job: %w", err)
	}

	log.Info().
		Str("job_id", job.ID).
		Str("tool", job.ToolName).
		Str("query", job.Query).
		Msg("Job created")

	return job, nil
}

// GetJob retrieves a job by ID
// If AutoDeleteCompleted is enabled, completed jobs are deleted after retrieval
func (m *JobManager) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := m.storage.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Auto-delete completed jobs AFTER client retrieves result
	if m.config.AutoDeleteCompleted && job.Status == JobCompleted {
		go func() {
			if err := m.storage.Delete(ctx, jobID); err != nil {
				log.Debug().Err(err).Str("job_id", jobID).Msg("Failed to auto-delete job after retrieval")
			} else {
				log.Debug().Str("job_id", jobID).Msg("Auto-deleted completed job after client retrieval")
			}
		}()
	}

	return job, nil
}

// CancelJob cancels a job
func (m *JobManager) CancelJob(ctx context.Context, jobID string) error {
	return m.queue.Cancel(jobID)
}

// ListJobs retrieves jobs matching the filter
func (m *JobManager) ListJobs(ctx context.Context, filter *JobListFilter) ([]*Job, error) {
	return m.storage.List(ctx, filter)
}

// DeleteJob deletes a job (only if terminal)
func (m *JobManager) DeleteJob(ctx context.Context, jobID string) error {
	job, err := m.storage.Get(ctx, jobID)
	if err != nil {
		return err
	}

	if !job.Status.IsTerminal() {
		return fmt.Errorf("cannot delete job in non-terminal state: %s", job.Status)
	}

	return m.storage.Delete(ctx, jobID)
}

// Subscribe subscribes to job events for real-time updates
func (m *JobManager) Subscribe(jobID string) (<-chan *JobEvent, func()) {
	return m.queue.Subscribe(jobID)
}

// GetStats returns job queue statistics
func (m *JobManager) GetStats() *JobStats {
	return m.queue.GetStats()
}

// WaitForJob waits for a job to complete with timeout
func (m *JobManager) WaitForJob(ctx context.Context, jobID string, timeout time.Duration) (*Job, error) {
	// Subscribe to job events
	events, unsubscribe := m.Subscribe(jobID)
	defer unsubscribe()

	// Check current status first
	job, err := m.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status.IsTerminal() {
		return job, nil
	}

	// Wait for completion
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for job %s", jobID)

		case event, ok := <-events:
			if !ok {
				// Channel closed, get final state
				return m.GetJob(ctx, jobID)
			}

			if event.Status.IsTerminal() {
				return m.GetJob(ctx, jobID)
			}
		}
	}
}

// SubmitAndWait creates a job and waits for it to complete
func (m *JobManager) SubmitAndWait(ctx context.Context, req *CreateJobRequest, timeout time.Duration) (*Job, error) {
	job, err := m.CreateJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return m.WaitForJob(ctx, job.ID, timeout)
}

// Cleanup runs manual cleanup of old jobs
func (m *JobManager) Cleanup(ctx context.Context) (int, error) {
	return m.storage.Cleanup(ctx, m.config.MaxJobAge)
}

// JobResponse is the API response for a job
type JobResponse struct {
	ID              string                 `json:"id"`
	ToolName        string                 `json:"tool_name,omitempty"`
	Query           string                 `json:"query,omitempty"`
	Status          JobStatus              `json:"status"`
	Progress        int                    `json:"progress,omitempty"`
	ProgressMessage string                 `json:"progress_message,omitempty"`
	Result          interface{}            `json:"result,omitempty"`
	Error           string                 `json:"error,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	Duration        string                 `json:"duration,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// ToResponse converts a Job to JobResponse
func (j *Job) ToResponse() *JobResponse {
	resp := &JobResponse{
		ID:              j.ID,
		ToolName:        j.ToolName,
		Query:           j.Query,
		Status:          j.Status,
		Progress:        j.Progress,
		ProgressMessage: j.ProgressMessage,
		Error:           j.Error,
		CreatedAt:       j.CreatedAt,
		StartedAt:       j.StartedAt,
		CompletedAt:     j.CompletedAt,
		Metadata:        j.Metadata,
	}

	if j.Result != nil {
		resp.Result = j.Result.Result
	}

	if j.StartedAt != nil {
		resp.Duration = j.Duration().String()
	}

	return resp
}
