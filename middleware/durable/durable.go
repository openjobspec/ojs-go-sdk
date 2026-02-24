// Package durable provides checkpoint-based durable execution middleware for OJS workers.
//
// Usage:
//
//	store := durable.NewMemoryStore(time.Hour)
//	worker.Use(durable.Middleware(store))
//
//	worker.Register("data.migrate", func(ctx ojs.JobContext) error {
//	    dc := durable.FromContext(ctx)
//	    var state MyState
//	    startStep, resumed := dc.Restore(&state)
//	    if !resumed { state = MyState{} }
//	    for step := startStep; step < total; step++ {
//	        process(step)
//	        dc.Save(state, step)
//	    }
//	    dc.Clear()
//	    return nil
//	})
package durable

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// MaxCheckpointSize is the maximum checkpoint payload (1 MB per spec).
const MaxCheckpointSize = 1 << 20

// Checkpoint represents saved intermediate state.
type Checkpoint struct {
	JobID     string          `json:"job_id"`
	Version   int             `json:"version"`
	State     json.RawMessage `json:"state"`
	StepIndex int             `json:"step_index"`
	CreatedAt time.Time       `json:"created_at"`
}

// Store persists checkpoints.
type Store interface {
	Save(jobID string, state json.RawMessage, stepIndex int) (*Checkpoint, error)
	Get(jobID string) (*Checkpoint, bool)
	Delete(jobID string) error
}

// MemoryStore is a thread-safe in-memory checkpoint store.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*Checkpoint
	ttl  time.Duration
}

// NewMemoryStore creates an in-memory store with the given TTL.
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &MemoryStore{data: make(map[string]*Checkpoint), ttl: ttl}
}

func (s *MemoryStore) Save(jobID string, state json.RawMessage, stepIndex int) (*Checkpoint, error) {
	if len(state) > MaxCheckpointSize {
		return nil, fmt.Errorf("checkpoint exceeds %d bytes", MaxCheckpointSize)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	version := 1
	if existing, ok := s.data[jobID]; ok {
		version = existing.Version + 1
	}
	cp := &Checkpoint{JobID: jobID, Version: version, State: state, StepIndex: stepIndex, CreatedAt: time.Now()}
	s.data[jobID] = cp
	return cp, nil
}

func (s *MemoryStore) Get(jobID string) (*Checkpoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.data[jobID]
	if !ok || time.Since(cp.CreatedAt) > s.ttl {
		return nil, false
	}
	return cp, true
}

func (s *MemoryStore) Delete(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, jobID)
	return nil
}

// DurableContext provides checkpoint operations within a job handler.
type DurableContext struct {
	store Store
	jobID string
}

// Save persists the current state as a checkpoint.
func (dc *DurableContext) Save(state interface{}, stepIndex int) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}
	_, err = dc.store.Save(dc.jobID, data, stepIndex)
	return err
}

// Restore retrieves the last checkpoint. Returns (stepIndex, true) if found.
func (dc *DurableContext) Restore(target interface{}) (int, bool) {
	cp, ok := dc.store.Get(dc.jobID)
	if !ok {
		return 0, false
	}
	if err := json.Unmarshal(cp.State, target); err != nil {
		return 0, false
	}
	return cp.StepIndex, true
}

// Clear removes the checkpoint after successful completion.
func (dc *DurableContext) Clear() error {
	return dc.store.Delete(dc.jobID)
}

// registry stores DurableContext references per job ID for retrieval via FromContext.
var registry sync.Map

// Middleware returns OJS worker middleware that injects a DurableContext.
func Middleware(store Store) ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		dc := &DurableContext{store: store, jobID: ctx.Job.ID}
		registry.Store(ctx.Job.ID, dc)
		defer registry.Delete(ctx.Job.ID)
		return next(ctx)
	}
}

// FromContext retrieves the DurableContext for the current job.
func FromContext(ctx ojs.JobContext) *DurableContext {
	if dc, ok := registry.Load(ctx.Job.ID); ok {
		return dc.(*DurableContext)
	}
	return &DurableContext{store: &noopStore{}}
}

type noopStore struct{}

func (s *noopStore) Save(string, json.RawMessage, int) (*Checkpoint, error) { return nil, nil }
func (s *noopStore) Get(string) (*Checkpoint, bool)                         { return nil, false }
func (s *noopStore) Delete(string) error                                    { return nil }
