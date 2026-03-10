package ojs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DurableContext provides deterministic execution support within a job handler.
// Non-deterministic operations (time, randomness, external calls) are recorded
// on first execution and replayed from the checkpoint on retry, ensuring
// idempotent re-execution after crashes.
type DurableContext struct {
	mu        sync.Mutex
	transport *transport
	jobID     string
	attempt   int
	entries   []sideEffectEntry
	cursor    int
	replaying bool
}

type sideEffectEntry struct {
	Seq    int             `json:"seq"`
	Type   string          `json:"type"`
	Key    string          `json:"key,omitempty"`
	Result json.RawMessage `json:"result"`
}

type checkpointState struct {
	StepIndex int               `json:"step_index"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	State     json.RawMessage   `json:"state"`
}

// newDurableContext creates a DurableContext, loading any existing checkpoint.
func newDurableContext(transport *transport, jobID string, attempt int) *DurableContext {
	dc := &DurableContext{
		transport: transport,
		jobID:     jobID,
		attempt:   attempt,
	}

	// Try to load existing replay log from server
	var resp struct {
		HasCheckpoint bool `json:"has_checkpoint"`
		Checkpoint    *struct {
			Metadata map[string]string `json:"metadata,omitempty"`
		} `json:"checkpoint,omitempty"`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := transport.get(ctx, fmt.Sprintf("%s/checkpoints/%s/resume", basePath, jobID), &resp)
	if err == nil && resp.HasCheckpoint && resp.Checkpoint != nil {
		if logData, ok := resp.Checkpoint.Metadata["_replay_log"]; ok {
			var entries []sideEffectEntry
			if json.Unmarshal([]byte(logData), &entries) == nil && len(entries) > 0 {
				dc.entries = entries
				dc.replaying = true
			}
		}
	}

	return dc
}

// Now returns the current time deterministically.
// On first execution the real time is recorded; on replay the recorded time is returned.
func (dc *DurableContext) Now() time.Time {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.replaying && dc.cursor < len(dc.entries) {
		entry := dc.entries[dc.cursor]
		if entry.Type == "time" {
			var t time.Time
			if json.Unmarshal(entry.Result, &t) == nil {
				dc.cursor++
				if dc.cursor >= len(dc.entries) {
					dc.replaying = false
				}
				return t
			}
		}
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

	if dc.replaying && dc.cursor < len(dc.entries) {
		entry := dc.entries[dc.cursor]
		if entry.Type == "random" {
			var s string
			if json.Unmarshal(entry.Result, &s) == nil {
				dc.cursor++
				if dc.cursor >= len(dc.entries) {
					dc.replaying = false
				}
				return s
			}
		}
	}

	buf := make([]byte, numBytes)
	rand.Read(buf)
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
		if entry.Type == "call" {
			// Strict key matching during replay: if both keys are set, they must match.
			// A mismatch means the code changed between the checkpoint save and replay,
			// which would silently return the wrong data.
			if key != "" && entry.Key != "" && entry.Key != key {
				return nil, fmt.Errorf("ojs: durable replay key mismatch at position %d: checkpoint has %q but code called %q — handler logic may have changed since checkpoint was saved", dc.cursor, entry.Key, key)
			}
			dc.cursor++
			if dc.cursor >= len(dc.entries) {
				dc.replaying = false
			}
			return entry.Result, nil
		}
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

	logData, err := json.Marshal(entriesCopy)
	if err != nil {
		return fmt.Errorf("ojs: marshal replay log: %w", err)
	}

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("ojs: marshal checkpoint state: %w", err)
	}

	req := struct {
		State     json.RawMessage   `json:"state"`
		StepIndex int               `json:"step_index"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}{
		State:     stateData,
		StepIndex: stepIndex,
		Metadata: map[string]string{
			"_replay_log": string(logData),
			"attempt":     fmt.Sprintf("%d", dc.attempt),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return dc.transport.do(ctx, "PUT",
		fmt.Sprintf("%s/checkpoints/%s", basePath, dc.jobID), req, nil)
}

// Complete clears the checkpoint after successful job completion.
func (dc *DurableContext) Complete() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return dc.transport.delete(ctx,
		fmt.Sprintf("%s/checkpoints/%s", basePath, dc.jobID), nil)
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
		dc := newDurableContext(w.transport, ctx.Job.ID, ctx.Attempt)
		return handler(ctx, dc)
	})
}
