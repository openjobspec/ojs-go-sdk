package ojs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// This file proves that DurableContext retains a resumed checkpoint's decoded
// stepIndex and application state (not just its replay log), and that a
// step-driven durable handler built on top of that information does not
// repeat work a prior attempt already completed. Earlier versions of
// newDurableContext computed restore.stepIndex/restore.state only to feed a
// legacy migration write; a handler resuming purely on checkpointed step
// progress (as opposed to inferring progress from Now/Random/SideEffect
// replay) had no way to observe them at all.

// pipelineState is example application state a durable handler might
// checkpoint alongside its step index.
type pipelineState struct {
	Processed int    `json:"processed"`
	Phase     string `json:"phase"`
}

// runPipeline is a representative step-driven durable handler: it consults
// dc.StepIndex() before doing each step's work, so a resumed attempt skips
// whatever a prior attempt already checkpointed past. stepRan records which
// steps actually executed on this call, so tests can assert completed steps
// were not repeated.
func runPipeline(dc *DurableContext, stepRan map[int]bool) error {
	var st pipelineState
	if err := dc.LoadState(&st); err != nil {
		return err
	}

	if dc.StepIndex() < 1 {
		stepRan[1] = true
		st.Phase = "extracted"
		st.Processed = 10
		if err := dc.Checkpoint(1, st); err != nil {
			return err
		}
	}
	if dc.StepIndex() < 2 {
		stepRan[2] = true
		st.Phase = "transformed"
		st.Processed = 20
		if err := dc.Checkpoint(2, st); err != nil {
			return err
		}
	}
	if dc.StepIndex() < 3 {
		stepRan[3] = true
		st.Phase = "loaded"
		st.Processed = 30
		if err := dc.Checkpoint(3, st); err != nil {
			return err
		}
	}
	return nil
}

// canonicalCheckpointServer serves a fixed canonical (v1) checkpoint at the
// standard resource and records every checkpoint written back to it.
type canonicalCheckpointServer struct {
	initial  *checkpointState // nil means "no checkpoint yet"
	written  []checkpointState
	notFound bool
}

func (s *canonicalCheckpointServer) handler(t *testing.T, jobID string) http.HandlerFunc {
	t.Helper()
	path := durableCheckpointPath(jobID)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		if r.URL.EscapedPath() != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if s.initial == nil || s.notFound {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no checkpoint"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"checkpoint": map[string]any{"state": s.initial},
			})
		case http.MethodPost:
			var req checkpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode checkpoint write: %v", err)
			}
			s.written = append(s.written, req.State)
			_ = json.NewEncoder(w).Encode(map[string]any{"checkpoint": map[string]any{"sequence": len(s.written)}})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// TestDurableContextRetainsCanonicalStepIndexAndState is the direct
// characterization of the fix: a canonical checkpoint's stepIndex and
// application state must reach the DurableContext, not just its replay log.
func TestDurableContextRetainsCanonicalStepIndexAndState(t *testing.T) {
	stateJSON, err := json.Marshal(pipelineState{Processed: 20, Phase: "transformed"})
	if err != nil {
		t.Fatalf("marshal fixture state: %v", err)
	}
	srv := &canonicalCheckpointServer{initial: &checkpointState{
		Version:   durableCheckpointVersion,
		State:     stateJSON,
		StepIndex: 2,
		Attempt:   1,
	}}
	ts := httptest.NewServer(srv.handler(t, "job-resume"))
	defer ts.Close()

	tp := newTransport(ts.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-resume", 2)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}

	if !dc.HasCheckpoint() {
		t.Fatal("HasCheckpoint() = false, want true: a checkpoint exists")
	}
	if got := dc.StepIndex(); got != 2 {
		t.Fatalf("StepIndex() = %d, want 2", got)
	}
	if got := string(dc.StateRaw()); got != string(stateJSON) {
		t.Fatalf("StateRaw() = %s, want %s", got, stateJSON)
	}

	var st pipelineState
	if err := dc.LoadState(&st); err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.Processed != 20 || st.Phase != "transformed" {
		t.Fatalf("LoadState decoded %+v, want Processed=20 Phase=transformed", st)
	}
}

func TestDurableContextStateRawReturnsDefensiveCopy(t *testing.T) {
	stateJSON := json.RawMessage(`{"processed":20,"phase":"transformed"}`)
	dc := &DurableContext{
		jobID: "job-state-copy",
		state: append(json.RawMessage(nil), stateJSON...),
	}

	raw := dc.StateRaw()
	raw[0] = '['

	if got := dc.StateRaw(); string(got) != string(stateJSON) {
		t.Fatalf("StateRaw() after caller mutation = %s, want %s", got, stateJSON)
	}

	var st pipelineState
	if err := dc.LoadState(&st); err != nil {
		t.Fatalf("LoadState after caller mutation: %v", err)
	}
	if st.Processed != 20 || st.Phase != "transformed" {
		t.Fatalf("LoadState decoded %+v after caller mutation, want original state", st)
	}
}

type blockingStateCapture struct {
	started chan struct{}
	resume  chan struct{}
	data    []byte
}

func (c *blockingStateCapture) UnmarshalJSON(data []byte) error {
	close(c.started)
	<-c.resume
	c.data = append([]byte(nil), data...)
	return nil
}

func TestDurableContextLoadStateDecodesDefensiveCopyAfterUnlock(t *testing.T) {
	original := json.RawMessage(`{"value":"original"}`)
	replacement := json.RawMessage(`{"value":"mutated!"}`)
	dc := &DurableContext{
		jobID: "job-load-copy",
		state: append(json.RawMessage(nil), original...),
	}
	target := &blockingStateCapture{
		started: make(chan struct{}),
		resume:  make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- dc.LoadState(target)
	}()

	<-target.started
	dc.mu.Lock()
	copy(dc.state, replacement)
	dc.mu.Unlock()
	close(target.resume)

	if err := <-errCh; err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := string(target.data); got != string(original) {
		t.Fatalf("LoadState decoded %s, want pre-unlock copy %s", got, original)
	}
}

func TestDurableContextStateAccessIsRaceFree(t *testing.T) {
	stateJSON := json.RawMessage(`{"processed":20,"phase":"transformed"}`)
	dc := &DurableContext{
		jobID: "job-state-race",
		state: append(json.RawMessage(nil), stateJSON...),
	}
	callerState := dc.StateRaw()

	const iterations = 1000
	start := make(chan struct{})
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			callerState[25] = 'u'
			callerState[25] = 't'
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			var st pipelineState
			if err := dc.LoadState(&st); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if st.Processed != 20 || st.Phase != "transformed" {
				select {
				case errCh <- errors.New("LoadState observed caller-mutated state"):
				default:
				}
				return
			}
		}
	}()

	close(start)
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestDurableContextNoCheckpointReportsZeroValues locks the fresh-attempt
// side: no checkpoint means HasCheckpoint is false, StepIndex is 0, StateRaw
// is empty, and LoadState is a safe no-op rather than an error.
func TestDurableContextNoCheckpointReportsZeroValues(t *testing.T) {
	srv := &canonicalCheckpointServer{initial: nil}
	ts := httptest.NewServer(srv.handler(t, "job-fresh"))
	defer ts.Close()

	tp := newTransport(ts.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-fresh", 1)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}

	if dc.HasCheckpoint() {
		t.Error("HasCheckpoint() = true, want false: no checkpoint was ever written")
	}
	if got := dc.StepIndex(); got != 0 {
		t.Errorf("StepIndex() = %d, want 0", got)
	}
	if got := dc.StateRaw(); got != nil {
		t.Errorf("StateRaw() = %s, want nil", got)
	}

	st := pipelineState{Processed: -1, Phase: "untouched"}
	if err := dc.LoadState(&st); err != nil {
		t.Fatalf("LoadState on a fresh context: %v", err)
	}
	if st.Processed != -1 || st.Phase != "untouched" {
		t.Errorf("LoadState mutated target on a fresh context: %+v", st)
	}
}

// TestDurableResumeSkipsCompletedCanonicalSteps proves the actual behavior the
// retained stepIndex enables: a step-driven handler resuming from a canonical
// checkpoint does not repeat steps a prior attempt already completed, using
// only dc.StepIndex() -- not the Now/Random/SideEffect replay log, which this
// pipeline never touches.
// runPipelineAttempt creates a DurableContext for the given job/attempt and
// runs runPipeline against it, returning the context (so callers can inspect
// e.g. StepIndex) and which steps actually executed.
func runPipelineAttempt(t *testing.T, tp *transport, jobID string, attempt int) (*DurableContext, map[int]bool) {
	t.Helper()
	dc, err := newDurableContext(context.Background(), tp, jobID, attempt)
	if err != nil {
		t.Fatalf("newDurableContext (attempt %d): %v", attempt, err)
	}
	ran := map[int]bool{}
	if err := runPipeline(dc, ran); err != nil {
		t.Fatalf("runPipeline (attempt %d): %v", attempt, err)
	}
	return dc, ran
}

// assertFinalCheckpoint asserts exactly one checkpoint was written, carrying
// the given step index and application state.
func assertFinalCheckpoint(t *testing.T, written []checkpointState, wantStepIndex int, want pipelineState) {
	t.Helper()
	if len(written) != 1 {
		t.Fatalf("checkpoints written = %d, want exactly 1", len(written))
	}
	if written[0].StepIndex != wantStepIndex {
		t.Fatalf("final checkpoint step_index = %d, want %d", written[0].StepIndex, wantStepIndex)
	}
	var got pipelineState
	if err := json.Unmarshal(written[0].State, &got); err != nil {
		t.Fatalf("decode final state: %v", err)
	}
	if got != want {
		t.Fatalf("final state = %+v, want %+v", got, want)
	}
}

func TestDurableResumeSkipsCompletedCanonicalSteps(t *testing.T) {
	srv := &canonicalCheckpointServer{}
	ts := httptest.NewServer(srv.handler(t, "job-pipeline"))
	defer ts.Close()
	tp := newTransport(ts.URL, clientConfig{})

	// Attempt 1: nothing checkpointed yet, every step must run.
	_, ran1 := runPipelineAttempt(t, tp, "job-pipeline", 1)
	if !ran1[1] || !ran1[2] || !ran1[3] {
		t.Fatalf("attempt 1 ran steps %v, want all three steps to run from a fresh start", ran1)
	}
	if len(srv.written) != 3 {
		t.Fatalf("checkpoints written = %d, want 3 (one per step)", len(srv.written))
	}

	// Simulate a crash after step 2's checkpoint: the server holds step 2's
	// state, and a new attempt resumes against it.
	srv.initial = &srv.written[1] // step 2's checkpoint (index 1 of 0,1,2)
	srv.written = nil

	dc2, ran2 := runPipelineAttempt(t, tp, "job-pipeline", 2)
	if got := dc2.StepIndex(); got != 2 {
		t.Fatalf("resumed StepIndex() = %d, want 2", got)
	}
	if ran2[1] || ran2[2] {
		t.Fatalf("attempt 2 ran steps %v, want steps 1 and 2 skipped: they were already checkpointed", ran2)
	}
	if !ran2[3] {
		t.Fatal("attempt 2 did not run step 3, which had not yet completed")
	}
	assertFinalCheckpoint(t, srv.written, 3, pipelineState{Processed: 30, Phase: "loaded"})
}

// TestDurableLegacyResumeRetainsStepIndexAndSkipsCompletedSteps is the legacy
// counterpart: a checkpoint recovered from the pre-standard resume endpoint
// must retain its stepIndex/state exactly like a canonical one, so a
// step-driven handler resuming across an SDK upgrade also skips completed
// steps rather than restarting the pipeline from zero.
func TestDurableLegacyResumeRetainsStepIndexAndSkipsCompletedSteps(t *testing.T) {
	legacyState, err := json.Marshal(pipelineState{Processed: 20, Phase: "transformed"})
	if err != nil {
		t.Fatalf("marshal legacy state fixture: %v", err)
	}
	s := &legacyServer{resume: map[string]any{
		"has_checkpoint": true,
		"checkpoint": map[string]any{
			"step_index": 2,
			"state":      json.RawMessage(legacyState),
			"metadata": map[string]string{
				legacyReplayLogKey: "[]", // this pipeline never calls Now/Random/SideEffect
			},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 3)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}

	if !dc.HasCheckpoint() {
		t.Fatal("HasCheckpoint() = false, want true for a recovered legacy checkpoint")
	}
	if got := dc.StepIndex(); got != 2 {
		t.Fatalf("StepIndex() = %d, want 2 (preserved from the legacy checkpoint)", got)
	}
	var st pipelineState
	if err := dc.LoadState(&st); err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.Processed != 20 || st.Phase != "transformed" {
		t.Fatalf("LoadState = %+v, want the legacy state verbatim", st)
	}

	ran := map[int]bool{}
	if err := runPipeline(dc, ran); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	if ran[1] || ran[2] {
		t.Fatalf("ran steps %v, want steps 1 and 2 skipped: the legacy checkpoint already completed them", ran)
	}
	if !ran[3] {
		t.Fatal("step 3 did not run, but it had not completed in the legacy checkpoint")
	}

	// The forward migration write must also carry the resumed stepIndex, not
	// just the (empty) replay log.
	migrated := s.migratedCheckpoints()
	if len(migrated) == 0 {
		t.Fatal("expected the legacy checkpoint to be migrated forward")
	}
	if migrated[0].State.StepIndex != 2 {
		t.Fatalf("migrated checkpoint step_index = %d, want 2", migrated[0].State.StepIndex)
	}
}
