package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates a temporary BadgerDB for testing
func setupTestDB(t *testing.T) *badgerdb.DB {
	t.Helper()

	// Create temp directory
	dir, err := os.MkdirTemp("", "jobs-test-*")
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(dir)
	})

	// Open BadgerDB
	opts := badgerdb.DefaultOptions(dir)
	opts.Logger = nil

	db, err := badgerdb.Open(opts)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// ============================================
// Job Type Tests
// ============================================

func TestNewJob(t *testing.T) {
	job := NewJob("weather.get", map[string]interface{}{"city": "Moscow"})

	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "weather.get", job.ToolName)
	assert.Equal(t, "Moscow", job.Args["city"])
	assert.Equal(t, JobPending, job.Status)
	assert.NotZero(t, job.CreatedAt)
	assert.Equal(t, 24*time.Hour, job.TTL)
}

func TestNewJobWithQuery(t *testing.T) {
	job := NewJobWithQuery("какая погода в Москве?")

	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "какая погода в Москве?", job.Query)
	assert.Equal(t, JobPending, job.Status)
}

func TestJobStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   JobStatus
		terminal bool
	}{
		{JobPending, false},
		{JobRunning, false},
		{JobCompleted, true},
		{JobFailed, true},
		{JobCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.terminal, tt.status.IsTerminal())
		})
	}
}

func TestJob_SetRunning(t *testing.T) {
	job := NewJob("test", nil)
	assert.Nil(t, job.StartedAt)

	job.SetRunning()

	assert.Equal(t, JobRunning, job.Status)
	assert.NotNil(t, job.StartedAt)
}

func TestJob_SetCompleted(t *testing.T) {
	job := NewJob("test", nil)
	job.SetRunning()

	job.SetCompleted(nil)

	assert.Equal(t, JobCompleted, job.Status)
	assert.NotNil(t, job.CompletedAt)
	assert.Equal(t, 100, job.Progress)
}

func TestJob_SetFailed(t *testing.T) {
	job := NewJob("test", nil)
	job.SetRunning()

	job.SetFailed(assert.AnError)

	assert.Equal(t, JobFailed, job.Status)
	assert.NotNil(t, job.CompletedAt)
	assert.NotEmpty(t, job.Error)
}

func TestJob_SetCancelled(t *testing.T) {
	job := NewJob("test", nil)

	job.SetCancelled()

	assert.Equal(t, JobCancelled, job.Status)
	assert.NotNil(t, job.CompletedAt)
}

func TestJob_SetProgress(t *testing.T) {
	job := NewJob("test", nil)

	job.SetProgress(50, "Processing...")
	assert.Equal(t, 50, job.Progress)
	assert.Equal(t, "Processing...", job.ProgressMessage)

	// Test bounds
	job.SetProgress(-10, "")
	assert.Equal(t, 0, job.Progress)

	job.SetProgress(150, "")
	assert.Equal(t, 100, job.Progress)
}

func TestJob_Duration(t *testing.T) {
	job := NewJob("test", nil)

	// No duration if not started
	assert.Zero(t, job.Duration())

	// Has duration when running
	job.SetRunning()
	time.Sleep(10 * time.Millisecond)
	assert.Greater(t, job.Duration(), time.Duration(0))

	// Fixed duration when completed
	job.SetCompleted(nil)
	duration1 := job.Duration()
	time.Sleep(10 * time.Millisecond)
	duration2 := job.Duration()
	assert.Equal(t, duration1, duration2)
}

func TestJob_IsExpired(t *testing.T) {
	job := NewJob("test", nil)
	job.TTL = 10 * time.Millisecond

	assert.False(t, job.IsExpired())

	time.Sleep(15 * time.Millisecond)
	assert.True(t, job.IsExpired())
}

func TestJob_JSON(t *testing.T) {
	job := NewJob("test", map[string]interface{}{"key": "value"})
	job.SetRunning()

	// Serialize
	data, err := job.ToJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), "test")

	// Deserialize
	parsed, err := JobFromJSON(data)
	require.NoError(t, err)
	assert.Equal(t, job.ID, parsed.ID)
	assert.Equal(t, job.ToolName, parsed.ToolName)
	assert.Equal(t, job.Status, parsed.Status)
}

// ============================================
// Job Event Tests
// ============================================

func TestNewJobEvent(t *testing.T) {
	job := NewJob("test", nil)
	job.SetRunning()

	event := NewJobEvent(EventJobStarted, job)

	assert.Equal(t, EventJobStarted, event.Type)
	assert.Equal(t, job.ID, event.JobID)
	assert.Equal(t, JobRunning, event.Status)
	assert.NotZero(t, event.Timestamp)
}

func TestJobEvent_ToJSON(t *testing.T) {
	job := NewJob("test", nil)
	event := NewJobEvent(EventJobCreated, job)

	data, err := event.ToJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), "job.created")
}

// ============================================
// Job Storage Tests
// ============================================

func TestBadgerJobStorage_SaveAndGet(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	job := NewJob("test.tool", map[string]interface{}{"arg": "value"})

	// Save
	err := storage.Save(ctx, job)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrieved.ID)
	assert.Equal(t, job.ToolName, retrieved.ToolName)
	assert.Equal(t, job.Status, retrieved.Status)
}

func TestBadgerJobStorage_Get_NotFound(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	_, err := storage.Get(ctx, "non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBadgerJobStorage_Delete(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	job := NewJob("test", nil)
	storage.Save(ctx, job)

	// Delete
	err := storage.Delete(ctx, job.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Get(ctx, job.ID)
	assert.Error(t, err)
}

func TestBadgerJobStorage_List(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		job := NewJob("test", nil)
		storage.Save(ctx, job)
	}

	// List all
	jobs, err := storage.List(ctx, &JobListFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, jobs, 5)

	// List with limit
	jobs, err = storage.List(ctx, &JobListFilter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, jobs, 2)
}

func TestBadgerJobStorage_GetPending(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	// Create pending jobs
	for i := 0; i < 3; i++ {
		job := NewJob("test", nil)
		storage.Save(ctx, job)
	}

	// Create running job
	runningJob := NewJob("running", nil)
	runningJob.SetRunning()
	storage.Save(ctx, runningJob)

	// Get pending
	pending, err := storage.GetPending(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, pending, 3)
}

func TestBadgerJobStorage_UpdateStatus(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	job := NewJob("test", nil)
	storage.Save(ctx, job)

	// Update to running
	err := storage.UpdateStatus(ctx, job.ID, JobRunning)
	require.NoError(t, err)

	// Verify
	updated, _ := storage.Get(ctx, job.ID)
	assert.Equal(t, JobRunning, updated.Status)
	assert.NotNil(t, updated.StartedAt)
}

func TestBadgerJobStorage_GetStats(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	// Create jobs with different statuses
	pending := NewJob("pending", nil)
	storage.Save(ctx, pending)

	running := NewJob("running", nil)
	running.SetRunning()
	storage.Save(ctx, running)

	completed := NewJob("completed", nil)
	completed.SetCompleted(nil)
	storage.Save(ctx, completed)

	// Get stats
	stats, err := storage.GetStats(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, stats.TotalJobs)
	assert.Equal(t, 1, stats.PendingJobs)
	assert.Equal(t, 1, stats.RunningJobs)
	assert.Equal(t, 1, stats.CompletedJobs)
}

func TestBadgerJobStorage_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	storage := NewBadgerJobStorage(db)
	ctx := context.Background()

	// Create expired job
	expiredJob := NewJob("expired", nil)
	expiredJob.TTL = 1 * time.Millisecond
	expiredJob.SetCompleted(nil)
	storage.Save(ctx, expiredJob)

	time.Sleep(5 * time.Millisecond)

	// Cleanup with very short max age
	deleted, err := storage.Cleanup(ctx, 1*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	// Verify deleted
	_, err = storage.Get(ctx, expiredJob.ID)
	assert.Error(t, err)
}

// ============================================
// Job Config Tests
// ============================================

func TestDefaultJobConfig(t *testing.T) {
	config := DefaultJobConfig()

	assert.Equal(t, 10, config.NumWorkers)
	assert.Equal(t, 1000, config.QueueSize)
	assert.Equal(t, 24*time.Hour, config.DefaultTTL)
	assert.Equal(t, 5*time.Minute, config.CleanupInterval)
	assert.Equal(t, 1*time.Hour, config.MaxJobAge)
	assert.Equal(t, 5*time.Minute, config.JobTimeout)
	assert.False(t, config.AutoDeleteCompleted) // Default: keep jobs for history
	assert.True(t, config.KeepFailedJobs)       // Default: keep failed jobs for debugging
}

// ============================================
// Job Response Tests
// ============================================

func TestJob_ToResponse(t *testing.T) {
	job := NewJob("test.tool", nil)
	job.SetRunning()
	time.Sleep(10 * time.Millisecond)
	job.SetCompleted(nil)

	resp := job.ToResponse()

	assert.Equal(t, job.ID, resp.ID)
	assert.Equal(t, job.ToolName, resp.ToolName)
	assert.Equal(t, JobCompleted, resp.Status)
	assert.NotEmpty(t, resp.Duration)
}

// ============================================
// UUID Generation Tests
// ============================================

func TestGenerateUUID(t *testing.T) {
	uuids := make(map[string]bool)

	// Generate 100 UUIDs and check for uniqueness
	for i := 0; i < 100; i++ {
		uuid := generateUUID()
		assert.Len(t, uuid, 36) // Standard UUID length
		assert.False(t, uuids[uuid], "UUID should be unique")
		uuids[uuid] = true
	}
}

