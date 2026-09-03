package ojs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DurableContext provides deterministic execution support within a job handler.
// Non-deterministic operations (time, randomness, external calls) are recorded
// on first execution and replayed from the checkpoint on retry, ensuring
// idempotent re-execution after crashes.
type DurableContext struct {
	mu        sync.Mutex
	parent    context.Context
	transport *transport
	jobID     string
	attempt   int
	entries   []sideEffectEntry
	cursor    int
	replaying bool

	// hasCheckpoint, stepIndex, and state retain the decoded checkpoint this
	// job resumed from -- the exact values a prior attempt passed to
	// Checkpoint(stepIndex, state) -- alongside the replay log. Earlier
	// versions of this type discarded both once the replay log was extracted,
	// so a handler that checkpoints application-level progress (rather than
	// relying solely on Now/Random/SideEffect calls to imply progress) had no
	// way to learn where a resumed attempt should continue from, and
	// unconditionally redid every step. See HasCheckpoint, StepIndex,
	// StateRaw, and LoadState.
	hasCheckpoint bool
	stepIndex     int
	state         json.RawMessage

	// migrationErr records a failure to rewrite a recovered legacy checkpoint
	// in the canonical format. Replay is unaffected by it, so it must not stop
	// the handler, but it must not vanish either: see MigrationError.
	migrationErr error
}

type sideEffectEntry struct {
	Seq    int             `json:"seq"`
	Type   string          `json:"type"`
	Key    string          `json:"key,omitempty"`
	Result json.RawMessage `json:"result"`
}

// checkpointState is the SDK-owned state stored inside the standard OJS
// checkpoint "state" field. Keeping replay metadata inside state preserves the
// protocol's opaque-state contract without inventing non-standard top-level
// fields.
type checkpointState struct {
	Version   int               `json:"ojs_go_durable_version"`
	State     json.RawMessage   `json:"state"`
	StepIndex int               `json:"step_index"`
	ReplayLog []sideEffectEntry `json:"replay_log,omitempty"`
	Attempt   int               `json:"attempt"`
}

type checkpointRequest struct {
	State checkpointState `json:"state"`
}

// legacyCheckpointState is the pre-standard state this SDK wrote before the
// checkpoint binding moved to the normative job checkpoint resource: the replay
// log lived in `metadata._replay_log` as an embedded JSON *string* and carried
// no version marker.
//
// It is read-only. Nothing writes this shape any more; it exists so a job that
// checkpointed under an older SDK can still be resumed deterministically.
type legacyCheckpointState struct {
	StepIndex int               `json:"step_index"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	State     json.RawMessage   `json:"state,omitempty"`
}

const (
	durableCheckpointVersion = 1

	// durableLegacyVersion is the version a legacy state decodes as: the marker
	// simply was not written, so the field is absent and reads back as zero.
	durableLegacyVersion = 0

	// legacyReplayLogKey is the metadata key the legacy encoding used.
	legacyReplayLogKey = "_replay_log"
)

// durableRestore is a decoded checkpoint ready to be installed on a context.
type durableRestore struct {
	entries   []sideEffectEntry
	stepIndex int
	state     json.RawMessage

	// legacy marks a checkpoint that was decoded from the pre-standard
	// encoding and therefore still has to be written back canonically.
	legacy bool
}

// newDurableContext creates a DurableContext, loading any existing checkpoint.
// The parent context is used for checkpoint operations to ensure cancellation
// and distributed tracing propagate correctly.
func newDurableContext(parent context.Context, transport *transport, jobID string, attempt int) (*DurableContext, error) {
	dc := &DurableContext{
		parent:    parent,
		transport: transport,
		jobID:     jobID,
		attempt:   attempt,
	}

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	restore, err := loadDurableCheckpoint(ctx, transport, jobID)
	if err != nil {
		return nil, err
	}
	if restore == nil {
		return dc, nil
	}
	if err := validateReplaySequence(jobID, restore.entries); err != nil {
		return nil, err
	}

	dc.entries = restore.entries
	dc.replaying = len(restore.entries) > 0
	dc.hasCheckpoint = true
	dc.stepIndex = restore.stepIndex
	dc.state = restore.state

	if restore.legacy {
		// Migrate forward so the next attempt reads the canonical encoding. The
		// replay log is already loaded, so a failure here changes nothing about
		// how this attempt replays: it is recorded, not raised.
		dc.migrationErr = dc.Checkpoint(restore.stepIndex, restore.state)
	}
	return dc, nil
}

// MigrationError reports a failure to rewrite a recovered legacy checkpoint in
// the canonical format, or nil if no migration was needed or it succeeded.
//
// Deterministic replay for the current attempt is unaffected either way — the
// recovered replay log is used regardless — so this is diagnostic: it tells an
// operator that the job is still checkpointed in the legacy encoding.
func (dc *DurableContext) MigrationError() error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.migrationErr
}

// HasCheckpoint reports whether this job resumed from an existing checkpoint,
// as opposed to starting a fresh attempt with nothing recorded.
//
// This is deliberately independent of IsReplaying: a handler may have
// checkpointed application progress via Checkpoint(stepIndex, state) without
// ever calling Now, Random, or SideEffect, in which case the replay log is
// empty (IsReplaying is false) even though a checkpoint — and a StepIndex to
// resume from — genuinely exists.
func (dc *DurableContext) HasCheckpoint() bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.hasCheckpoint
}

// StepIndex returns the stepIndex most recently passed to Checkpoint by a
// prior attempt, or 0 if the job has no checkpoint.
//
// A durable handler that checkpoints after each logical step typically
// compares StepIndex against its own step numbering to skip steps that
// already completed, rather than depending entirely on Now/Random/SideEffect
// replay to make re-execution a no-op:
//
//	if dc.StepIndex() < 1 {
//	    // do step 1
//	    dc.Checkpoint(1, state)
//	}
//	if dc.StepIndex() < 2 {
//	    // do step 2
//	    dc.Checkpoint(2, state)
//	}
func (dc *DurableContext) StepIndex() int {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.stepIndex
}

// StateRaw returns a copy of the raw JSON application state most recently
// passed to Checkpoint by a prior attempt, or nil if the job has no checkpoint
// or the checkpoint carried no state. Use LoadState to decode it into a Go
// value.
func (dc *DurableContext) StateRaw() json.RawMessage {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return append(json.RawMessage(nil), dc.state...)
}

// LoadState decodes the checkpointed application state — the value most
// recently passed as Checkpoint's state argument by a prior attempt — into
// target, which must be a non-nil pointer.
//
// If the job has no checkpoint, or the checkpoint carried no state, LoadState
// leaves target untouched and returns nil rather than an error: callers that
// need to distinguish "nothing to load" from "loaded the zero value" should
// check HasCheckpoint first.
func (dc *DurableContext) LoadState(target any) error {
	dc.mu.Lock()
	state := append(json.RawMessage(nil), dc.state...)
	jobID := dc.jobID
	dc.mu.Unlock()

	if len(state) == 0 {
		return nil
	}
	if err := json.Unmarshal(state, target); err != nil {
		return fmt.Errorf("ojs: decode durable checkpoint state for job %q: %w", jobID, err)
	}
	return nil
}

// loadDurableCheckpoint reads the job's checkpoint, preferring the standard
// resource and falling back to the legacy resume endpoint when the standard one
// reports that no checkpoint exists.
//
// It returns (nil, nil) when the job genuinely has no checkpoint.
func loadDurableCheckpoint(ctx context.Context, tp *transport, jobID string) (*durableRestore, error) {
	var resp struct {
		Checkpoint *struct {
			State json.RawMessage `json:"state"`
		} `json:"checkpoint,omitempty"`
	}

	err := tp.get(ctx, durableCheckpointPath(jobID), &resp)
	switch {
	case err == nil:
		if resp.Checkpoint == nil {
			return nil, fmt.Errorf("ojs: durable checkpoint for job %q is missing checkpoint data", jobID)
		}
		return decodeCheckpointState(jobID, resp.Checkpoint.State)
	case httpStatusIs(err, http.StatusNotFound):
		// Per the durable-execution spec 404 means "no checkpoint" — but a job
		// checkpointed by an older SDK is stored under the legacy resource,
		// where the standard one cannot see it.
		return loadLegacyDurableCheckpoint(ctx, tp, jobID)
	default:
		return nil, fmt.Errorf("ojs: load durable checkpoint for job %q: %w", jobID, err)
	}
}

// decodeCheckpointState decodes the SDK state stored in a standard checkpoint,
// accepting both the canonical version and the unversioned legacy encoding.
func decodeCheckpointState(jobID string, raw json.RawMessage) (*durableRestore, error) {
	var saved checkpointState
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, fmt.Errorf("ojs: decode durable checkpoint state for job %q: %w", jobID, err)
	}

	switch saved.Version {
	case durableCheckpointVersion:
		return &durableRestore{
			entries:   saved.ReplayLog,
			stepIndex: saved.StepIndex,
			state:     saved.State,
		}, nil
	case durableLegacyVersion:
		return decodeLegacyCheckpointState(jobID, raw)
	default:
		return nil, fmt.Errorf(
			"ojs: durable checkpoint for job %q has unsupported SDK state version %d",
			jobID, saved.Version,
		)
	}
}

// decodeLegacyCheckpointState reads an unversioned state written by an older
// SDK. State that carries no legacy replay log is not ours: it keeps reporting
// the unsupported-version error rather than being silently adopted.
func decodeLegacyCheckpointState(jobID string, raw json.RawMessage) (*durableRestore, error) {
	var legacy legacyCheckpointState
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, fmt.Errorf("ojs: decode legacy durable checkpoint state for job %q: %w", jobID, err)
	}

	logData, ok := legacy.Metadata[legacyReplayLogKey]
	if !ok {
		return nil, fmt.Errorf(
			"ojs: durable checkpoint for job %q has unsupported SDK state version %d",
			jobID, durableLegacyVersion,
		)
	}

	entries, err := decodeLegacyReplayLog(jobID, logData)
	if err != nil {
		return nil, err
	}
	return &durableRestore{
		entries:   entries,
		stepIndex: legacy.StepIndex,
		state:     legacy.State,
		legacy:    true,
	}, nil
}

// loadLegacyDurableCheckpoint reads the pre-standard resume endpoint.
//
// It returns (nil, nil) both when that endpoint does not exist — every
// spec-compliant server that never implemented it — and when it reports no
// checkpoint. A checkpoint that exists but cannot be decoded is an error: the
// alternative is re-running recorded side effects.
func loadLegacyDurableCheckpoint(ctx context.Context, tp *transport, jobID string) (*durableRestore, error) {
	var resp struct {
		HasCheckpoint bool `json:"has_checkpoint"`
		Checkpoint    *struct {
			StepIndex int               `json:"step_index"`
			State     json.RawMessage   `json:"state,omitempty"`
			Metadata  map[string]string `json:"metadata,omitempty"`
		} `json:"checkpoint,omitempty"`
	}

	if err := tp.get(ctx, durableLegacyCheckpointPath(jobID), &resp); err != nil {
		if legacyCheckpointUnavailable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ojs: load legacy durable checkpoint for job %q: %w", jobID, err)
	}
	if !resp.HasCheckpoint || resp.Checkpoint == nil {
		return nil, nil
	}

	logData, ok := resp.Checkpoint.Metadata[legacyReplayLogKey]
	if !ok {
		// A legacy checkpoint with no SDK replay log carries nothing this
		// context can replay, and nothing worth rewriting.
		return nil, nil
	}

	entries, err := decodeLegacyReplayLog(jobID, logData)
	if err != nil {
		return nil, err
	}
	return &durableRestore{
		entries:   entries,
		stepIndex: resp.Checkpoint.StepIndex,
		state:     resp.Checkpoint.State,
		legacy:    true,
	}, nil
}

// decodeLegacyReplayLog decodes the embedded JSON string the legacy encoding
// used for the replay log.
func decodeLegacyReplayLog(jobID, logData string) ([]sideEffectEntry, error) {
	var entries []sideEffectEntry
	if err := json.Unmarshal([]byte(logData), &entries); err != nil {
		return nil, fmt.Errorf("ojs: decode legacy durable replay log for job %q: %w", jobID, err)
	}
	return entries, nil
}

// validateReplaySequence rejects a replay log whose entries are not densely
// ordered: replaying it would hand a handler the wrong recorded value.
func validateReplaySequence(jobID string, entries []sideEffectEntry) error {
	for i, entry := range entries {
		if entry.Seq != i {
			return fmt.Errorf(
				"ojs: durable replay log for job %q has sequence %d at position %d",
				jobID, entry.Seq, i,
			)
		}
	}
	return nil
}

// httpStatusIs reports whether err is an OJS error carrying the given status.
func httpStatusIs(err error, status int) bool {
	var ojsErr *Error
	return errors.As(err, &ojsErr) && ojsErr.HTTPStatus == status
}

// legacyCheckpointUnavailable reports whether err means the legacy resume
// endpoint is simply not served, as opposed to failing.
//
// 404 and 405 are the two ways a server that never implemented the endpoint
// answers it; anything else is a real failure and must not be read as "this job
// has no checkpoint".
func legacyCheckpointUnavailable(err error) bool {
	return httpStatusIs(err, http.StatusNotFound) ||
		httpStatusIs(err, http.StatusMethodNotAllowed)
}

func durableCheckpointPath(jobID string) string {
	return fmt.Sprintf("%s/jobs/%s/checkpoint", basePath, url.PathEscape(jobID))
}

// durableLegacyCheckpointPath is the pre-standard resume endpoint. It is read
// only: nothing writes to the legacy resource any more.
func durableLegacyCheckpointPath(jobID string) string {
	return fmt.Sprintf("%s/checkpoints/%s/resume", basePath, url.PathEscape(jobID))
}

// replayEntry returns the next entry when replay is active. A type mismatch is
// an execution-integrity failure: falling back to a live operation would append
// a new value in the middle of the saved history and make every later replay
// non-deterministic.
func (dc *DurableContext) replayEntry(expectedType string) (sideEffectEntry, bool) {
	if !dc.replaying || dc.cursor >= len(dc.entries) {
		return sideEffectEntry{}, false
	}

	entry := dc.entries[dc.cursor]
	if entry.Type != expectedType {
		panic(fmt.Sprintf(
			"ojs: durable replay type mismatch at position %d: checkpoint has %q but handler requested %q",
			dc.cursor, entry.Type, expectedType,
		))
	}
	return entry, true
}

// Now returns the current time deterministically.
// On first execution the real time is recorded; on replay the recorded time is returned.
func (dc *DurableContext) Now() time.Time {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if entry, ok := dc.replayEntry("time"); ok {
		var t time.Time
		if err := json.Unmarshal(entry.Result, &t); err != nil {
			panic(fmt.Sprintf("ojs: decode durable time at position %d: %v", dc.cursor, err))
		}
		dc.cursor++
		if dc.cursor >= len(dc.entries) {
			dc.replaying = false
		}
		return t
	}

	t := time.Now()
	data, _ := json.Marshal(t)
	dc.entries = append(dc.entries, sideEffectEntry{
		Seq: len(dc.entries), Type: "time", Key: "now", Result: data,
	})
	dc.replaying = false
	return t
}

// Random returns a deterministic random hex string of the specified byte length.
func (dc *DurableContext) Random(numBytes int) string {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if entry, ok := dc.replayEntry("random"); ok {
		var s string
		if err := json.Unmarshal(entry.Result, &s); err != nil {
			panic(fmt.Sprintf("ojs: decode durable random value at position %d: %v", dc.cursor, err))
		}
		dc.cursor++
		if dc.cursor >= len(dc.entries) {
			dc.replaying = false
		}
		return s
	}

	buf := make([]byte, numBytes)
	// crypto/rand.Read never returns an error as of Go 1.24 — it fills the
	// buffer entirely or terminates the process. The check keeps that contract
	// explicit: silently proceeding would append an all-zero value to the
	// durable replay log, which every later replay would then reproduce.
	if _, err := rand.Read(buf); err != nil {
		panic("ojs: durable: crypto/rand unavailable: " + err.Error())
	}
	s := hex.EncodeToString(buf)
	data, _ := json.Marshal(s)
	dc.entries = append(dc.entries, sideEffectEntry{
		Seq: len(dc.entries), Type: "random", Result: data,
	})
	dc.replaying = false
	return s
}

// SideEffect executes fn deterministically. On first execution, fn is called and
// the result is recorded. On replay, the recorded result is returned without
// calling fn. The result must be JSON-serializable.
//
// Example:
//
//	price, err := dc.SideEffect("fetch-price", func() (any, error) {
//	    return externalAPI.GetPrice(productID)
//	})
func (dc *DurableContext) SideEffect(key string, fn func() (any, error)) (json.RawMessage, error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.replaying && dc.cursor < len(dc.entries) {
		entry := dc.entries[dc.cursor]
		if entry.Type != "call" {
			return nil, fmt.Errorf(
				"ojs: durable replay type mismatch at position %d: checkpoint has %q but handler requested %q",
				dc.cursor, entry.Type, "call",
			)
		}
		// Strict key matching during replay: if both keys are set, they must match.
		// A mismatch means the code changed between the checkpoint save and replay,
		// which would silently return the wrong data.
		if key != "" && entry.Key != "" && entry.Key != key {
			return nil, fmt.Errorf("ojs: durable replay key mismatch at position %d: checkpoint has %q but code called %q — handler logic may have changed since checkpoint was saved", dc.cursor, entry.Key, key)
		}
		if !json.Valid(entry.Result) {
			return nil, fmt.Errorf("ojs: durable replay result at position %d is not valid JSON", dc.cursor)
		}
		dc.cursor++
		if dc.cursor >= len(dc.entries) {
			dc.replaying = false
		}
		return entry.Result, nil
	}

	dc.replaying = false

	result, err := fn()
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("ojs: marshal side effect %q result: %w", key, err)
	}

	dc.entries = append(dc.entries, sideEffectEntry{
		Seq: len(dc.entries), Type: "call", Key: key, Result: data,
	})
	return data, nil
}

// Checkpoint saves the current execution state to the server.
// Call this after completing an important step to enable resume from this point.
func (dc *DurableContext) Checkpoint(stepIndex int, state any) error {
	dc.mu.Lock()
	entriesCopy := make([]sideEffectEntry, len(dc.entries))
	copy(entriesCopy, dc.entries)
	dc.mu.Unlock()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("ojs: marshal checkpoint state: %w", err)
	}

	req := checkpointRequest{
		State: checkpointState{
			Version:   durableCheckpointVersion,
			State:     stateData,
			StepIndex: stepIndex,
			ReplayLog: entriesCopy,
			Attempt:   dc.attempt,
		},
	}

	ctx, cancel := context.WithTimeout(dc.parent, 5*time.Second)
	defer cancel()

	return dc.transport.post(ctx, durableCheckpointPath(dc.jobID), req, nil)
}

// Complete clears the checkpoint after successful job completion.
func (dc *DurableContext) Complete() error {
	ctx, cancel := context.WithTimeout(dc.parent, 5*time.Second)
	defer cancel()
	return dc.transport.delete(ctx, durableCheckpointPath(dc.jobID), nil)
}

// IsReplaying returns true if the context is currently replaying from a checkpoint.
func (dc *DurableContext) IsReplaying() bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.replaying && dc.cursor < len(dc.entries)
}

// --- DurableHandlerFunc ---

// DurableHandlerFunc is a job handler that receives a DurableContext for
// checkpoint-based durable execution.
type DurableHandlerFunc func(ctx JobContext, dc *DurableContext) error

// RegisterDurable registers a job handler with durable execution support.
// The handler receives a DurableContext that provides deterministic wrappers
// for non-deterministic operations.
//
// Example:
//
//	worker.RegisterDurable("etl.process", func(ctx ojs.JobContext, dc *ojs.DurableContext) error {
//	    // Step 1: Fetch data (result is recorded for replay)
//	    data, err := dc.SideEffect("fetch-data", func() (any, error) {
//	        return fetchFromAPI(ctx.Job.Args["url"].(string))
//	    })
//	    if err != nil {
//	        return err
//	    }
//	    dc.Checkpoint(1, map[string]any{"fetched": true})
//
//	    // Step 2: Transform
//	    transformed := transform(data)
//	    dc.Checkpoint(2, map[string]any{"transformed": true})
//
//	    // Step 3: Load
//	    if err := loadIntoDB(transformed); err != nil {
//	        return err
//	    }
//
//	    dc.Complete()
//	    ctx.SetResult(map[string]any{"records": 100})
//	    return nil
//	})
func (w *Worker) RegisterDurable(jobType string, handler DurableHandlerFunc) {
	w.Register(jobType, func(ctx JobContext) error {
		dc, err := newDurableContext(ctx.Context(), w.transport, ctx.Job.ID, ctx.Attempt)
		if err != nil {
			return err
		}
		return handler(ctx, dc)
	})
}
