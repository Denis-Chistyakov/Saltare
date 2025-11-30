package jobs

import (
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/Denis-Chistyakov/Saltare/internal/execution"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	// JobPending - Job is queued and waiting for execution
	JobPending JobStatus = "pending"
	// JobRunning - Job is currently being executed
	JobRunning JobStatus = "running"
	// JobCompleted - Job completed successfully
	JobCompleted JobStatus = "completed"
	// JobFailed - Job failed with an error
	JobFailed JobStatus = "failed"
	// JobCancelled - Job was cancelled before completion
	JobCancelled JobStatus = "cancelled"
)

// IsTerminal returns true if the job status is a terminal state
func (s JobStatus) IsTerminal() bool {
	return s == JobCompleted || s == JobFailed || s == JobCancelled
}

// Job represents an async tool execution task
type Job struct {
	// ID is a unique identifier for the job (UUID)
	ID string `json:"id"`

	// ToolName is the name of the tool to execute
	ToolName string `json:"tool_name"`

	// ToolID is the ID of the tool (for direct lookup)
	ToolID string `json:"tool_id,omitempty"`

	// Args are the arguments passed to the tool
	Args map[string]interface{} `json:"args"`

	// Query is the original natural language query (for smart calls)
	Query string `json:"query,omitempty"`

	// Status is the current job status
	Status JobStatus `json:"status"`

	// Result contains the execution result when completed
	Result *execution.ExecutionResult `json:"result,omitempty"`

	// Error contains the error message if failed
	Error string `json:"error,omitempty"`

	// Progress is a percentage (0-100) for long-running jobs
	Progress int `json:"progress,omitempty"`

	// ProgressMessage is an optional status message
	ProgressMessage string `json:"progress_message,omitempty"`

	// CreatedAt is when the job was created
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is when the job started running
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when the job completed/failed/cancelled
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// TTL is the time-to-live for job cleanup (default: 24h)
	TTL time.Duration `json:"ttl,omitempty"`

	// Metadata contains additional job metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NewJob creates a new job with the given parameters
func NewJob(toolName string, args map[string]interface{}) *Job {
	return &Job{
		ID:        generateUUID(),
		ToolName:  toolName,
		Args:      args,
		Status:    JobPending,
		CreatedAt: time.Now(),
		TTL:       24 * time.Hour, // Default TTL
	}
}

// NewJobWithQuery creates a new job from a natural language query
func NewJobWithQuery(query string) *Job {
	return &Job{
		ID:        generateUUID(),
		Query:     query,
		Args:      make(map[string]interface{}),
		Status:    JobPending,
		CreatedAt: time.Now(),
		TTL:       24 * time.Hour,
	}
}

// Duration returns how long the job has been running (or ran)
func (j *Job) Duration() time.Duration {
	if j.StartedAt == nil {
		return 0
	}
	if j.CompletedAt != nil {
		return j.CompletedAt.Sub(*j.StartedAt)
	}
	return time.Since(*j.StartedAt)
}

// Age returns how old the job is since creation
func (j *Job) Age() time.Duration {
	return time.Since(j.CreatedAt)
}

// IsExpired returns true if the job has exceeded its TTL
func (j *Job) IsExpired() bool {
	if j.TTL == 0 {
		return false
	}
	return j.Age() > j.TTL
}

// SetRunning marks the job as running
func (j *Job) SetRunning() {
	j.Status = JobRunning
	now := time.Now()
	j.StartedAt = &now
}

// SetCompleted marks the job as completed with result
func (j *Job) SetCompleted(result *execution.ExecutionResult) {
	j.Status = JobCompleted
	j.Result = result
	now := time.Now()
	j.CompletedAt = &now
	j.Progress = 100
}

// SetFailed marks the job as failed with error
func (j *Job) SetFailed(err error) {
	j.Status = JobFailed
	if err != nil {
		j.Error = err.Error()
	}
	now := time.Now()
	j.CompletedAt = &now
}

// SetCancelled marks the job as cancelled
func (j *Job) SetCancelled() {
	j.Status = JobCancelled
	now := time.Now()
	j.CompletedAt = &now
}

// SetProgress updates the job progress
func (j *Job) SetProgress(progress int, message string) {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	j.Progress = progress
	j.ProgressMessage = message
}

// ToJSON serializes the job to JSON
func (j *Job) ToJSON() ([]byte, error) {
	return json.Marshal(j)
}

// JobFromJSON deserializes a job from JSON
func JobFromJSON(data []byte) (*Job, error) {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// JobEvent represents a job status change event for SSE
type JobEvent struct {
	// Type is the event type
	Type JobEventType `json:"type"`

	// JobID is the job this event relates to
	JobID string `json:"job_id"`

	// Status is the current job status
	Status JobStatus `json:"status"`

	// Progress is the current progress (0-100)
	Progress int `json:"progress,omitempty"`

	// Message is an optional event message
	Message string `json:"message,omitempty"`

	// Result is included when job completes
	Result *execution.ExecutionResult `json:"result,omitempty"`

	// Error is included when job fails
	Error string `json:"error,omitempty"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`
}

// JobEventType represents the type of job event
type JobEventType string

const (
	// EventJobCreated - Job was created
	EventJobCreated JobEventType = "job.created"
	// EventJobStarted - Job started running
	EventJobStarted JobEventType = "job.started"
	// EventJobProgress - Job progress updated
	EventJobProgress JobEventType = "job.progress"
	// EventJobCompleted - Job completed successfully
	EventJobCompleted JobEventType = "job.completed"
	// EventJobFailed - Job failed
	EventJobFailed JobEventType = "job.failed"
	// EventJobCancelled - Job was cancelled
	EventJobCancelled JobEventType = "job.cancelled"
)

// NewJobEvent creates a new job event
func NewJobEvent(eventType JobEventType, job *Job) *JobEvent {
	event := &JobEvent{
		Type:      eventType,
		JobID:     job.ID,
		Status:    job.Status,
		Progress:  job.Progress,
		Timestamp: time.Now(),
	}

	if job.ProgressMessage != "" {
		event.Message = job.ProgressMessage
	}

	if eventType == EventJobCompleted {
		event.Result = job.Result
	}

	if eventType == EventJobFailed {
		event.Error = job.Error
	}

	return event
}

// ToJSON serializes the event to JSON
func (e *JobEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// JobListFilter defines filters for listing jobs
type JobListFilter struct {
	// Status filters by job status
	Status *JobStatus `json:"status,omitempty"`

	// ToolName filters by tool name
	ToolName string `json:"tool_name,omitempty"`

	// CreatedAfter filters jobs created after this time
	CreatedAfter *time.Time `json:"created_after,omitempty"`

	// CreatedBefore filters jobs created before this time
	CreatedBefore *time.Time `json:"created_before,omitempty"`

	// Limit is the max number of jobs to return
	Limit int `json:"limit,omitempty"`

	// Offset for pagination
	Offset int `json:"offset,omitempty"`
}

// JobConfig contains configuration for the job queue
type JobConfig struct {
	// NumWorkers is the number of concurrent workers (default: 10)
	NumWorkers int `json:"num_workers"`

	// QueueSize is the size of the job queue buffer (default: 1000)
	QueueSize int `json:"queue_size"`

	// DefaultTTL is the default job TTL (default: 24h)
	DefaultTTL time.Duration `json:"default_ttl"`

	// CleanupInterval is how often to run job cleanup (default: 5m)
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// MaxJobAge is the max age for completed jobs before cleanup (default: 1h)
	MaxJobAge time.Duration `json:"max_job_age"`

	// JobTimeout is the max execution time per job (default: 5m)
	JobTimeout time.Duration `json:"job_timeout"`

	// AutoDeleteCompleted deletes jobs immediately after completion (default: false)
	// When true, completed/failed jobs are removed from storage right away
	// When false, jobs are kept until cleanup routine removes them based on MaxJobAge
	AutoDeleteCompleted bool `json:"auto_delete_completed"`

	// KeepFailedJobs keeps failed jobs even when AutoDeleteCompleted is true (default: true)
	// Useful for debugging - you can see what failed
	KeepFailedJobs bool `json:"keep_failed_jobs"`
}

// DefaultJobConfig returns the default job configuration
func DefaultJobConfig() *JobConfig {
	return &JobConfig{
		NumWorkers:          10,
		QueueSize:           1000,
		DefaultTTL:          24 * time.Hour,
		CleanupInterval:     5 * time.Minute,
		MaxJobAge:           1 * time.Hour,
		JobTimeout:          5 * time.Minute,
		AutoDeleteCompleted: false, // Keep jobs for history by default
		KeepFailedJobs:      true,  // Always keep failed jobs for debugging
	}
}

// JobStats contains statistics about the job queue
type JobStats struct {
	// TotalJobs is the total number of jobs
	TotalJobs int `json:"total_jobs"`

	// PendingJobs is the number of pending jobs
	PendingJobs int `json:"pending_jobs"`

	// RunningJobs is the number of running jobs
	RunningJobs int `json:"running_jobs"`

	// CompletedJobs is the number of completed jobs
	CompletedJobs int `json:"completed_jobs"`

	// FailedJobs is the number of failed jobs
	FailedJobs int `json:"failed_jobs"`

	// CancelledJobs is the number of cancelled jobs
	CancelledJobs int `json:"cancelled_jobs"`

	// AvgDuration is the average job duration
	AvgDuration time.Duration `json:"avg_duration"`

	// QueueDepth is the current queue depth
	QueueDepth int `json:"queue_depth"`

	// ActiveWorkers is the number of currently active workers
	ActiveWorkers int `json:"active_workers"`
}

// generateUUID generates a unique ID for jobs
func generateUUID() string {
	// Use crypto/rand for secure UUIDs
	b := make([]byte, 16)
	_, _ = randReader.Read(b)

	// Set version (4) and variant (2) bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return formatUUID(b)
}

func formatUUID(b []byte) string {
	return string(hexEncode(b[0:4])) + "-" +
		string(hexEncode(b[4:6])) + "-" +
		string(hexEncode(b[6:8])) + "-" +
		string(hexEncode(b[8:10])) + "-" +
		string(hexEncode(b[10:16]))
}

func hexEncode(b []byte) []byte {
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = "0123456789abcdef"[v>>4]
		dst[i*2+1] = "0123456789abcdef"[v&0x0f]
	}
	return dst
}

// randReader is used for UUID generation
var randReader = rand.Reader
