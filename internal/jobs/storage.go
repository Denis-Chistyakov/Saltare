package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

// JobStorage defines the interface for job persistence
type JobStorage interface {
	// Save saves or updates a job
	Save(ctx context.Context, job *Job) error

	// Get retrieves a job by ID
	Get(ctx context.Context, id string) (*Job, error)

	// Delete removes a job by ID
	Delete(ctx context.Context, id string) error

	// List retrieves jobs matching the filter
	List(ctx context.Context, filter *JobListFilter) ([]*Job, error)

	// GetPending retrieves pending jobs in order
	GetPending(ctx context.Context, limit int) ([]*Job, error)

	// UpdateStatus updates job status and manages indexes
	UpdateStatus(ctx context.Context, id string, status JobStatus) error

	// GetStats retrieves job statistics
	GetStats(ctx context.Context) (*JobStats, error)

	// Cleanup removes expired jobs
	Cleanup(ctx context.Context, maxAge time.Duration) (int, error)

	// Close closes the storage
	Close() error
}

// BadgerJobStorage implements JobStorage using BadgerDB
type BadgerJobStorage struct {
	db *badger.DB
}

// Key prefixes
const (
	keyPrefixJob      = "jobs:"
	keyPrefixPending  = "jobs:pending:"
	keyPrefixByTool   = "jobs:by_tool:"
	keyPrefixByStatus = "jobs:by_status:"
)

// NewBadgerJobStorage creates a new BadgerDB job storage
func NewBadgerJobStorage(db *badger.DB) *BadgerJobStorage {
	return &BadgerJobStorage{db: db}
}

// jobKey returns the key for a job
func jobKey(id string) []byte {
	return []byte(keyPrefixJob + id)
}

// pendingKey returns the key for pending index
func pendingKey(timestamp time.Time, id string) []byte {
	return []byte(fmt.Sprintf("%s%d:%s", keyPrefixPending, timestamp.UnixNano(), id))
}

// toolKey returns the key for tool index
func toolKey(toolName, id string) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", keyPrefixByTool, toolName, id))
}

// statusKey returns the key for status index
func statusKey(status JobStatus, id string) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", keyPrefixByStatus, status, id))
}

// Save saves or updates a job
func (s *BadgerJobStorage) Save(ctx context.Context, job *Job) error {
	data, err := job.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize job: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		// Check if job exists to manage indexes
		existingJob, _ := s.getInTxn(txn, job.ID)

		// Save the job
		if err := txn.Set(jobKey(job.ID), data); err != nil {
			return fmt.Errorf("failed to save job: %w", err)
		}

		// Update indexes
		if existingJob != nil {
			// Remove old indexes
			s.removeIndexes(txn, existingJob)
		}

		// Add new indexes
		s.addIndexes(txn, job)

		return nil
	})
}

// Get retrieves a job by ID
func (s *BadgerJobStorage) Get(ctx context.Context, id string) (*Job, error) {
	var job *Job

	err := s.db.View(func(txn *badger.Txn) error {
		var err error
		job, err = s.getInTxn(txn, id)
		return err
	})

	return job, err
}

// getInTxn retrieves a job within a transaction
func (s *BadgerJobStorage) getInTxn(txn *badger.Txn, id string) (*Job, error) {
	item, err := txn.Get(jobKey(id))
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, fmt.Errorf("job not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	var job *Job
	err = item.Value(func(val []byte) error {
		var parseErr error
		job, parseErr = JobFromJSON(val)
		return parseErr
	})

	return job, err
}

// Delete removes a job by ID
func (s *BadgerJobStorage) Delete(ctx context.Context, id string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Get job to remove indexes
		job, err := s.getInTxn(txn, id)
		if err != nil {
			return err
		}

		// Remove indexes
		s.removeIndexes(txn, job)

		// Delete job
		return txn.Delete(jobKey(id))
	})
}

// List retrieves jobs matching the filter
func (s *BadgerJobStorage) List(ctx context.Context, filter *JobListFilter) ([]*Job, error) {
	var jobsList []*Job

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100

		it := txn.NewIterator(opts)
		defer it.Close()

		// Determine which prefix to use
		var prefix []byte
		useIndex := false

		// Use status index if filtering by status
		if filter != nil && filter.Status != nil {
			prefix = []byte(fmt.Sprintf("%s%s:", keyPrefixByStatus, *filter.Status))
			useIndex = true
		} else if filter != nil && filter.ToolName != "" {
			// Use tool index if filtering by tool
			prefix = []byte(fmt.Sprintf("%s%s:", keyPrefixByTool, filter.ToolName))
			useIndex = true
		} else {
			// Use direct job keys (need to filter out index keys)
			prefix = []byte(keyPrefixJob)
		}

		count := 0
		offset := 0
		if filter != nil {
			offset = filter.Offset
		}

		limit := 100
		if filter != nil && filter.Limit > 0 {
			limit = filter.Limit
		}

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.Key())

			var job *Job
			var err error

			if useIndex {
				// Index key - extract job ID from the last part
				parts := strings.Split(key, ":")
				if len(parts) >= 4 {
					jobID := parts[len(parts)-1]
					job, err = s.getInTxn(txn, jobID)
				}
			} else {
				// Direct job key - skip index keys
				if strings.HasPrefix(key, keyPrefixPending) ||
					strings.HasPrefix(key, keyPrefixByTool) ||
					strings.HasPrefix(key, keyPrefixByStatus) {
					continue
				}

				// Parse direct job
				err = item.Value(func(val []byte) error {
					var parseErr error
					job, parseErr = JobFromJSON(val)
					return parseErr
				})
			}

			if err != nil {
				log.Warn().Err(err).Str("key", key).Msg("Failed to load job")
				continue
			}

			if job == nil {
				continue
			}

			// Apply additional filters
			if filter != nil {
				if filter.CreatedAfter != nil && job.CreatedAt.Before(*filter.CreatedAfter) {
					continue
				}
				if filter.CreatedBefore != nil && job.CreatedAt.After(*filter.CreatedBefore) {
					continue
				}
			}

			// Handle offset
			if offset > 0 {
				offset--
				continue
			}

			jobsList = append(jobsList, job)
			count++

			if count >= limit {
				break
			}
		}

		return nil
	})

	return jobsList, err
}

// GetPending retrieves pending jobs in order
func (s *BadgerJobStorage) GetPending(ctx context.Context, limit int) ([]*Job, error) {
	var jobs []*Job

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = limit

		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(keyPrefixPending)
		count := 0

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if count >= limit {
				break
			}

			key := string(it.Item().Key())
			// Extract job ID from pending key: jobs:pending:{timestamp}:{id}
			parts := strings.Split(key, ":")
			if len(parts) < 4 {
				continue
			}
			jobID := parts[3]

			job, err := s.getInTxn(txn, jobID)
			if err != nil {
				log.Warn().Err(err).Str("job_id", jobID).Msg("Failed to load pending job")
				continue
			}

			if job.Status == JobPending {
				jobs = append(jobs, job)
				count++
			}
		}

		return nil
	})

	return jobs, err
}

// UpdateStatus updates job status and manages indexes
func (s *BadgerJobStorage) UpdateStatus(ctx context.Context, id string, status JobStatus) error {
	return s.db.Update(func(txn *badger.Txn) error {
		job, err := s.getInTxn(txn, id)
		if err != nil {
			return err
		}

		// Remove old indexes
		s.removeIndexes(txn, job)

		// Update status
		oldStatus := job.Status
		job.Status = status

		// Update timestamps
		now := time.Now()
		if status == JobRunning && job.StartedAt == nil {
			job.StartedAt = &now
		}
		if status.IsTerminal() && job.CompletedAt == nil {
			job.CompletedAt = &now
		}

		// Save updated job
		data, err := job.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to serialize job: %w", err)
		}
		if err := txn.Set(jobKey(job.ID), data); err != nil {
			return fmt.Errorf("failed to save job: %w", err)
		}

		// Add new indexes
		s.addIndexes(txn, job)

		log.Debug().
			Str("job_id", id).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(status)).
			Msg("Job status updated")

		return nil
	})
}

// GetStats retrieves job statistics
func (s *BadgerJobStorage) GetStats(ctx context.Context) (*JobStats, error) {
	stats := &JobStats{}

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		// Count by status
		for _, status := range []JobStatus{JobPending, JobRunning, JobCompleted, JobFailed, JobCancelled} {
			prefix := []byte(fmt.Sprintf("%s%s:", keyPrefixByStatus, status))
			count := 0
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				count++
			}

			switch status {
			case JobPending:
				stats.PendingJobs = count
			case JobRunning:
				stats.RunningJobs = count
			case JobCompleted:
				stats.CompletedJobs = count
			case JobFailed:
				stats.FailedJobs = count
			case JobCancelled:
				stats.CancelledJobs = count
			}
		}

		stats.TotalJobs = stats.PendingJobs + stats.RunningJobs +
			stats.CompletedJobs + stats.FailedJobs + stats.CancelledJobs

		return nil
	})

	return stats, err
}

// Cleanup removes expired jobs
func (s *BadgerJobStorage) Cleanup(ctx context.Context, maxAge time.Duration) (int, error) {
	var deleted int

	err := s.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		// Only clean up terminal jobs
		for _, status := range []JobStatus{JobCompleted, JobFailed, JobCancelled} {
			prefix := []byte(fmt.Sprintf("%s%s:", keyPrefixByStatus, status))

			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := string(it.Item().Key())
				parts := strings.Split(key, ":")
				if len(parts) < 4 {
					continue
				}
				jobID := parts[3]

				job, err := s.getInTxn(txn, jobID)
				if err != nil {
					continue
				}

				// Check if job is expired
				isExpired := job.IsExpired()
				isOld := job.CompletedAt != nil && time.Since(*job.CompletedAt) > maxAge

				if isExpired || isOld {
					// Remove indexes
					s.removeIndexes(txn, job)

					// Delete job
					if err := txn.Delete(jobKey(jobID)); err == nil {
						deleted++
					}
				}
			}
		}

		return nil
	})

	if deleted > 0 {
		log.Info().Int("deleted", deleted).Dur("max_age", maxAge).Msg("Cleaned up old jobs")
	}

	return deleted, err
}

// Close closes the storage (no-op for BadgerDB as it's managed externally)
func (s *BadgerJobStorage) Close() error {
	return nil
}

// addIndexes adds index entries for a job
func (s *BadgerJobStorage) addIndexes(txn *badger.Txn, job *Job) {
	// Pending index (only for pending jobs)
	if job.Status == JobPending {
		txn.Set(pendingKey(job.CreatedAt, job.ID), []byte{})
	}

	// Tool index
	if job.ToolName != "" {
		txn.Set(toolKey(job.ToolName, job.ID), []byte{})
	}

	// Status index
	txn.Set(statusKey(job.Status, job.ID), []byte{})
}

// removeIndexes removes index entries for a job
func (s *BadgerJobStorage) removeIndexes(txn *badger.Txn, job *Job) {
	// Pending index
	txn.Delete(pendingKey(job.CreatedAt, job.ID))

	// Tool index
	if job.ToolName != "" {
		txn.Delete(toolKey(job.ToolName, job.ID))
	}

	// Status index
	txn.Delete(statusKey(job.Status, job.ID))
}
