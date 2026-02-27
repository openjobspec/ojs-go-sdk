package ojs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDurableContextNow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Resume endpoint returns no checkpoint
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-1", 1)

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
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-2", 1)

	r := dc.Random(16)
	if len(r) != 32 { // hex doubles bytes
		t.Errorf("expected 32 hex chars, got %d", len(r))
	}
}

func TestDurableContextSideEffect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-3", 1)

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
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-4", 1)

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
		json.NewEncoder(w).Encode(map[string]any{
			"has_checkpoint": true,
			"checkpoint": map[string]any{
				"metadata": map[string]string{
					"_replay_log": string(logJSON),
				},
			},
		})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-replay", 2)

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
	var savedBody json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			body, _ := json.Marshal(map[string]any{"job_id": "job-cp", "version": 1})
			savedBody = body
			w.Write(body)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-cp", 1)

	dc.Now()
	dc.Random(8)

	err := dc.Checkpoint(2, map[string]any{"step": "transform"})
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if savedBody == nil {
		t.Error("expected checkpoint to be saved")
	}
}

func TestDurableContextComplete(t *testing.T) {
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deleted = true
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"has_checkpoint": false})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-complete", 1)

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

func TestDurableContextNoCheckpointServer(t *testing.T) {
	// Server that returns 404 for checkpoint endpoints
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc := newDurableContext(tp, "job-no-cp", 1)

	// Should work in record mode even without checkpoint server
	if dc.IsReplaying() {
		t.Error("expected record mode when checkpoint server unavailable")
	}

	t1 := dc.Now()
	if t1.IsZero() {
		t.Error("expected valid time in record mode")
	}
}
