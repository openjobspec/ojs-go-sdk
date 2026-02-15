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

func TestNewWorker(t *testing.T) {
	w := NewWorker("http://localhost:8080")
	if w == nil {
		t.Fatal("NewWorker() returned nil")
	}
	if w.State() != WorkerStateRunning {
		t.Errorf("expected initial state running, got %s", w.State())
	}
	if w.config.concurrency != 10 {
		t.Errorf("expected default concurrency 10, got %d", w.config.concurrency)
	}
}

func TestNewWorkerWithOptions(t *testing.T) {
	w := NewWorker("http://localhost:8080",
		WithQueues("email", "default"),
		WithConcurrency(20),
		WithGracePeriod(30*time.Second),
		WithHeartbeatInterval(10*time.Second),
		WithLabels("canary", "v2"),
		WithPollInterval(2*time.Second),
	)
	if len(w.config.queues) != 2 || w.config.queues[0] != "email" {
		t.Errorf("expected queues [email, default], got %v", w.config.queues)
	}
	if w.config.concurrency != 20 {
		t.Errorf("expected concurrency 20, got %d", w.config.concurrency)
	}
	if w.config.gracePeriod != 30*time.Second {
		t.Errorf("expected grace period 30s, got %v", w.config.gracePeriod)
	}
	if w.config.heartbeatInterval != 10*time.Second {
		t.Errorf("expected heartbeat interval 10s, got %v", w.config.heartbeatInterval)
	}
	if len(w.config.labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(w.config.labels))
	}
}

func TestRegister(t *testing.T) {
	w := NewWorker("http://localhost:8080")
	w.Register("test.job", func(ctx JobContext) error {
		return nil
	})

	w.handlersMu.RLock()
	_, exists := w.handlers["test.job"]
	w.handlersMu.RUnlock()

	if !exists {
		t.Error("expected handler to be registered")
	}
}

func TestStartWithoutHandlers(t *testing.T) {
	w := NewWorker("http://localhost:8080")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := w.Start(ctx)
	if err == nil {
		t.Fatal("expected error when starting without handlers")
	}
}

func TestWorkerProcessesJobs(t *testing.T) {
	var jobsProcessed atomic.Int32
	fetchCount := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)

		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			count := fetchCount.Add(1)
			if count == 1 {
				// First fetch: return a job.
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id":           "job-1",
							"type":         "test.job",
							"state":        "active",
							"args":         []any{map[string]any{"value": "hello"}},
							"queue":        "default",
							"attempt":      1,
							"max_attempts": 3,
						},
					},
				})
			} else {
				// Subsequent fetches: no jobs.
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []any{},
				})
			}

		case "/ojs/v1/workers/ack":
			var req struct {
				JobID  string         `json:"job_id"`
				Result map[string]any `json:"result"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.JobID != "job-1" {
				t.Errorf("expected job_id=job-1, got %s", req.JobID)
			}
			if req.Result["processed"] != true {
				t.Errorf("expected result.processed=true, got %v", req.Result)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"acknowledged": true,
				"job_id":       req.JobID,
				"state":        "completed",
			})

		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{
				"state": "running",
			})

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)

	worker.Register("test.job", func(ctx JobContext) error {
		jobsProcessed.Add(1)
		if ctx.Job.Args["value"] != "hello" {
			t.Errorf("expected args.value=hello, got %v", ctx.Job.Args["value"])
		}
		ctx.SetResult(map[string]any{"processed": true})
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = worker.Start(ctx)

	if jobsProcessed.Load() != 1 {
		t.Errorf("expected 1 job processed, got %d", jobsProcessed.Load())
	}
}

func TestWorkerMiddleware(t *testing.T) {
	var middlewareCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if middlewareCalled.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "job-mw", "type": "test.job", "state": "active",
							"args": []any{map[string]any{}}, "queue": "default",
							"attempt": 1, "max_attempts": 3,
						},
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

	// Add middleware.
	worker.Use(func(ctx JobContext, next HandlerFunc) error {
		middlewareCalled.Add(1)
		return next(ctx)
	})

	worker.Register("test.job", func(ctx JobContext) error {
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if middlewareCalled.Load() < 1 {
		t.Error("expected middleware to be called")
	}
}

func TestWorkerHandlesServerQuietDirective(t *testing.T) {
	beatCount := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
		case "/ojs/v1/workers/heartbeat":
			count := beatCount.Add(1)
			if count >= 2 {
				// After second heartbeat, tell worker to go quiet.
				json.NewEncoder(w).Encode(map[string]any{"state": "quiet"})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"state": "running"})
			}
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(50*time.Millisecond),
	)
	worker.Register("test.job", func(ctx JobContext) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go worker.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	if worker.State() != WorkerStateQuiet {
		t.Errorf("expected quiet state, got %s", worker.State())
	}
}

func TestWorkerGracefulShutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithGracePeriod(100*time.Millisecond),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)
	worker.Register("test.job", func(ctx JobContext) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := worker.Start(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Should complete within a reasonable time (context timeout + grace period).
	if elapsed > 2*time.Second {
		t.Errorf("shutdown took too long: %v", elapsed)
	}
}

func TestMiddlewareChain(t *testing.T) {
	var order []string
	chain := newMiddlewareChain()

	chain.Add("first", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "first-before")
		err := next(ctx)
		order = append(order, "first-after")
		return err
	})

	chain.Add("second", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "second-before")
		err := next(ctx)
		order = append(order, "second-after")
		return err
	})

	handler := chain.then(func(ctx JobContext) error {
		order = append(order, "handler")
		return nil
	})

	handler(JobContext{})

	expected := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("expected order[%d]=%s, got %s", i, v, order[i])
		}
	}
}

func TestMiddlewareChainOperations(t *testing.T) {
	chain := newMiddlewareChain()
	noop := func(ctx JobContext, next HandlerFunc) error { return next(ctx) }

	chain.Add("a", noop)
	chain.Add("b", noop)
	chain.Add("c", noop)

	if len(chain.middleware) != 3 {
		t.Fatalf("expected 3, got %d", len(chain.middleware))
	}

	// Prepend.
	chain.Prepend("z", noop)
	if chain.middleware[0].name != "z" {
		t.Errorf("expected first to be z, got %s", chain.middleware[0].name)
	}

	// Remove.
	chain.Remove("b")
	if len(chain.middleware) != 3 {
		t.Fatalf("expected 3 after remove, got %d", len(chain.middleware))
	}
	for _, m := range chain.middleware {
		if m.name == "b" {
			t.Error("b should have been removed")
		}
	}
}

func TestMiddlewareInsertBefore(t *testing.T) {
	chain := newMiddlewareChain()
	noop := func(ctx JobContext, next HandlerFunc) error { return next(ctx) }

	chain.Add("a", noop)
	chain.Add("c", noop)

	// Insert "b" before "c".
	chain.InsertBefore("c", "b", noop)

	if len(chain.middleware) != 3 {
		t.Fatalf("expected 3, got %d", len(chain.middleware))
	}
	names := make([]string, len(chain.middleware))
	for i, m := range chain.middleware {
		names[i] = m.name
	}
	expected := []string{"a", "b", "c"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("position %d: expected %s, got %s (order: %v)", i, name, names[i], names)
		}
	}
}

func TestMiddlewareInsertBeforeNotFound(t *testing.T) {
	chain := newMiddlewareChain()
	noop := func(ctx JobContext, next HandlerFunc) error { return next(ctx) }

	chain.Add("a", noop)

	// Insert before non-existent target should append.
	chain.InsertBefore("nonexistent", "b", noop)

	if len(chain.middleware) != 2 {
		t.Fatalf("expected 2, got %d", len(chain.middleware))
	}
	if chain.middleware[1].name != "b" {
		t.Errorf("expected b appended at end, got %s", chain.middleware[1].name)
	}
}

func TestMiddlewareInsertAfter(t *testing.T) {
	chain := newMiddlewareChain()
	noop := func(ctx JobContext, next HandlerFunc) error { return next(ctx) }

	chain.Add("a", noop)
	chain.Add("c", noop)

	// Insert "b" after "a".
	chain.InsertAfter("a", "b", noop)

	if len(chain.middleware) != 3 {
		t.Fatalf("expected 3, got %d", len(chain.middleware))
	}
	names := make([]string, len(chain.middleware))
	for i, m := range chain.middleware {
		names[i] = m.name
	}
	expected := []string{"a", "b", "c"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("position %d: expected %s, got %s (order: %v)", i, name, names[i], names)
		}
	}
}

func TestMiddlewareInsertAfterNotFound(t *testing.T) {
	chain := newMiddlewareChain()
	noop := func(ctx JobContext, next HandlerFunc) error { return next(ctx) }

	chain.Add("a", noop)

	// Insert after non-existent target should append.
	chain.InsertAfter("nonexistent", "b", noop)

	if len(chain.middleware) != 2 {
		t.Fatalf("expected 2, got %d", len(chain.middleware))
	}
	if chain.middleware[1].name != "b" {
		t.Errorf("expected b appended at end, got %s", chain.middleware[1].name)
	}
}

func TestMiddlewareInsertBeforeAfterExecution(t *testing.T) {
	var order []string
	chain := newMiddlewareChain()

	chain.Add("first", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "first")
		return next(ctx)
	})
	chain.Add("third", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "third")
		return next(ctx)
	})

	// Insert "second" after "first".
	chain.InsertAfter("first", "second", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "second")
		return next(ctx)
	})

	// Insert "zeroth" before "first".
	chain.InsertBefore("first", "zeroth", func(ctx JobContext, next HandlerFunc) error {
		order = append(order, "zeroth")
		return next(ctx)
	})

	handler := chain.then(func(ctx JobContext) error {
		order = append(order, "handler")
		return nil
	})

	handler(JobContext{})

	expected := []string{"zeroth", "first", "second", "third", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("expected order[%d]=%s, got %s", i, v, order[i])
		}
	}
}
