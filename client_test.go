package ojs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client, err := NewClient("http://localhost:8080")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestNewClientEmptyURL(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("NewClient(\"\") should return error")
	}
}

func TestNewClientWithOptions(t *testing.T) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	client, err := NewClient("http://localhost:8080",
		WithHTTPClient(httpClient),
		WithAuthToken("test-token"),
		WithHeader("X-Custom", "value"),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.transport.httpClient != httpClient {
		t.Error("expected custom HTTP client")
	}
	if client.transport.authToken != "test-token" {
		t.Error("expected auth token to be set")
	}
}

func TestEnqueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/jobs" {
			t.Errorf("expected /ojs/v1/jobs, got %s", r.URL.Path)
		}

		ct := r.Header.Get("Content-Type")
		if ct != ojsContentType {
			t.Errorf("expected content type %s, got %s", ojsContentType, ct)
		}

		version := r.Header.Get("OJS-Version")
		if version != ojsVersion {
			t.Errorf("expected OJS-Version %s, got %s", ojsVersion, version)
		}

		// Verify request body.
		var req enqueueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Type != "email.send" {
			t.Errorf("expected type email.send, got %s", req.Type)
		}
		if len(req.Args) != 1 {
			t.Fatalf("expected 1 arg, got %d", len(req.Args))
		}
		argMap, ok := req.Args[0].(map[string]any)
		if !ok {
			t.Fatal("expected args[0] to be a map")
		}
		if argMap["to"] != "user@example.com" {
			t.Errorf("expected to=user@example.com, got %v", argMap["to"])
		}
		if req.Options.Queue != "email" {
			t.Errorf("expected queue=email, got %s", req.Options.Queue)
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id":           "019414d4-8b2e-7c3a-b5d1-f0e2a3b4c5d6",
				"type":         "email.send",
				"state":        "available",
				"args":         []any{map[string]any{"to": "user@example.com"}},
				"queue":        "email",
				"priority":     0,
				"attempt":      0,
				"max_attempts":  5,
				"created_at":   "2026-02-12T10:30:00.000Z",
				"enqueued_at":  "2026-02-12T10:30:00.123Z",
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	job, err := client.Enqueue(context.Background(), "email.send",
		Args{"to": "user@example.com"},
		WithQueue("email"),
		WithRetry(RetryPolicy{MaxAttempts: 5}),
	)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if job.ID != "019414d4-8b2e-7c3a-b5d1-f0e2a3b4c5d6" {
		t.Errorf("expected job ID, got %s", job.ID)
	}
	if job.Type != "email.send" {
		t.Errorf("expected type email.send, got %s", job.Type)
	}
	if job.State != JobStateAvailable {
		t.Errorf("expected state available, got %s", job.State)
	}
	if job.Args["to"] != "user@example.com" {
		t.Errorf("expected args.to=user@example.com, got %v", job.Args["to"])
	}
}

func TestEnqueueBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ojs/v1/jobs/batch" {
			t.Errorf("expected /ojs/v1/jobs/batch, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"jobs": []map[string]any{
				{"id": "job-1", "type": "email.send", "state": "available", "args": []any{map[string]any{"to": "a@example.com"}}, "queue": "default"},
				{"id": "job-2", "type": "email.send", "state": "available", "args": []any{map[string]any{"to": "b@example.com"}}, "queue": "default"},
			},
			"count": 2,
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobs, err := client.EnqueueBatch(context.Background(), []JobRequest{
		{Type: "email.send", Args: Args{"to": "a@example.com"}},
		{Type: "email.send", Args: Args{"to": "b@example.com"}},
	})
	if err != nil {
		t.Fatalf("EnqueueBatch() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestGetJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/jobs/test-id" {
			t.Errorf("expected /ojs/v1/jobs/test-id, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id":    "test-id",
				"type":  "email.send",
				"state": "completed",
				"args":  []any{map[string]any{"to": "user@example.com"}},
				"queue": "email",
				"result": map[string]any{
					"message_id": "msg-123",
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	job, err := client.GetJob(context.Background(), "test-id")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if job.State != JobStateCompleted {
		t.Errorf("expected completed, got %s", job.State)
	}
	if job.Result["message_id"] != "msg-123" {
		t.Errorf("expected result message_id, got %v", job.Result)
	}
}

func TestCancelJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id":             "test-id",
				"type":           "email.send",
				"state":          "cancelled",
				"args":           []any{},
				"queue":          "email",
				"previous_state": "available",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	job, err := client.CancelJob(context.Background(), "test-id")
	if err != nil {
		t.Fatalf("CancelJob() error = %v", err)
	}
	if job.State != JobStateCancelled {
		t.Errorf("expected cancelled, got %s", job.State)
	}
	if job.PreviousState != "available" {
		t.Errorf("expected previous_state=available, got %s", job.PreviousState)
	}
}

func TestErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":       "not_found",
				"message":    "Job 'test-id' not found.",
				"retryable":  false,
				"request_id": "req_123",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.GetJob(context.Background(), "test-id")
	if err == nil {
		t.Fatal("expected error")
	}

	// Test errors.Is with sentinel error.
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Test errors.As.
	var ojsErr *Error
	if !errors.As(err, &ojsErr) {
		t.Fatal("expected *Error")
	}
	if ojsErr.Code != ErrCodeNotFound {
		t.Errorf("expected code not_found, got %s", ojsErr.Code)
	}
	if ojsErr.RequestID != "req_123" {
		t.Errorf("expected request_id req_123, got %s", ojsErr.RequestID)
	}
	if IsRetryable(err) {
		t.Error("not_found should not be retryable")
	}
}

func TestErrorIsDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "duplicate",
				"message":   "A job with the same unique key already exists.",
				"retryable": false,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.Enqueue(context.Background(), "test", Args{})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}

func TestErrorIsRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "Rate limit exceeded.",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.Enqueue(context.Background(), "test", Args{})
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if !IsRetryable(err) {
		t.Error("rate_limited should be retryable")
	}
}

func TestErrorRetryAfterHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "Rate limit exceeded.",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.Enqueue(context.Background(), "test", Args{})

	var ojsErr *Error
	if !errors.As(err, &ojsErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if ojsErr.RetryAfter != 60*time.Second {
		t.Errorf("expected RetryAfter=60s, got %v", ojsErr.RetryAfter)
	}
}

func TestErrorRetryAfterHeaderAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "Rate limit exceeded.",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.Enqueue(context.Background(), "test", Args{})

	var ojsErr *Error
	if !errors.As(err, &ojsErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if ojsErr.RetryAfter != 0 {
		t.Errorf("expected RetryAfter=0, got %v", ojsErr.RetryAfter)
	}
}

func TestErrorRateLimitInfoHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "Rate limit exceeded.",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.Enqueue(context.Background(), "test", Args{})

	var ojsErr *Error
	if !errors.As(err, &ojsErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if ojsErr.RateLimit == nil {
		t.Fatal("expected RateLimit to be set")
	}
	if ojsErr.RateLimit.Limit != 100 {
		t.Errorf("expected Limit=100, got %d", ojsErr.RateLimit.Limit)
	}
	if ojsErr.RateLimit.Remaining != 0 {
		t.Errorf("expected Remaining=0, got %d", ojsErr.RateLimit.Remaining)
	}
	if ojsErr.RateLimit.Reset != 1700000000 {
		t.Errorf("expected Reset=1700000000, got %d", ojsErr.RateLimit.Reset)
	}
	if ojsErr.RateLimit.RetryAfter != 30*time.Second {
		t.Errorf("expected RetryAfter=30s, got %v", ojsErr.RateLimit.RetryAfter)
	}
}

func TestHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ojs/v1/health" {
			t.Errorf("expected /ojs/v1/health, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"version":        "1.0.0-rc.1",
			"uptime_seconds": 86400,
			"backend": map[string]any{
				"type":       "redis",
				"status":     "connected",
				"latency_ms": 2,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected status ok, got %s", health.Status)
	}
	if health.Backend.Type != "redis" {
		t.Errorf("expected backend type redis, got %s", health.Backend.Type)
	}
}

func TestAuthToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("expected Bearer my-token, got %s", auth)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, WithAuthToken("my-token"))
	_, _ = client.Health(context.Background())
}

func TestJobStateIsTerminal(t *testing.T) {
	tests := []struct {
		state    JobState
		terminal bool
	}{
		{JobStatePending, false},
		{JobStateScheduled, false},
		{JobStateAvailable, false},
		{JobStateActive, false},
		{JobStateRetryable, false},
		{JobStateCompleted, true},
		{JobStateCancelled, true},
		{JobStateDiscarded, true},
	}
	for _, tt := range tests {
		if got := tt.state.IsTerminal(); got != tt.terminal {
			t.Errorf("JobState(%q).IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}

func TestArgsWireFormat(t *testing.T) {
	args := Args{"to": "user@example.com", "subject": "hello"}
	wire := argsToWire(args)
	if len(wire) != 1 {
		t.Fatalf("expected 1 element, got %d", len(wire))
	}
	m, ok := wire[0].(map[string]any)
	if !ok {
		t.Fatal("expected map")
	}
	if m["to"] != "user@example.com" {
		t.Errorf("expected to=user@example.com, got %v", m["to"])
	}

	// Round-trip.
	back := argsFromWire(wire)
	if back["to"] != "user@example.com" {
		t.Errorf("round-trip failed: %v", back)
	}
}

func TestArgsFromWirePositional(t *testing.T) {
	wire := []any{"user@example.com", "welcome", map[string]any{"locale": "en"}}
	args := argsFromWire(wire)
	// First element is a string, not a map, so fallback to indexed keys.
	if args["0"] != "user@example.com" {
		t.Errorf("expected positional arg 0, got %v", args["0"])
	}
	if args["1"] != "welcome" {
		t.Errorf("expected positional arg 1, got %v", args["1"])
	}
}

func TestJobJSONRoundTrip(t *testing.T) {
	original := `{
		"id": "test-123",
		"type": "email.send",
		"state": "available",
		"queue": "email",
		"args": [{"to": "user@example.com"}],
		"priority": 0,
		"attempt": 1,
		"max_attempts": 3,
		"tags": ["onboarding"]
	}`

	var job Job
	if err := json.Unmarshal([]byte(original), &job); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if job.Args["to"] != "user@example.com" {
		t.Errorf("expected to=user@example.com, got %v", job.Args["to"])
	}
	if len(job.RawArgs) != 1 {
		t.Errorf("expected 1 raw arg, got %d", len(job.RawArgs))
	}

	// Marshal back and verify.
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var result map[string]any
	json.Unmarshal(data, &result)
	args, ok := result["args"].([]any)
	if !ok {
		t.Fatal("expected args to be array")
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}

func TestGetJobURLEncoding(t *testing.T) {
	var receivedURI string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id": "id/with/slashes", "type": "test", "state": "available",
				"args": []any{}, "queue": "default", "attempt": 0, "max_attempts": 3,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.GetJob(context.Background(), "id/with/slashes")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	expected := "/ojs/v1/jobs/id%2Fwith%2Fslashes"
	if receivedURI != expected {
		t.Errorf("expected URI %q, got %q", expected, receivedURI)
	}
}
