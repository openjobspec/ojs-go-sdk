package ojs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerNackOnHandlerError(t *testing.T) {
	var nackReceived atomic.Int32
	var nackCode, nackMessage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if nackReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "fail-job", "type": "test.fail", "state": "active",
							"args": []any{map[string]any{}}, "queue": "default",
							"attempt": 1, "max_attempts": 3,
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/nack":
			var req struct {
				JobID string `json:"job_id"`
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			nackCode = req.Error.Code
			nackMessage = req.Error.Message
			if req.JobID != "fail-job" {
				t.Errorf("expected job_id=fail-job, got %s", req.JobID)
			}
			if !req.Error.Retryable {
				t.Error("expected retryable=true")
			}
			nackReceived.Add(1)
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
	worker.Register("test.fail", func(ctx JobContext) error {
		return fmt.Errorf("processing failed: invalid data")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if nackReceived.Load() < 1 {
		t.Fatal("expected nack to be sent for failed handler")
	}
	if nackCode != "handler_error" {
		t.Errorf("expected nack code=handler_error, got %s", nackCode)
	}
	if nackMessage != "processing failed: invalid data" {
		t.Errorf("expected nack message to contain error, got %s", nackMessage)
	}
}

func TestWorkerNackOnMissingHandler(t *testing.T) {
	var nackReceived atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if nackReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "unknown-job", "type": "unknown.type", "state": "active",
							"args": []any{map[string]any{}}, "queue": "default",
							"attempt": 1, "max_attempts": 3,
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/nack":
			var req struct {
				JobID string `json:"job_id"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Error.Code != "handler_error" {
				t.Errorf("expected code=handler_error, got %s", req.Error.Code)
			}
			nackReceived.Add(1)
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
	// Register a different handler, not "unknown.type".
	worker.Register("other.type", func(ctx JobContext) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if nackReceived.Load() < 1 {
		t.Fatal("expected nack for missing handler")
	}
}

func TestWorkerUseNamed(t *testing.T) {
	var called atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if called.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "j1", "type": "test.job", "state": "active",
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
	worker.UseNamed("my-middleware", func(ctx JobContext, next HandlerFunc) error {
		called.Add(1)
		return next(ctx)
	})
	worker.Register("test.job", func(ctx JobContext) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if called.Load() < 1 {
		t.Error("expected named middleware to be called")
	}
}

func TestJobContextContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKey("test"), "value")
	jc := JobContext{
		ctx: ctx,
		Job: Job{ID: "test-1", Type: "test"},
	}
	if jc.Context().Value(contextKey("test")) != "value" {
		t.Error("expected context value to be preserved")
	}
}

type contextKey string

func TestJobContextHeartbeatNoWorker(t *testing.T) {
	jc := JobContext{
		ctx: context.Background(),
	}
	// Heartbeat with nil worker should be a no-op.
	err := jc.Heartbeat()
	if err != nil {
		t.Errorf("expected nil error for nil worker heartbeat, got %v", err)
	}
}

func TestJobContextHeartbeat(t *testing.T) {
	var heartbeatReceived atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if heartbeatReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "hb-job", "type": "long.task", "state": "active",
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
			heartbeatReceived.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)
	worker.Register("long.task", func(ctx JobContext) error {
		// Call heartbeat from the handler.
		return ctx.Heartbeat()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if heartbeatReceived.Load() < 1 {
		t.Error("expected at least one heartbeat")
	}
}

func TestWorkerHandlesServerTerminateDirective(t *testing.T) {
	beatCount := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
		case "/ojs/v1/workers/heartbeat":
			count := beatCount.Add(1)
			if count >= 2 {
				json.NewEncoder(w).Encode(map[string]any{"state": "terminate"})
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

	if worker.State() != WorkerStateTerminate {
		t.Errorf("expected terminate state, got %s", worker.State())
	}
}

func TestWorkerQuietToRunningTransition(t *testing.T) {
	beatCount := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
		case "/ojs/v1/workers/heartbeat":
			count := beatCount.Add(1)
			if count <= 2 {
				json.NewEncoder(w).Encode(map[string]any{"state": "quiet"})
			} else {
				// After going quiet, tell it to resume.
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

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	go worker.Start(ctx)
	// Wait for quiet state.
	time.Sleep(150 * time.Millisecond)
	if worker.State() != WorkerStateQuiet {
		t.Errorf("expected quiet state, got %s", worker.State())
	}
	// Wait for running state to resume.
	time.Sleep(150 * time.Millisecond)
	if worker.State() != WorkerStateRunning {
		t.Errorf("expected running state after resume, got %s", worker.State())
	}
}

func TestErrorCodeHelper(t *testing.T) {
	tests := []struct {
		err      error
		expected string
	}{
		{&Error{Code: "not_found"}, "not_found"},
		{&Error{Code: "duplicate"}, "duplicate"},
		{fmt.Errorf("not an ojs error"), ""},
	}
	for _, tt := range tests {
		got := ErrorCode(tt.err)
		if got != tt.expected {
			t.Errorf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.expected)
		}
	}
}

func TestErrorIs(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		target   error
		expected bool
	}{
		{"not_found matches ErrNotFound", &Error{Code: ErrCodeNotFound}, ErrNotFound, true},
		{"duplicate matches ErrDuplicate", &Error{Code: ErrCodeDuplicate}, ErrDuplicate, true},
		{"queue_paused matches ErrQueuePaused", &Error{Code: ErrCodeQueuePaused}, ErrQueuePaused, true},
		{"rate_limited matches ErrRateLimited", &Error{Code: ErrCodeRateLimited}, ErrRateLimited, true},
		{"backend_error matches ErrBackend", &Error{Code: ErrCodeBackendError}, ErrBackend, true},
		{"timeout matches ErrTimeout", &Error{Code: ErrCodeTimeout}, ErrTimeout, true},
		{"409 matches ErrConflict", &Error{Code: "unknown", HTTPStatus: 409}, ErrConflict, true},
		{"unknown does not match ErrNotFound", &Error{Code: "unknown"}, ErrNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.expected {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.expected)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	tests := []struct {
		code     string
		status   int
		expected error
	}{
		{ErrCodeNotFound, 404, ErrNotFound},
		{ErrCodeDuplicate, 409, ErrDuplicate},
		{ErrCodeQueuePaused, 422, ErrQueuePaused},
		{ErrCodeRateLimited, 429, ErrRateLimited},
		{ErrCodeBackendError, 500, ErrBackend},
		{ErrCodeTimeout, 408, ErrTimeout},
		{"some_other", 409, ErrConflict},
		{"some_other", 400, nil},
	}
	for _, tt := range tests {
		err := &Error{Code: tt.code, HTTPStatus: tt.status}
		got := err.Unwrap()
		if got != tt.expected {
			t.Errorf("Error{Code:%q, Status:%d}.Unwrap() = %v, want %v", tt.code, tt.status, got, tt.expected)
		}
	}
}

func TestErrorString(t *testing.T) {
	err := &Error{Code: "not_found", Message: "Job not found"}
	if err.Error() != "ojs: not_found: Job not found" {
		t.Errorf("unexpected error string: %s", err.Error())
	}

	errWithReq := &Error{Code: "not_found", Message: "Job not found", RequestID: "req-123"}
	if errWithReq.Error() != "ojs: not_found: Job not found (request_id=req-123)" {
		t.Errorf("unexpected error string: %s", errWithReq.Error())
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(fmt.Errorf("regular error")) {
		t.Error("regular error should not be retryable")
	}
	if IsRetryable(&Error{Code: "not_found", Retryable: false}) {
		t.Error("not_found should not be retryable")
	}
	if !IsRetryable(&Error{Code: "rate_limited", Retryable: true}) {
		t.Error("rate_limited should be retryable")
	}
	if IsRetryable(NonRetryable(fmt.Errorf("fatal error"))) {
		t.Error("NonRetryable-wrapped error should not be retryable")
	}
}

func TestNonRetryable(t *testing.T) {
	// NonRetryable(nil) returns nil.
	if NonRetryable(nil) != nil {
		t.Error("NonRetryable(nil) should return nil")
	}

	// Wrapped error preserves the original message.
	orig := fmt.Errorf("bad input")
	wrapped := NonRetryable(orig)
	if wrapped.Error() != "bad input" {
		t.Errorf("expected message 'bad input', got %q", wrapped.Error())
	}

	// errors.Is still works through NonRetryable.
	if !errors.Is(NonRetryable(ErrNotFound), ErrNotFound) {
		t.Error("errors.Is should work through NonRetryable wrapper")
	}

	// isHandlerRetryable respects NonRetryable.
	if isHandlerRetryable(wrapped) {
		t.Error("isHandlerRetryable should return false for NonRetryable errors")
	}
	if !isHandlerRetryable(fmt.Errorf("regular error")) {
		t.Error("isHandlerRetryable should return true for regular errors")
	}
}

func TestWorkerNackNonRetryableError(t *testing.T) {
	var nackReceived atomic.Int32
	var nackRetryable atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if nackReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "nr-job", "type": "test.nonretryable", "state": "active",
							"args": []any{map[string]any{}}, "queue": "default",
							"attempt": 1, "max_attempts": 3,
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/nack":
			var req struct {
				JobID string `json:"job_id"`
				Error struct {
					Retryable bool `json:"retryable"`
				} `json:"error"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Error.Retryable {
				nackRetryable.Add(1)
			}
			nackReceived.Add(1)
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
	worker.Register("test.nonretryable", func(ctx JobContext) error {
		return NonRetryable(fmt.Errorf("invalid input: cannot process"))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if nackReceived.Load() < 1 {
		t.Fatal("expected nack to be sent")
	}
	if nackRetryable.Load() != 0 {
		t.Error("expected nack with retryable=false for NonRetryable error")
	}
}
