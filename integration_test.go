package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestIntegrationEnqueueAndProcess tests the full lifecycle of enqueuing
// a job via the client and processing it via the worker.
func TestIntegrationEnqueueAndProcess(t *testing.T) {
	// Shared state between client and worker via a mock server.
	var (
		jobQueue     = make(chan Job, 10)
		completedIDs = make(chan string, 10)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.Header().Set("OJS-Version", ojsVersion)

		switch r.URL.Path {
		case "/ojs/v1/jobs":
			// Enqueue: parse request, create a job, put it on the queue.
			var req enqueueRequest
			json.NewDecoder(r.Body).Decode(&req)

			job := Job{
				ID:          "integ-job-1",
				Type:        req.Type,
				State:       JobStateAvailable,
				Queue:       "default",
				Attempt:     0,
				MaxAttempts: 3,
			}
			if req.Options != nil && req.Options.Queue != "" {
				job.Queue = req.Options.Queue
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"job": map[string]any{
					"id":          job.ID,
					"type":        job.Type,
					"state":       string(job.State),
					"args":        req.Args,
					"queue":       job.Queue,
					"attempt":     job.Attempt,
					"max_attempts": job.MaxAttempts,
				},
			})

			// Put on internal queue for the worker to fetch.
			job.RawArgs = req.Args
			job.Args = argsFromWire(req.Args)
			jobQueue <- job

		case "/ojs/v1/workers/fetch":
			// Worker fetch: return a job if available.
			select {
			case job := <-jobQueue:
				job.State = JobStateActive
				job.Attempt = 1
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id":          job.ID,
							"type":        job.Type,
							"state":       "active",
							"args":        argsToWire(job.Args),
							"queue":       job.Queue,
							"attempt":     1,
							"max_attempts": job.MaxAttempts,
						},
					},
				})
			default:
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}

		case "/ojs/v1/workers/ack":
			var req struct {
				JobID  string         `json:"job_id"`
				Result map[string]any `json:"result,omitempty"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			completedIDs <- req.JobID
			json.NewEncoder(w).Encode(map[string]any{
				"acknowledged": true,
				"job_id":       req.JobID,
				"state":        "completed",
			})

		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Step 1: Enqueue a job via the client.
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	job, err := client.Enqueue(context.Background(), "email.send",
		Args{"to": "integration@example.com"},
		WithQueue("email"),
	)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.ID != "integ-job-1" {
		t.Errorf("expected job ID integ-job-1, got %s", job.ID)
	}

	// Step 2: Start a worker that processes the job.
	var processedTo atomic.Value

	worker := NewWorker(server.URL,
		WithQueues("email", "default"),
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)

	worker.Register("email.send", func(ctx JobContext) error {
		to, _ := ctx.Job.Args["to"].(string)
		processedTo.Store(to)
		ctx.SetResult(map[string]any{"sent": true})
		return nil
	})

	workerCtx, cancelWorker := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelWorker()

	go worker.Start(workerCtx)

	// Step 3: Wait for the job to be completed.
	select {
	case id := <-completedIDs:
		if id != "integ-job-1" {
			t.Errorf("expected completed job integ-job-1, got %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job completion")
	}

	// Verify the handler received the correct args.
	if to := processedTo.Load(); to != "integration@example.com" {
		t.Errorf("expected to=integration@example.com, got %v", to)
	}
}

// TestIntegrationWorkflow tests creating a workflow via the client.
func TestIntegrationWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)

		switch r.URL.Path {
		case "/ojs/v1/workflows":
			var req workflowRequest
			json.NewDecoder(r.Body).Decode(&req)

			if len(req.Steps) != 2 {
				t.Errorf("expected 2 steps, got %d", len(req.Steps))
			}
			if req.Steps[0].Type != "data.fetch" {
				t.Errorf("expected first step type data.fetch, got %s", req.Steps[0].Type)
			}
			if len(req.Steps[0].DependsOn) != 0 {
				t.Errorf("expected first step to have no dependencies")
			}
			if len(req.Steps[1].DependsOn) != 1 || req.Steps[1].DependsOn[0] != "step-0" {
				t.Errorf("expected second step to depend on step-0, got %v", req.Steps[1].DependsOn)
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"workflow": map[string]any{
					"id":    "wf-123",
					"name":  "",
					"state": "running",
					"steps": []map[string]any{
						{"id": "step-0", "type": "data.fetch", "state": "available", "depends_on": []any{}},
						{"id": "step-1", "type": "data.transform", "state": "pending", "depends_on": []any{"step-0"}},
					},
				},
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	wf, err := client.CreateWorkflow(context.Background(), Chain(
		Step{Type: "data.fetch", Args: Args{"url": "https://example.com/data"}},
		Step{Type: "data.transform", Args: Args{"format": "csv"}},
	))
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.ID != "wf-123" {
		t.Errorf("expected workflow ID wf-123, got %s", wf.ID)
	}
	if wf.State != WorkflowStateRunning {
		t.Errorf("expected state running, got %s", wf.State)
	}
	if len(wf.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(wf.Steps))
	}
}

// TestIntegrationMiddlewareOrdering verifies middleware executes in the correct order.
func TestIntegrationMiddlewareOrdering(t *testing.T) {
	var order []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if len(order) == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{"id": "j1", "type": "test.job", "state": "active",
							"args": []any{map[string]any{}}, "queue": "default",
							"attempt": 1, "max_attempts": 3},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/ack":
			json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})
		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)

	worker.Use(func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "mw1-before")
		err := next(ctx)
		order = append(order, "mw1-after")
		return err
	})

	worker.Use(func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "mw2-before")
		err := next(ctx)
		order = append(order, "mw2-after")
		return err
	})

	worker.Register("test.job", func(ctx JobContext) error {
		order = append(order, "handler")
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) < len(expected) {
		t.Fatalf("expected at least %d entries, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %s, want %s", i, order[i], v)
		}
	}
}
