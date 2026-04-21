package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateGroupWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Type != workflowTypeGroup {
			t.Errorf("expected type=group, got %q", req.Type)
		}
		if len(req.Steps) != 0 {
			t.Errorf("expected no \"steps\" on a group request, got %v", req.Steps)
		}
		if len(req.Jobs) != 3 {
			t.Errorf("expected 3 jobs, got %d", len(req.Jobs))
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":    "wf-group-1",
				"state": "running",
				"steps": []map[string]any{
					{"id": "job-0", "type": "export.csv", "state": "available", "depends_on": []any{}},
					{"id": "job-1", "type": "export.pdf", "state": "available", "depends_on": []any{}},
					{"id": "job-2", "type": "export.xlsx", "state": "available", "depends_on": []any{}},
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.CreateWorkflow(context.Background(), Group(
		Step{Type: "export.csv", Args: Args{"id": "rpt_1"}},
		Step{Type: "export.pdf", Args: Args{"id": "rpt_1"}},
		Step{Type: "export.xlsx", Args: Args{"id": "rpt_1"}},
	))
	if err != nil {
		t.Fatalf("CreateWorkflow(Group) error = %v", err)
	}
	if wf.ID != "wf-group-1" {
		t.Errorf("expected workflow ID wf-group-1, got %s", wf.ID)
	}
	if len(wf.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(wf.Steps))
	}
}

// assertCallback fails unless cb is present and has the expected type.
func assertCallback(t *testing.T, name string, cb *workflowStepWire, wantType string) {
	t.Helper()
	if cb == nil || cb.Type != wantType {
		t.Errorf("callbacks.%s = %+v, want type=%s", name, cb, wantType)
		return
	}
}

// assertBatchWorkflowRequest checks a decoded batch workflowRequest against
// the discriminated wire shape: jobs (not steps), and the expected
// on_complete/on_failure callbacks with on_success absent.
func assertBatchWorkflowRequest(t *testing.T, req workflowRequest) {
	t.Helper()
	if req.Type != workflowTypeBatch {
		t.Errorf("expected type=batch, got %q", req.Type)
	}
	if len(req.Steps) != 0 {
		t.Errorf("expected no \"steps\" on a batch request, got %v", req.Steps)
	}
	if len(req.Jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(req.Jobs))
	}

	if req.Callbacks == nil {
		t.Errorf("expected callbacks to be set")
		return
	}
	assertCallback(t, "on_complete", req.Callbacks.OnComplete, "batch.report")
	assertCallback(t, "on_failure", req.Callbacks.OnFailure, "batch.alert")
	if req.Callbacks.OnSuccess != nil {
		t.Errorf("expected callbacks.on_success to be unset, got %+v", req.Callbacks.OnSuccess)
	}
}

func TestCreateBatchWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)
		assertBatchWorkflowRequest(t, req)

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":    "wf-batch-1",
				"state": "running",
				"steps": []map[string]any{
					{"id": "job-0", "type": "email.send", "state": "available", "depends_on": []any{}},
					{"id": "job-1", "type": "email.send", "state": "available", "depends_on": []any{}},
					{"id": "on-complete", "type": "batch.report", "state": "pending", "depends_on": []any{"job-0", "job-1"}},
					{"id": "on-failure", "type": "batch.alert", "state": "pending", "depends_on": []any{"job-0", "job-1"}},
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.CreateWorkflow(context.Background(), Batch(
		BatchCallbacks{
			OnComplete: &Step{Type: "batch.report", Args: Args{}},
			OnFailure:  &Step{Type: "batch.alert", Args: Args{}},
		},
		Step{Type: "email.send", Args: Args{"to": "user1@example.com"}},
		Step{Type: "email.send", Args: Args{"to": "user2@example.com"}},
	))
	if err != nil {
		t.Fatalf("CreateWorkflow(Batch) error = %v", err)
	}
	if wf.ID != "wf-batch-1" {
		t.Errorf("expected workflow ID wf-batch-1, got %s", wf.ID)
	}
	if len(wf.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(wf.Steps))
	}
}

func TestGetWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/workflows/wf-123" {
			t.Errorf("expected /ojs/v1/workflows/wf-123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":    "wf-123",
				"state": "completed",
				"steps": []map[string]any{
					{"id": "step-0", "type": "data.fetch", "state": "completed"},
					{"id": "step-1", "type": "data.transform", "state": "completed"},
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.GetWorkflow(context.Background(), "wf-123")
	if err != nil {
		t.Fatalf("GetWorkflow() error = %v", err)
	}
	if wf.State != WorkflowStateCompleted {
		t.Errorf("expected state completed, got %s", wf.State)
	}
	if len(wf.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(wf.Steps))
	}
}

func TestCancelWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/workflows/wf-123" {
			t.Errorf("expected /ojs/v1/workflows/wf-123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":                      "wf-123",
				"state":                   "cancelled",
				"steps_cancelled":         2,
				"steps_already_completed": 1,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.CancelWorkflow(context.Background(), "wf-123")
	if err != nil {
		t.Fatalf("CancelWorkflow() error = %v", err)
	}
	if wf.State != WorkflowStateCancelled {
		t.Errorf("expected state cancelled, got %s", wf.State)
	}
	if wf.StepsCancelled != 2 {
		t.Errorf("expected 2 steps cancelled, got %d", wf.StepsCancelled)
	}
	if wf.StepsAlreadyComplete != 1 {
		t.Errorf("expected 1 step already completed, got %d", wf.StepsAlreadyComplete)
	}
}

func TestGroupDefinition(t *testing.T) {
	def := Group(
		Step{Type: "a", Args: Args{"k": "v1"}},
		Step{Type: "b", Args: Args{"k": "v2"}},
	)
	if def.Type != "group" {
		t.Errorf("expected type group, got %s", def.Type)
	}
	if len(def.Jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(def.Jobs))
	}
}

func TestBatchDefinition(t *testing.T) {
	def := Batch(
		BatchCallbacks{
			OnComplete: &Step{Type: "done", Args: Args{}},
			OnSuccess:  &Step{Type: "yay", Args: Args{}},
		},
		Step{Type: "work", Args: Args{}},
	)
	if def.Type != "batch" {
		t.Errorf("expected type batch, got %s", def.Type)
	}
	if def.Callbacks == nil {
		t.Fatal("expected callbacks to be set")
	}
	if def.Callbacks.OnComplete.Type != "done" {
		t.Errorf("expected OnComplete type=done, got %s", def.Callbacks.OnComplete.Type)
	}
	if def.Callbacks.OnSuccess.Type != "yay" {
		t.Errorf("expected OnSuccess type=yay, got %s", def.Callbacks.OnSuccess.Type)
	}
	if def.Callbacks.OnFailure != nil {
		t.Error("expected OnFailure to be nil")
	}
}

func TestCreateWorkflowWithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)

		if len(req.Steps) > 0 && req.Steps[0].Options == nil {
			t.Error("expected step-level options with materialized defaults")
		}
		if len(req.Steps) > 0 && req.Steps[0].Options != nil && req.Steps[0].Options.Queue != "priority" {
			t.Errorf("expected queue=priority, got %s", req.Steps[0].Options.Queue)
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":    "wf-opt-1",
				"state": "running",
				"steps": []map[string]any{},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateWorkflow(context.Background(),
		Chain(Step{Type: "task", Args: Args{}}),
		WithQueue("priority"),
	)
	if err != nil {
		t.Fatalf("CreateWorkflow with options error = %v", err)
	}
}

// assertChainWorkflowRequest checks a decoded chain workflowRequest against
// the discriminated wire shape: steps (not jobs/callbacks), with the expected
// count. Chain ordering is represented by array position.
func assertChainWorkflowRequest(t *testing.T, req workflowRequest, wantSteps int) {
	t.Helper()
	if req.Type != workflowTypeChain {
		t.Errorf("expected type=chain, got %q", req.Type)
	}
	if len(req.Jobs) != 0 || req.Callbacks != nil {
		t.Errorf("expected no jobs/callbacks on a chain request, got jobs=%v callbacks=%+v", req.Jobs, req.Callbacks)
	}
	if len(req.Steps) != wantSteps {
		t.Errorf("expected %d steps, got %d", wantSteps, len(req.Steps))
	}
}

func TestCreateChainWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)
		assertChainWorkflowRequest(t, req, 3)

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":    "wf-chain-1",
				"state": "running",
				"steps": []map[string]any{
					{"id": "step-0", "type": "validate", "state": "available", "depends_on": []any{}},
					{"id": "step-1", "type": "process", "state": "pending", "depends_on": []any{"step-0"}},
					{"id": "step-2", "type": "notify", "state": "pending", "depends_on": []any{"step-1"}},
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.CreateWorkflow(context.Background(), Chain(
		Step{Type: "validate", Args: Args{"input": "data"}},
		Step{Type: "process", Args: Args{"mode": "fast"}},
		Step{Type: "notify", Args: Args{"channel": "email"}},
	))
	if err != nil {
		t.Fatalf("CreateWorkflow(Chain) error = %v", err)
	}
	if wf.ID != "wf-chain-1" {
		t.Errorf("expected workflow ID wf-chain-1, got %s", wf.ID)
	}
	if len(wf.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(wf.Steps))
	}
	// Verify step dependencies in response
	if len(wf.Steps) >= 3 {
		if len(wf.Steps[0].DependsOn) != 0 {
			t.Errorf("response step 0 should have no deps")
		}
		if len(wf.Steps[2].DependsOn) != 1 || wf.Steps[2].DependsOn[0] != "step-1" {
			t.Errorf("response step 2 should depend on step-1, got %v", wf.Steps[2].DependsOn)
		}
	}
}

func TestCreateWorkflowResponseDecodesBackendFields(t *testing.T) {
	createdAt := time.Date(2026, 6, 15, 11, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":              "wf-batch-42",
				"name":            "bulk-email",
				"type":            "batch",
				"state":           "running",
				"created_at":      createdAt.Format(time.RFC3339),
				"jobs_total":      10,
				"jobs_completed":  3,
				"steps_total":     12,
				"steps_completed": 3,
				"callbacks": map[string]any{
					"on_complete": map[string]any{
						"type": "batch.report",
						"args": json.RawMessage(`[]`),
						"options": map[string]any{
							"queue":      "callbacks",
							"timeout_ms": 15000,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL)
	wf, err := client.CreateWorkflow(context.Background(),
		Batch(BatchCallbacks{OnComplete: &Step{Type: "batch.report", Args: Args{}}},
			Step{Type: "email.send", Args: Args{}}))
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.Type != "batch" {
		t.Errorf("Type = %q, want batch", wf.Type)
	}
	if wf.JobsTotal == nil || *wf.JobsTotal != 10 {
		t.Errorf("JobsTotal = %v, want 10", wf.JobsTotal)
	}
	if wf.JobsCompleted == nil || *wf.JobsCompleted != 3 {
		t.Errorf("JobsCompleted = %v, want 3", wf.JobsCompleted)
	}
	if wf.Callbacks == nil || wf.Callbacks.OnComplete == nil || wf.Callbacks.OnComplete.Type != "batch.report" {
		t.Fatalf("Callbacks = %+v, want on_complete with type batch.report", wf.Callbacks)
	}
	var callbackOptions map[string]any
	if err := json.Unmarshal(wf.Callbacks.OnComplete.Options, &callbackOptions); err != nil {
		t.Fatalf("decode callback options: %v", err)
	}
	if callbackOptions["queue"] != "callbacks" || callbackOptions["timeout_ms"] != float64(15000) {
		t.Errorf("callback options = %v, want queue and timeout", callbackOptions)
	}
}

func TestGetWorkflowResponseDecodesBackendFields(t *testing.T) {
	createdAt := time.Date(2026, 6, 15, 11, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":              "wf-chain-99",
				"type":            "chain",
				"state":           "completed",
				"created_at":      createdAt.Format(time.RFC3339),
				"completed_at":    completedAt.Format(time.RFC3339),
				"steps_total":     3,
				"steps_completed": 3,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL)
	wf, err := client.GetWorkflow(context.Background(), "wf-chain-99")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.State != WorkflowStateCompleted {
		t.Errorf("State = %q, want completed", wf.State)
	}
	if wf.CompletedAt == nil || !wf.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", wf.CompletedAt, completedAt)
	}
	if wf.StepsTotal == nil || *wf.StepsTotal != 3 {
		t.Errorf("StepsTotal = %v, want 3", wf.StepsTotal)
	}
	if wf.StepsCompleted == nil || *wf.StepsCompleted != 3 {
		t.Errorf("StepsCompleted = %v, want 3", wf.StepsCompleted)
	}
}

func TestCancelWorkflowResponseRetainsExistingFields(t *testing.T) {
	cancelledAt := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{
				"id":                      "wf-cancel-1",
				"type":                    "group",
				"state":                   "cancelled",
				"cancelled_at":            cancelledAt.Format(time.RFC3339),
				"steps_cancelled":         4,
				"steps_already_completed": 2,
				"jobs_total":              6,
				"jobs_completed":          2,
			},
		})
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL)
	wf, err := client.CancelWorkflow(context.Background(), "wf-cancel-1")
	if err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}
	if wf.Type != "group" {
		t.Errorf("Type = %q, want group", wf.Type)
	}
	if wf.StepsCancelled != 4 {
		t.Errorf("StepsCancelled = %d, want 4", wf.StepsCancelled)
	}
	if wf.StepsAlreadyComplete != 2 {
		t.Errorf("StepsAlreadyComplete = %d, want 2", wf.StepsAlreadyComplete)
	}
	if wf.CancelledAt == nil || !wf.CancelledAt.Equal(cancelledAt) {
		t.Errorf("CancelledAt = %v, want %v", wf.CancelledAt, cancelledAt)
	}
}
