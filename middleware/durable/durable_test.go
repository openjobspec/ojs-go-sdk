package durable

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func TestSaveAndRestore(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	dc := &DurableContext{store: store, jobID: "job-1"}

	type State struct {
		Count int    `json:"count"`
		Batch string `json:"batch"`
	}

	err := dc.Save(State{Count: 42, Batch: "B3"}, 5)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	var restored State
	step, ok := dc.Restore(&restored)
	if !ok {
		t.Fatal("expected to restore")
	}
	if step != 5 {
		t.Errorf("expected step 5, got %d", step)
	}
	if restored.Count != 42 || restored.Batch != "B3" {
		t.Errorf("unexpected state: %+v", restored)
	}
}

func TestClear(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	dc := &DurableContext{store: store, jobID: "job-1"}
	dc.Save(map[string]int{"x": 1}, 1)
	dc.Clear()

	var state map[string]int
	_, ok := dc.Restore(&state)
	if ok {
		t.Error("expected not found after clear")
	}
}

func TestRestoreNoCheckpoint(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	dc := &DurableContext{store: store, jobID: "new-job"}

	var state map[string]int
	_, ok := dc.Restore(&state)
	if ok {
		t.Error("expected false for new job")
	}
}

func TestMiddlewareInjectsContext(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	mw := Middleware(store)

	// Pre-save a checkpoint
	store.Save("job-test", json.RawMessage(`{"step":3}`), 3)

	var captured *DurableContext
	handler := func(ctx ojs.JobContext) error {
		captured = FromContext(ctx)
		return nil
	}

	ctx := ojs.JobContext{
		Job: ojs.Job{ID: "job-test", Type: "test"},
	}

	err := mw(ctx, handler)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if captured == nil {
		t.Fatal("expected DurableContext to be injected")
	}

	var state map[string]int
	step, ok := captured.Restore(&state)
	if !ok {
		t.Fatal("expected to restore from middleware-injected context")
	}
	if step != 3 {
		t.Errorf("expected step 3, got %d", step)
	}
}

func TestFromContextNoMiddleware(t *testing.T) {
	ctx := ojs.JobContext{
		Job: ojs.Job{ID: "no-middleware"},
	}
	dc := FromContext(ctx)
	// Should return noop — no panic
	err := dc.Save(map[string]int{"x": 1}, 1)
	if err != nil {
		t.Errorf("noop save should not error: %v", err)
	}
	_, ok := dc.Restore(&map[string]int{})
	if ok {
		t.Error("noop restore should return false")
	}
}

func TestMaxCheckpointSize(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	bigState := strings.Repeat("x", MaxCheckpointSize+1)
	_, err := store.Save("job-big", json.RawMessage(bigState), 0)
	if err == nil {
		t.Error("expected error for oversized checkpoint")
	}
}

func TestVersionIncrement(t *testing.T) {
	store := NewMemoryStore(time.Hour)
	store.Save("j1", json.RawMessage(`{"v":1}`), 1)
	cp, _ := store.Save("j1", json.RawMessage(`{"v":2}`), 2)
	if cp.Version != 2 {
		t.Errorf("expected version 2, got %d", cp.Version)
	}
}

func TestExpiredCheckpoint(t *testing.T) {
	store := NewMemoryStore(time.Millisecond)
	store.Save("j1", json.RawMessage(`{}`), 0)
	time.Sleep(10 * time.Millisecond)
	_, ok := store.Get("j1")
	if ok {
		t.Error("expected expired checkpoint to not be found")
	}
}
