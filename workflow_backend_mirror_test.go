package ojs

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

// This file contains a local test mirror of the shared backend
// WorkflowRequest and EnqueueOptions types from ojs-go-backend-common.
// The SDK must not import the backend module, so we mirror the wire types
// here and validate that the SDK's serialized workflow request can decode
// cleanly into the backend's expected shape.

// backendEnqueueOptions mirrors core.EnqueueOptions from
// ojs-go-backend-common/core/job.go. It validates that every field the SDK
// emits is recognized by the backend.
type backendEnqueueOptions struct {
	Queue               string           `json:"queue,omitempty"`
	Priority            *int             `json:"priority,omitempty"`
	TimeoutMs           *int             `json:"timeout_ms,omitempty"`
	DelayUntil          string           `json:"delay_until,omitempty"`
	ScheduledAt         string           `json:"scheduled_at,omitempty"`
	ExpiresAt           string           `json:"expires_at,omitempty"`
	Retry               *json.RawMessage `json:"retry,omitempty"`
	RetryPolicy         *json.RawMessage `json:"retry_policy,omitempty"`
	Unique              *json.RawMessage `json:"unique,omitempty"`
	Tags                []string         `json:"tags,omitempty"`
	VisibilityTimeoutMs *int             `json:"visibility_timeout_ms,omitempty"`
	VisibilityTimeout   string           `json:"visibility_timeout,omitempty"`
	Metadata            json.RawMessage  `json:"metadata,omitempty"`
	RateLimit           *json.RawMessage `json:"rate_limit,omitempty"`
}

// backendWorkflowJobRequest mirrors core.WorkflowJobRequest.
type backendWorkflowJobRequest struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Args    json.RawMessage        `json:"args"`
	Options *backendEnqueueOptions `json:"options,omitempty"`
}

// backendWorkflowCallback mirrors core.WorkflowCallback.
type backendWorkflowCallback struct {
	Type    string                 `json:"type"`
	Args    json.RawMessage        `json:"args,omitempty"`
	Options *backendEnqueueOptions `json:"options,omitempty"`
}

// backendWorkflowCallbacks mirrors core.WorkflowCallbacks.
type backendWorkflowCallbacks struct {
	OnSuccess  *backendWorkflowCallback `json:"on_success,omitempty"`
	OnFailure  *backendWorkflowCallback `json:"on_failure,omitempty"`
	OnComplete *backendWorkflowCallback `json:"on_complete,omitempty"`
}

// backendWorkflowRequest mirrors core.WorkflowRequest.
type backendWorkflowRequest struct {
	Type      string                      `json:"type"`
	Name      string                      `json:"name,omitempty"`
	Steps     []backendWorkflowJobRequest `json:"steps,omitempty"`
	Jobs      []backendWorkflowJobRequest `json:"jobs,omitempty"`
	Callbacks *backendWorkflowCallbacks   `json:"callbacks,omitempty"`
}

// TestGoldenWorkflowWireMatchesBackendShape serializes a representative
// workflow request from the SDK and decodes it into the backend mirror types
// to prove the wire shape is compatible. The strict decoder is intentional:
// a normal encoding/json decode would silently discard unsupported fields and
// make this compatibility test a false positive.
func TestGoldenWorkflowWireMatchesBackendShape(t *testing.T) {
	def := Batch(
		BatchCallbacks{
			OnComplete: &Step{Type: "batch.report", Args: Args{"fmt": "pdf"}},
		},
		Step{
			Type: "email.send",
			Args: Args{"to": "user@example.com"},
			Options: []EnqueueOption{
				WithQueue("email"),
				WithPriority(5),
				WithMeta(map[string]any{"tenant": "acme", "region": "eu"}),
			},
		},
	)
	def.Options = []EnqueueOption{
		WithQueue("default-jobs"),
		WithPriority(3),
		WithMeta(map[string]any{"tenant": "acme", "env": "prod"}),
	}

	cfg := resolveWorkflowDefaults(def, nil)
	req := buildWorkflowRequest(&def, cfg)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	backend := decodeBackendWorkflowRequest(t, raw)
	assertBackendWorkflowBatch(t, backend)
	assertNoUnsupportedWorkflowRootFields(t, raw)
}

func decodeBackendWorkflowRequest(t *testing.T, raw []byte) backendWorkflowRequest {
	t.Helper()
	var backend backendWorkflowRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&backend); err != nil {
		t.Fatalf("strict decode into backend mirror: %v\nJSON: %s", err, raw)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("workflow request has trailing JSON: %v", err)
	}
	return backend
}

func assertBackendWorkflowBatch(t *testing.T, backend backendWorkflowRequest) {
	t.Helper()
	if backend.Type != "batch" {
		t.Errorf("type = %q, want batch", backend.Type)
	}
	if len(backend.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(backend.Jobs))
	}
	assertBackendWorkflowJob(t, backend.Jobs[0])
	assertBackendWorkflowCallback(t, backend.Callbacks)
}

func assertBackendWorkflowJob(t *testing.T, job backendWorkflowJobRequest) {
	t.Helper()
	if job.Type != "email.send" {
		t.Errorf("job type = %q, want email.send", job.Type)
	}
	if job.Options == nil {
		t.Fatal("job options = nil, want materialized options")
	}
	if job.Options.Queue != "email" {
		t.Errorf("job options.queue = %q, want email (step override)", job.Options.Queue)
	}
	if job.Options.Priority == nil || *job.Options.Priority != 5 {
		t.Errorf("job options.priority = %v, want 5 (step override)", job.Options.Priority)
	}
	assertBackendMetadata(t, job.Options.Metadata)
}

func assertBackendMetadata(t *testing.T, raw json.RawMessage) {
	t.Helper()
	if raw == nil {
		t.Fatal("job options.metadata = nil, want merged metadata")
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode job metadata: %v", err)
	}
	if meta["tenant"] != "acme" || meta["region"] != "eu" || meta["env"] != "prod" {
		t.Errorf("job metadata = %v, want tenant=acme, region=eu (step), env=prod (default)", meta)
	}
}

func assertBackendWorkflowCallback(t *testing.T, callbacks *backendWorkflowCallbacks) {
	t.Helper()
	if callbacks == nil || callbacks.OnComplete == nil {
		t.Fatal("callbacks.on_complete = nil")
	}
	cb := callbacks.OnComplete
	if cb.Type != "batch.report" {
		t.Errorf("callback type = %q, want batch.report", cb.Type)
	}
	if cb.Options == nil || cb.Options.Queue != "default-jobs" {
		t.Errorf("callback options.queue = %v, want default-jobs (inherited)", cb.Options)
	}
}

func assertNoUnsupportedWorkflowRootFields(t *testing.T, raw []byte) {
	t.Helper()
	var rootMap map[string]any
	if err := json.Unmarshal(raw, &rootMap); err != nil {
		t.Fatalf("decode workflow root: %v", err)
	}
	if _, present := rootMap["options"]; present {
		t.Error("workflow root must not carry options")
	}
	if _, present := rootMap["meta"]; present {
		t.Error("workflow root must not carry meta")
	}
}
