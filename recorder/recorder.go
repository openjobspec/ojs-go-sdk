// Package recorder captures execution traces for OJS job handlers.
// Traces can be exported to the OJS Replay Studio for step-through
// debugging and shadow replay.
//
// This package is part of OJS Labs — forward-looking R&D that is not
// part of the core release train. APIs may change between minor versions.
package recorder

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SourceMap links a trace entry to its source code location.
type SourceMap struct {
	GitSHA   string `json:"git_sha"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
}

// TraceEntry is a single recorded function call.
type TraceEntry struct {
	FuncName   string     `json:"func_name"`
	Args       string     `json:"args"`
	Result     string     `json:"result"`
	DurationMs int64      `json:"duration_ms"`
	SourceMap  *SourceMap `json:"source_map,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
	Error      string     `json:"error,omitempty"`
}

// Recorder captures execution traces for a single job handler invocation.
type Recorder struct {
	mu      sync.Mutex
	entries []TraceEntry
}

// New creates a new Recorder.
func New() *Recorder {
	return &Recorder{}
}

// safeMarshal marshals v to JSON, falling back to fmt.Sprintf on error.
func safeMarshal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// RecordCall appends a trace entry for a function call.
func (r *Recorder) RecordCall(funcName string, args, result any, durationMs int64) {
	if funcName == "" {
		funcName = "<unknown>"
	}
	if durationMs < 0 {
		durationMs = 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, TraceEntry{
		FuncName:   funcName,
		Args:       safeMarshal(args),
		Result:     safeMarshal(result),
		DurationMs: durationMs,
		Timestamp:  time.Now().UTC(),
	})
}

// RecordError appends a trace entry for a failed function call.
func (r *Recorder) RecordError(funcName string, args any, err error, durationMs int64) {
	if funcName == "" {
		funcName = "<unknown>"
	}
	if durationMs < 0 {
		durationMs = 0
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, TraceEntry{
		FuncName:   funcName,
		Args:       safeMarshal(args),
		DurationMs: durationMs,
		Error:      errMsg,
		Timestamp:  time.Now().UTC(),
	})
}

// AttachSourceMap attaches source location metadata to the most recent
// trace entry. If gitSHA is empty, the operation is skipped.
func (r *Recorder) AttachSourceMap(gitSHA, filePath string, line int) {
	if gitSHA == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return
	}
	r.entries[len(r.entries)-1].SourceMap = &SourceMap{
		GitSHA:   gitSHA,
		FilePath: filePath,
		Line:     line,
	}
}

// Trace returns a copy of all recorded trace entries.
func (r *Recorder) Trace() []TraceEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TraceEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Len returns the number of recorded entries.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Reset clears all recorded entries.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = r.entries[:0]
}

// MarshalJSON exports the trace as JSON.
func (r *Recorder) MarshalJSON() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return json.Marshal(r.entries)
}
