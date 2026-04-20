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
//
// Only that exact shape — an array of exactly one object — maps to Args. Any
// other array is positional and is preserved verbatim in [Job.RawArgs]; Args
// then holds an index-keyed view of it that is a convenience, not the source of
// truth.
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
	//
	// It is authoritative for every args array except the canonical
	// single-object form, so a job decoded from the wire re-encodes to exactly
	// the bytes it arrived as.
	RawArgs []any `json:"-"`

	// PreviousState is set on cancel responses.
	PreviousState string `json:"previous_state,omitempty"`
}

// jobJSON is an alias used for custom JSON marshaling/unmarshaling.
type jobJSON struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	State         JobState        `json:"state"`
	Queue         string          `json:"queue"`
	Priority      int             `json:"priority"`
	Attempt       int             `json:"attempt"`
	MaxAttempts   int             `json:"max_attempts"`
	TimeoutMS     int             `json:"timeout_ms,omitempty"`
	Tags          []string        `json:"tags,omitempty"`
	Meta          map[string]any  `json:"meta,omitempty"`
	Result        map[string]any  `json:"result,omitempty"`
	Error         *JobError       `json:"error,omitempty"`
	CreatedAt     *time.Time      `json:"created_at,omitempty"`
	EnqueuedAt    *time.Time      `json:"enqueued_at,omitempty"`
	StartedAt     *time.Time      `json:"started_at,omitempty"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
	CancelledAt   *time.Time      `json:"cancelled_at,omitempty"`
	DiscardedAt   *time.Time      `json:"discarded_at,omitempty"`
	ScheduledAt   *time.Time      `json:"scheduled_at,omitempty"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
	Args          json.RawMessage `json:"args,omitempty"`
	PreviousState string          `json:"previous_state,omitempty"`
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
	argsBytes, err := json.Marshal(j.wireArgs())
	if err != nil {
		return nil, fmt.Errorf("ojs: failed to marshal job args: %w", err)
	}
	raw.Args = argsBytes
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
		if err := json.Unmarshal(raw.Args, &arr); err != nil {
			return fmt.Errorf("ojs: job %q has malformed args (OJS requires a JSON array): %w", j.ID, err)
		}
		j.RawArgs = arr
		j.Args = argsFromWire(arr)
	}
	return nil
}

// wireArgs returns the OJS wire representation of this job's arguments.
//
// When the job was decoded from positional arguments, RawArgs holds the only
// faithful representation: Args is a synthetic index->value map, and round
// tripping through it would rewrite [1,2,3] as [{"0":1,"1":2,"2":3}]. In that
// case the original array is emitted unchanged. Args in the canonical
// single-object form aliases RawArgs[0], so Args stays authoritative there and
// caller mutations are kept.
func (j Job) wireArgs() []any {
	if isPositionalWireArgs(j.RawArgs) {
		return j.RawArgs
	}
	// A decoded empty object (`[{}]`, one argument) is not the same wire value
	// as no arguments at all (`[]`), and an empty Args map cannot tell the two
	// apart. RawArgs can, so it decides this case.
	if len(j.Args) == 0 && len(j.RawArgs) > 0 {
		return j.RawArgs
	}
	return argsToWire(j.Args)
}

// isPositionalWireArgs reports whether raw is anything other than the canonical
// single-object args form.
//
// A leading object is not enough. `[{"a":1}, 2, 3]` used to be treated as the
// object form, which meant Args captured only the first element and re-encoding
// through it silently dropped everything after it. Only an array of exactly one
// object round-trips through Args; every other array is emitted verbatim.
func isPositionalWireArgs(raw []any) bool {
	if len(raw) == 0 {
		return false
	}
	_, canonical := canonicalObjectArgs(raw)
	return !canonical
}

// canonicalObjectArgs returns the object of the OJS canonical args form — an
// array holding exactly one JSON object — and whether raw is in that form.
func canonicalObjectArgs(raw []any) (map[string]any, bool) {
	if len(raw) != 1 {
		return nil, false
	}
	m, ok := raw[0].(map[string]any)
	return m, ok
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
	if len(a) == 0 {
		return []any{}
	}
	return []any{map[string]any(a)}
}

// argsFromWire converts the OJS wire format (JSON array) to Args (map).
//
// Only the canonical single-object array becomes the map form and aliases the
// decoded object. Every other array — including one that merely *starts* with
// an object — is indexed by position, because collapsing it into its first
// element would discard the remaining arguments the moment the job is
// re-encoded. Those jobs keep RawArgs as their authoritative representation.
func argsFromWire(raw []any) Args {
	if len(raw) == 0 {
		return Args{}
	}
	if m, ok := canonicalObjectArgs(raw); ok {
		return Args(m)
	}
	// Positional args: index them by position.
	result := make(Args, len(raw))
	for i, v := range raw {
		result[fmt.Sprintf("%d", i)] = v
	}
	return result
}
