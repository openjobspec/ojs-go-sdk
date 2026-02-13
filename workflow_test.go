package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateGroupWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)

		if len(req.Steps) != 3 {
			t.Errorf("expected 3 steps, got %d", len(req.Steps))
		}
		// Group steps should have no dependencies.
		for i, step := range req.Steps {
			if len(step.DependsOn) != 0 {
				t.Errorf("step %d should have no dependencies, got %v", i, step.DependsOn)
			}
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

func TestCreateBatchWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req workflowRequest
		json.NewDecoder(r.Body).Decode(&req)

		// 2 jobs + 2 callback steps (on-complete, on-failure).
		if len(req.Steps) != 4 {
			t.Errorf("expected 4 steps, got %d", len(req.Steps))
		}

		// Verify callback dependencies.
		for _, step := range req.Steps {
			if step.ID == "on-complete" || step.ID == "on-failure" {
				if len(step.DependsOn) != 2 {
					t.Errorf("callback %s should depend on 2 jobs, got %v", step.ID, step.DependsOn)
				}
			}
		}

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

		if req.Options == nil {
			t.Error("expected workflow-level options")
		}
		if req.Options != nil && req.Options.Queue != "priority" {
			t.Errorf("expected queue=priority, got %s", req.Options.Queue)
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
