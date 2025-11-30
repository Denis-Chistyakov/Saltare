package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// SSEWriter wraps an io.Writer for SSE format
type SSEWriter struct {
	w       io.Writer
	flusher func()
}

// NewSSEWriter creates a new SSE writer
func NewSSEWriter(w io.Writer, flusher func()) *SSEWriter {
	return &SSEWriter{
		w:       w,
		flusher: flusher,
	}
}

// WriteEvent writes a single SSE event
func (s *SSEWriter) WriteEvent(eventType string, data interface{}) error {
	var dataStr string

	switch v := data.(type) {
	case string:
		dataStr = v
	case []byte:
		dataStr = string(v)
	default:
		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal event data: %w", err)
		}
		dataStr = string(jsonData)
	}

	// Write SSE format
	if eventType != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", eventType); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", dataStr); err != nil {
		return err
	}

	if s.flusher != nil {
		s.flusher()
	}

	return nil
}

// WriteJobEvent writes a JobEvent in SSE format
func (s *SSEWriter) WriteJobEvent(event *JobEvent) error {
	return s.WriteEvent(string(event.Type), event)
}

// WritePing writes a ping/keepalive event
func (s *SSEWriter) WritePing() error {
	return s.WriteEvent("ping", map[string]interface{}{
		"timestamp": time.Now().Unix(),
	})
}

// WriteError writes an error event
func (s *SSEWriter) WriteError(err error) error {
	return s.WriteEvent("error", map[string]interface{}{
		"error":     err.Error(),
		"timestamp": time.Now().Unix(),
	})
}

// StreamJob streams job events to the SSE writer
func StreamJob(ctx context.Context, manager *JobManager, jobID string, w *SSEWriter) error {
	// Get initial job state
	job, err := manager.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	// Send initial state
	initialEvent := NewJobEvent(EventJobCreated, job)
	if job.Status == JobRunning {
		initialEvent.Type = EventJobStarted
	} else if job.Status == JobCompleted {
		initialEvent.Type = EventJobCompleted
	} else if job.Status == JobFailed {
		initialEvent.Type = EventJobFailed
	} else if job.Status == JobCancelled {
		initialEvent.Type = EventJobCancelled
	}

	if err := w.WriteJobEvent(initialEvent); err != nil {
		return err
	}

	// If already terminal, we're done
	if job.Status.IsTerminal() {
		return nil
	}

	// Subscribe to events
	events, unsubscribe := manager.Subscribe(jobID)
	defer unsubscribe()

	// Keepalive ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := w.WritePing(); err != nil {
				return err
			}

		case event, ok := <-events:
			if !ok {
				return nil
			}

			if err := w.WriteJobEvent(event); err != nil {
				return err
			}

			// Stop on terminal state
			if event.Status.IsTerminal() {
				return nil
			}
		}
	}
}

// JobStreamResponse represents the full job stream response
type JobStreamResponse struct {
	// Job is the final job state
	Job *Job `json:"job"`

	// Events is the list of events that occurred
	Events []*JobEvent `json:"events"`

	// Error is set if an error occurred during streaming
	Error string `json:"error,omitempty"`
}

// CollectJobEvents collects all job events until completion
func CollectJobEvents(ctx context.Context, manager *JobManager, jobID string, timeout time.Duration) (*JobStreamResponse, error) {
	response := &JobStreamResponse{
		Events: make([]*JobEvent, 0),
	}

	// Get initial job state
	job, err := manager.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// If already terminal, return immediately
	if job.Status.IsTerminal() {
		response.Job = job
		return response, nil
	}

	// Subscribe to events
	events, unsubscribe := manager.Subscribe(jobID)
	defer unsubscribe()

	// Timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			response.Error = ctx.Err().Error()
			response.Job, _ = manager.GetJob(ctx, jobID)
			return response, nil

		case <-timer.C:
			response.Error = "timeout"
			response.Job, _ = manager.GetJob(ctx, jobID)
			return response, nil

		case event, ok := <-events:
			if !ok {
				response.Job, _ = manager.GetJob(ctx, jobID)
				return response, nil
			}

			response.Events = append(response.Events, event)

			// Stop on terminal state
			if event.Status.IsTerminal() {
				response.Job, _ = manager.GetJob(ctx, jobID)
				return response, nil
			}
		}
	}
}

