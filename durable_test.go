package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func mustNewDurableContext(t *testing.T, tp *transport, jobID string, attempt int) *DurableContext {
	t.Helper()
	dc, err := newDurableContext(context.Background(), tp, jobID, attempt)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	return dc
}

func TestDurableContextNow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-1", 1)

	t1 := dc.Now()
	if t1.IsZero() {
		t.Error("expected non-zero time")
	}

	// Second call records a new entry
	t2 := dc.Now()
	if t2.IsZero() {
		t.Error("expected non-zero second time")
	}
}

func TestDurableContextRandom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-2", 1)

	r := dc.Random(16)
	if len(r) != 32 { // hex doubles bytes
		t.Errorf("expected 32 hex chars, got %d", len(r))
	}
}

func TestDurableContextSideEffect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-3", 1)

	var callCount int32
	result, err := dc.SideEffect("compute", func() (any, error) {
		atomic.AddInt32(&callCount, 1)
		return map[string]int{"value": 42}, nil
	})
	if err != nil {
		t.Fatalf("SideEffect: %v", err)
	}

	var parsed map[string]int
	json.Unmarshal(result, &parsed)
	if parsed["value"] != 42 {
		t.Errorf("expected 42, got %d", parsed["value"])
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Error("expected exactly 1 call")
	}
}

func TestDurableContextSideEffectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-4", 1)

	_, err := dc.SideEffect("fail", func() (any, error) {
		return nil, fmt.Errorf("external API error")
	})
	if err == nil {
		t.Error("expected error from side effect")
	}
}

func TestDurableContextReplayFromCheckpoint(t *testing.T) {
	// Simulate a checkpoint with a pre-recorded replay log
	replayLog := []sideEffectEntry{
		{Seq: 0, Type: "time", Key: "now", Result: json.RawMessage(`"2026-01-15T10:00:00Z"`)},
		{Seq: 1, Type: "random", Result: json.RawMessage(`"deadbeef"`)},
		{Seq: 2, Type: "call", Key: "api-call", Result: json.RawMessage(`{"price":99.99}`)},
	}
	logJSON, _ := json.Marshal(replayLog)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/ojs/v1/jobs/job-replay/checkpoint" {
			t.Errorf("path = %s, want standard checkpoint path", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"checkpoint": map[string]any{
				"state": map[string]any{
					"ojs_go_durable_version": durableCheckpointVersion,
					"state":                  nil,
					"step_index":             2,
					"replay_log":             json.RawMessage(logJSON),
					"attempt":                1,
				},
			},
		})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-replay", 2)

	if !dc.IsReplaying() {
		t.Fatal("expected to be in replay mode")
	}

	// Replay time
	t1 := dc.Now()
	if t1.Year() != 2026 || t1.Month() != 1 || t1.Day() != 15 {
		t.Errorf("unexpected replayed time: %v", t1)
	}

	// Replay random
	r := dc.Random(4) // bytes don't matter for replay
	if r != "deadbeef" {
		t.Errorf("expected deadbeef, got %s", r)
	}

	// Replay side effect (should NOT call fn)
	result, err := dc.SideEffect("api-call", func() (any, error) {
		t.Error("should not call fn during replay")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("SideEffect replay: %v", err)
	}

	var price map[string]float64
	json.Unmarshal(result, &price)
	if price["price"] != 99.99 {
		t.Errorf("expected 99.99, got %v", price["price"])
	}

	// After replay exhausted, should no longer be replaying
	if dc.IsReplaying() {
		t.Error("expected replay to be exhausted")
	}

	// New operations should record normally
	dc.Now() // this should work without error
}

func TestDurableContextCheckpoint(t *testing.T) {
	var saved checkpointRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/ojs/v1/jobs/job-cp/checkpoint" {
			t.Errorf("path = %s, want standard checkpoint path", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
			t.Errorf("decode checkpoint request: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"checkpoint": map[string]any{"job_id": "job-cp", "sequence": 1},
		})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := &DurableContext{
		parent:    context.Background(),
		transport: tp,
		jobID:     "job-cp",
		attempt:   1,
	}

	dc.Now()
	dc.Random(8)

	err := dc.Checkpoint(2, map[string]any{"step": "transform"})
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if saved.State.StepIndex != 2 || saved.State.Attempt != 1 {
		t.Errorf("saved checkpoint metadata = %+v", saved.State)
	}
	if saved.State.Version != durableCheckpointVersion {
		t.Errorf("saved checkpoint version = %d, want %d", saved.State.Version, durableCheckpointVersion)
	}
	if got := string(saved.State.State); got != `{"step":"transform"}` {
		t.Errorf("saved state = %s, want caller state", got)
	}
	if len(saved.State.ReplayLog) != 2 {
		t.Errorf("saved replay log length = %d, want 2", len(saved.State.ReplayLog))
	}
}

func TestDurableContextComplete(t *testing.T) {
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deleted = true
			if got := r.URL.EscapedPath(); got != "/ojs/v1/jobs/job-complete/checkpoint" {
				t.Errorf("path = %s, want standard checkpoint path", got)
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := mustNewDurableContext(t, tp, "job-complete", 1)

	err := dc.Complete()
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !deleted {
		t.Error("expected DELETE to be called")
	}
}

func TestRegisterDurable(t *testing.T) {
	w := NewWorker("http://localhost:8080")

	w.RegisterDurable("durable.job", func(ctx JobContext, dc *DurableContext) error {
		if dc == nil {
			t.Error("expected non-nil DurableContext")
		}
		return nil
	})

	// Verify handler is registered
	w.handlersMu.RLock()
	_, ok := w.handlers["durable.job"]
	w.handlersMu.RUnlock()

	if !ok {
		t.Error("expected durable handler to be registered")
	}
}

func TestDurableContextNoCheckpoint(t *testing.T) {
	// Per the durable execution spec, 404 means the job has no checkpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-no-cp", 1)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if dc.IsReplaying() {
		t.Fatal("new context unexpectedly entered replay mode")
	}
	if now := dc.Now(); now.IsZero() {
		t.Fatal("record mode did not produce a time")
	}
}

func TestRegisterDurableSkipsHandlerWhenCheckpointLoadFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "checkpoint service unavailable", http.StatusBadRequest)
	}))
	defer srv.Close()

	w := NewWorker(srv.URL)
	called := false
	w.RegisterDurable("durable.job", func(JobContext, *DurableContext) error {
		called = true
		return nil
	})

	w.handlersMu.RLock()
	handler := w.handlers["durable.job"]
	w.handlersMu.RUnlock()

	err := handler(NewJobContextForTest(Job{
		ID:      "job-load-failure",
		Type:    "durable.job",
		Attempt: 1,
	}))
	if err == nil {
		t.Fatal("expected checkpoint load error")
	}
	if called {
		t.Fatal("durable handler ran without a verified replay state")
	}
}

func TestDurableCheckpointLoadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "checkpoint service unavailable", http.StatusBadRequest)
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-load-failure", 1)
	if err == nil {
		t.Fatal("expected checkpoint load error")
	}
	if dc != nil {
		t.Fatalf("newDurableContext returned context %#v after load failure", dc)
	}
	if got := err.Error(); !strings.Contains(got, `load durable checkpoint for job "job-load-failure"`) {
		t.Fatalf("error = %q, want job-specific checkpoint context", got)
	}
}

func TestDurableCheckpointRejectsForeignState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"checkpoint": map[string]any{
				"state": map[string]any{"processed_count": 5000},
			},
		})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-foreign-state", 1)
	if err == nil || !strings.Contains(err.Error(), "unsupported SDK state version") {
		t.Fatalf("newDurableContext error = %v, want unsupported state version", err)
	}
	if dc != nil {
		t.Fatalf("newDurableContext returned context %#v for incompatible state", dc)
	}
}

func TestDurableCheckpointPathEscapesJobID(t *testing.T) {
	if got, want := durableCheckpointPath("job/with?reserved"), "/ojs/v1/jobs/job%2Fwith%3Freserved/checkpoint"; got != want {
		t.Fatalf("durableCheckpointPath = %q, want %q", got, want)
	}
}

func TestDurableReplayTypeMismatchFailsFast(t *testing.T) {
	tests := []struct {
		name string
		call func(*DurableContext)
	}{
		{name: "now", call: func(dc *DurableContext) { dc.Now() }},
		{name: "random", call: func(dc *DurableContext) { dc.Random(4) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := &DurableContext{
				entries:   []sideEffectEntry{{Seq: 0, Type: "call", Result: json.RawMessage(`null`)}},
				replaying: true,
			}

			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("expected replay mismatch panic")
				}
				if got := fmt.Sprint(recovered); !strings.Contains(got, "durable replay type mismatch") {
					t.Fatalf("panic = %q, want replay type mismatch", got)
				}
			}()
			tt.call(dc)
		})
	}
}

func TestDurableSideEffectRejectsReplayTypeMismatch(t *testing.T) {
	dc := &DurableContext{
		entries:   []sideEffectEntry{{Seq: 0, Type: "time", Result: json.RawMessage(`"2026-01-15T10:00:00Z"`)}},
		replaying: true,
	}
	called := false

	_, err := dc.SideEffect("fetch", func() (any, error) {
		called = true
		return "live", nil
	})
	if err == nil || !strings.Contains(err.Error(), "durable replay type mismatch") {
		t.Fatalf("SideEffect error = %v, want replay type mismatch", err)
	}
	if called {
		t.Fatal("SideEffect executed live code after a replay mismatch")
	}
}
