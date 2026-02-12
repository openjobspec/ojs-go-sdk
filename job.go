package ojs

import (
	"encoding/json"
	"fmt"
	"time"
)

// JobState represents the lifecycle state of a job.
type JobState string

const (
	JobStatePending   JobState = "pending"
	JobStateScheduled JobState = "scheduled"
	JobStateAvailable JobState = "available"
	JobStateActive    JobState = "active"
	JobStateCompleted JobState = "completed"
	JobStateRetryable JobState = "retryable"
	JobStateCancelled JobState = "cancelled"
	JobStateDiscarded JobState = "discarded"
)

// IsTerminal returns true if the job state is a terminal state.
func (s JobState) IsTerminal() bool {
	return s == JobStateCompleted || s == JobStateCancelled || s == JobStateDiscarded
}

// Args represents the arguments for a job as a key-value map.
// On the OJS wire format, args are serialized as a JSON array
// containing a single object: [{"key": "value", ...}].
type Args map[string]any

// Job represents an OJS job envelope.
type Job struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	State       JobState       `json:"state"`
	Queue       string         `json:"queue"`
	Priority    int            `json:"priority"`
	Attempt     int            `json:"attempt"`
	MaxAttempts int            `json:"max_attempts"`
	TimeoutMS   int            `json:"timeout_ms,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	Error       *JobError      `json:"error,omitempty"`

	CreatedAt   *time.Time `json:"created_at,omitempty"`
	EnqueuedAt  *time.Time `json:"enqueued_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	DiscardedAt *time.Time `json:"discarded_at,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`

	// Args is the user-friendly map representation.
	// The SDK handles conversion to/from the OJS wire format (JSON array).
	Args Args `json:"-"`

	// RawArgs preserves the original wire-format array for cases where
	// positional arguments are used instead of a single-object map.
	RawArgs []any `json:"-"`

	// PreviousState is set on cancel responses.
	PreviousState string `json:"previous_state,omitempty"`
}

// jobJSON is an alias used for custom JSON marshaling/unmarshaling.
type jobJSON struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	State         JobState       `json:"state"`
	Queue         string         `json:"queue"`
	Priority      int            `json:"priority"`
	Attempt       int            `json:"attempt"`
	MaxAttempts   int            `json:"max_attempts"`
	TimeoutMS     int            `json:"timeout_ms,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
	Result        map[string]any `json:"result,omitempty"`
	Error         *JobError      `json:"error,omitempty"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
	EnqueuedAt    *time.Time     `json:"enqueued_at,omitempty"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	CancelledAt   *time.Time     `json:"cancelled_at,omitempty"`
	DiscardedAt   *time.Time     `json:"discarded_at,omitempty"`
	ScheduledAt   *time.Time     `json:"scheduled_at,omitempty"`
	ExpiresAt     *time.Time     `json:"expires_at,omitempty"`
	Args          json.RawMessage `json:"args,omitempty"`
	PreviousState string         `json:"previous_state,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for Job.
func (j Job) MarshalJSON() ([]byte, error) {
	raw := jobJSON{
		ID:            j.ID,
		Type:          j.Type,
		State:         j.State,
		Queue:         j.Queue,
		Priority:      j.Priority,
		Attempt:       j.Attempt,
		MaxAttempts:   j.MaxAttempts,
		TimeoutMS:     j.TimeoutMS,
		Tags:          j.Tags,
		Meta:          j.Meta,
		Result:        j.Result,
		Error:         j.Error,
		CreatedAt:     j.CreatedAt,
		EnqueuedAt:    j.EnqueuedAt,
		StartedAt:     j.StartedAt,
		CompletedAt:   j.CompletedAt,
		CancelledAt:   j.CancelledAt,
		DiscardedAt:   j.DiscardedAt,
		ScheduledAt:   j.ScheduledAt,
		ExpiresAt:     j.ExpiresAt,
		PreviousState: j.PreviousState,
	}
	raw.Args, _ = json.Marshal(argsToWire(j.Args))
	return json.Marshal(raw)
}

// UnmarshalJSON implements custom JSON unmarshaling for Job.
func (j *Job) UnmarshalJSON(data []byte) error {
	var raw jobJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	j.ID = raw.ID
	j.Type = raw.Type
	j.State = raw.State
	j.Queue = raw.Queue
	j.Priority = raw.Priority
	j.Attempt = raw.Attempt
	j.MaxAttempts = raw.MaxAttempts
	j.TimeoutMS = raw.TimeoutMS
	j.Tags = raw.Tags
	j.Meta = raw.Meta
	j.Result = raw.Result
	j.Error = raw.Error
	j.CreatedAt = raw.CreatedAt
	j.EnqueuedAt = raw.EnqueuedAt
	j.StartedAt = raw.StartedAt
	j.CompletedAt = raw.CompletedAt
	j.CancelledAt = raw.CancelledAt
	j.DiscardedAt = raw.DiscardedAt
	j.ScheduledAt = raw.ScheduledAt
	j.ExpiresAt = raw.ExpiresAt
	j.PreviousState = raw.PreviousState

	if len(raw.Args) > 0 {
		var arr []any
		if err := json.Unmarshal(raw.Args, &arr); err == nil {
			j.RawArgs = arr
			j.Args = argsFromWire(arr)
		}
	}
	return nil
}

// JobError represents a structured error associated with a job.
type JobError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

// JobRequest represents a request to enqueue a job.
type JobRequest struct {
	Type    string
	Args    Args
	Options []EnqueueOption
}

// argsToWire converts Args (map) to the OJS wire format (JSON array).
func argsToWire(a Args) []any {
	if a == nil || len(a) == 0 {
		return []any{}
	}
	return []any{map[string]any(a)}
}

// argsFromWire converts the OJS wire format (JSON array) to Args (map).
func argsFromWire(raw []any) Args {
	if len(raw) == 0 {
		return Args{}
	}
	// If the first element is a map, use it directly as Args.
	if m, ok := raw[0].(map[string]any); ok {
		return Args(m)
	}
	// Fallback for positional args: index them by position.
	result := make(Args, len(raw))
	for i, v := range raw {
		result[fmt.Sprintf("%d", i)] = v
	}
	return result
}
