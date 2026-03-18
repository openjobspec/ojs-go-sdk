package recorder

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.Len() != 0 {
		t.Errorf("new recorder should have 0 entries, got %d", r.Len())
	}
}

func TestRecordCall(t *testing.T) {
	r := New()
	r.RecordCall("doWork", []string{"a", "b"}, "ok", 42)

	entries := r.Trace()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.FuncName != "doWork" {
		t.Errorf("FuncName = %q", e.FuncName)
	}
	if e.DurationMs != 42 {
		t.Errorf("DurationMs = %d", e.DurationMs)
	}
	if e.Error != "" {
		t.Errorf("Error = %q", e.Error)
	}
}

func TestRecordError(t *testing.T) {
	r := New()
	r.RecordError("doWork", nil, errors.New("boom"), 10)

	entries := r.Trace()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Error != "boom" {
		t.Errorf("Error = %q, want %q", entries[0].Error, "boom")
	}
}

func TestAttachSourceMap(t *testing.T) {
	r := New()
	r.RecordCall("handler", nil, nil, 5)
	r.AttachSourceMap("abc123", "main.go", 42)

	entries := r.Trace()
	sm := entries[0].SourceMap
	if sm == nil {
		t.Fatal("SourceMap is nil")
	}
	if sm.GitSHA != "abc123" {
		t.Errorf("GitSHA = %q", sm.GitSHA)
	}
	if sm.FilePath != "main.go" {
		t.Errorf("FilePath = %q", sm.FilePath)
	}
	if sm.Line != 42 {
		t.Errorf("Line = %d", sm.Line)
	}
}

func TestAttachSourceMap_NoEntries(t *testing.T) {
	r := New()
	// Should not panic.
	r.AttachSourceMap("abc", "x.go", 1)
	if r.Len() != 0 {
		t.Error("unexpected entries")
	}
}

func TestTrace_ReturnsCopy(t *testing.T) {
	r := New()
	r.RecordCall("a", nil, nil, 1)
	trace := r.Trace()
	trace[0].FuncName = "mutated"
	if r.Trace()[0].FuncName == "mutated" {
		t.Error("Trace() should return a copy")
	}
}

func TestReset(t *testing.T) {
	r := New()
	r.RecordCall("a", nil, nil, 1)
	r.RecordCall("b", nil, nil, 2)
	r.Reset()
	if r.Len() != 0 {
		t.Errorf("after Reset, Len() = %d", r.Len())
	}
}

func TestMarshalJSON(t *testing.T) {
	r := New()
	r.RecordCall("fn", "arg", "res", 10)
	data, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var entries []TraceEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry in JSON, got %d", len(entries))
	}
}

func TestMultipleRecordCalls(t *testing.T) {
	r := New()
	for i := 0; i < 100; i++ {
		r.RecordCall("fn", i, i*2, int64(i))
	}
	if r.Len() != 100 {
		t.Errorf("expected 100 entries, got %d", r.Len())
	}
	entries := r.Trace()
	for i, e := range entries {
		if e.DurationMs != int64(i) {
			t.Errorf("entry %d: DurationMs = %d", i, e.DurationMs)
		}
	}
}

func TestRecordCall_EmptyFuncName(t *testing.T) {
	r := New()
	r.RecordCall("", []string{"a"}, "ok", 10)
	entries := r.Trace()
	if entries[0].FuncName != "<unknown>" {
		t.Errorf("empty funcName should become <unknown>, got %q", entries[0].FuncName)
	}
}

func TestRecordCall_NegativeDuration(t *testing.T) {
	r := New()
	r.RecordCall("fn", nil, nil, -5)
	entries := r.Trace()
	if entries[0].DurationMs != 0 {
		t.Errorf("negative durationMs should be clamped to 0, got %d", entries[0].DurationMs)
	}
}

func TestRecordCall_MarshalFallback(t *testing.T) {
	r := New()
	// A channel cannot be marshalled to JSON.
	ch := make(chan int)
	r.RecordCall("fn", ch, nil, 1)
	entries := r.Trace()
	if entries[0].Args == "" || entries[0].Args == "null" {
		t.Errorf("marshal fallback should produce non-empty args, got %q", entries[0].Args)
	}
}

func TestRecordError_EmptyFuncName(t *testing.T) {
	r := New()
	r.RecordError("", nil, errors.New("fail"), 10)
	entries := r.Trace()
	if entries[0].FuncName != "<unknown>" {
		t.Errorf("empty funcName should become <unknown>, got %q", entries[0].FuncName)
	}
}

func TestRecordError_NegativeDuration(t *testing.T) {
	r := New()
	r.RecordError("fn", nil, errors.New("fail"), -1)
	entries := r.Trace()
	if entries[0].DurationMs != 0 {
		t.Errorf("negative durationMs should be clamped to 0, got %d", entries[0].DurationMs)
	}
}

func TestAttachSourceMap_EmptyGitSHA(t *testing.T) {
	r := New()
	r.RecordCall("handler", nil, nil, 5)
	r.AttachSourceMap("", "main.go", 42)
	entries := r.Trace()
	if entries[0].SourceMap != nil {
		t.Error("empty gitSHA should skip attaching source map")
	}
}
