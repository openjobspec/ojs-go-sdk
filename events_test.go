package ojs

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventConstants_CoreJobEvents(t *testing.T) {
	// Core job events are REQUIRED by the spec
	core := []string{
		EventJobEnqueued,
		EventJobStarted,
		EventJobCompleted,
		EventJobFailed,
		EventJobDiscarded,
	}
	for _, c := range core {
		if c == "" {
			t.Errorf("core event constant is empty")
		}
		if c[:4] != "job." {
			t.Errorf("core event %q should start with 'job.'", c)
		}
	}
}

func TestEventConstants_NoDuplicates(t *testing.T) {
	all := []string{
		EventJobEnqueued, EventJobStarted, EventJobCompleted, EventJobFailed, EventJobDiscarded,
		EventJobRetrying, EventJobCancelled, EventJobScheduled, EventJobExpired, EventJobProgress,
		EventWorkerStarted, EventWorkerStopped, EventWorkerQuiet, EventWorkerHeartbeat,
		EventWorkflowStarted, EventWorkflowStepCompleted, EventWorkflowCompleted, EventWorkflowFailed,
		EventCronTriggered, EventCronSkipped,
		EventQueuePaused, EventQueueResumed,
	}

	seen := make(map[string]bool, len(all))
	for _, c := range all {
		if seen[c] {
			t.Errorf("duplicate event constant: %q", c)
		}
		seen[c] = true
	}
}

func TestEvent_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	event := Event{
		Type:    EventJobCompleted,
		Source:  "ojs://test",
		Subject: "job-123",
		Time:    now,
		Data: map[string]any{
			"duration_ms": 1500,
			"queue":       "email",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != EventJobCompleted {
		t.Errorf("Type = %q, want %q", decoded.Type, EventJobCompleted)
	}
	if decoded.Source != "ojs://test" {
		t.Errorf("Source = %q, want %q", decoded.Source, "ojs://test")
	}
	if decoded.Subject != "job-123" {
		t.Errorf("Subject = %q, want %q", decoded.Subject, "job-123")
	}
}

func TestEvent_EmptyData(t *testing.T) {
	event := Event{
		Type:    EventWorkerHeartbeat,
		Source:  "ojs://worker-1",
		Subject: "worker-1",
		Time:    time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Data should be omitted when nil (omitempty)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, ok := raw["data"]; ok {
		t.Error("nil Data should be omitted from JSON (omitempty)")
	}
}
