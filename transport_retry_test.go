package ojs

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// This file proves the transport's retry classification is by HTTP method AND
// operation, not by status code alone: a non-idempotent action (enqueue,
// batch enqueue, workflow creation, worker fetch/ack/nack/heartbeat, durable
// checkpoint save) must never be retried automatically just because the
// response looked transient (429/502/503/504), since the server may already
// have committed the write before the response was lost. A safe operation
// (GET/HEAD, and the idempotent DELETE/POST operations this SDK issues) keeps
// the previous useful retry behavior.

func fastRetryConfig() RetryConfig {
	cfg := DefaultRetryConfig()
	cfg.MinBackoff = time.Millisecond
	cfg.MaxBackoff = 5 * time.Millisecond
	return cfg
}

// fastRetryConfigPtr is fastRetryConfig behind a pointer, for the
// clientConfig.retryConfig field.
func fastRetryConfigPtr() *RetryConfig {
	cfg := fastRetryConfig()
	return &cfg
}

// --- Retry matrix: one row per operation, transient-status and connection-error columns ---

type retryMatrixCase struct {
	name string
	// call performs the operation once against a client/worker pointed at
	// server. It returns the error from the SDK call.
	call func(t *testing.T, serverURL string) error
	// retryEligible is what this test asserts: whether the operation must be
	// retried automatically on a transient response.
	retryEligible bool
}

func retryMatrixCases() []retryMatrixCase {
	return []retryMatrixCase{
		// --- Non-idempotent: must NOT be retried automatically ---
		{
			name:          "Enqueue",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.Enqueue(context.Background(), "a.job", Args{})
				return err
			},
		},
		{
			name:          "EnqueueBatch",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.EnqueueBatch(context.Background(), []JobRequest{{Type: "a.job", Args: Args{}}})
				return err
			},
		},
		{
			name:          "CreateWorkflow",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.CreateWorkflow(context.Background(), Chain(Step{Type: "a.job", Args: Args{}}))
				return err
			},
		},
		{
			name:          "RetryDeadLetterJob",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.RetryDeadLetterJob(context.Background(), "job-1")
				return err
			},
		},
		{
			name:          "RegisterCronJob",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.RegisterCronJob(context.Background(), CronJobRequest{Name: "n", Cron: "* * * * *", Type: "a.job"})
				return err
			},
		},
		{
			name:          "WorkerFetch",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				w := NewWorker(url)
				w.transport.retryConfig = fastRetryConfig()
				_, err := w.fetchJobs(context.Background(), 1)
				return err
			},
		},
		{
			name:          "WorkerAck",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				w := NewWorker(url)
				w.transport.retryConfig = fastRetryConfig()
				return w.ackJob(context.Background(), "job-1", nil)
			},
		},
		{
			name:          "WorkerNack",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				w := NewWorker(url)
				w.transport.retryConfig = fastRetryConfig()
				return w.nackJob(context.Background(), "job-1", "E", "message", true)
			},
		},
		{
			name:          "WorkerHeartbeat",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				w := NewWorker(url)
				w.transport.retryConfig = fastRetryConfig()
				return w.sendHeartbeat(context.Background())
			},
		},
		{
			name:          "DurableCheckpointSave",
			retryEligible: false,
			call: func(t *testing.T, url string) error {
				tp := newTransport(url, clientConfig{retryConfig: fastRetryConfigPtr()})
				dc := &DurableContext{parent: context.Background(), transport: tp, jobID: "job-1", attempt: 1}
				return dc.Checkpoint(1, map[string]any{"k": "v"})
			},
		},

		// --- Idempotent / safe: existing retry behavior is preserved ---
		{
			name:          "GetJob",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.GetJob(context.Background(), "job-1")
				return err
			},
		},
		{
			name:          "CancelJob",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.CancelJob(context.Background(), "job-1")
				return err
			},
		},
		{
			name:          "GetWorkflow",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.GetWorkflow(context.Background(), "wf-1")
				return err
			},
		},
		{
			name:          "CancelWorkflow",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				_, err := c.CancelWorkflow(context.Background(), "wf-1")
				return err
			},
		},
		{
			name:          "PauseQueue",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				return c.PauseQueue(context.Background(), "default")
			},
		},
		{
			name:          "ResumeQueue",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				return c.ResumeQueue(context.Background(), "default")
			},
		},
		{
			name:          "DiscardDeadLetterJob",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				return c.DiscardDeadLetterJob(context.Background(), "job-1")
			},
		},
		{
			name:          "UnregisterCronJob",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				c, _ := NewClient(url, WithRetryConfig(fastRetryConfig()))
				return c.UnregisterCronJob(context.Background(), "n")
			},
		},
		{
			name:          "DurableCheckpointComplete",
			retryEligible: true,
			call: func(t *testing.T, url string) error {
				tp := newTransport(url, clientConfig{retryConfig: fastRetryConfigPtr()})
				dc := &DurableContext{parent: context.Background(), transport: tp, jobID: "job-1", attempt: 1}
				return dc.Complete()
			},
		},
	}
}

// TestTransportRetryMatrix drives every case against a server that fails with
// 503 exactly once and then succeeds, and asserts retry-eligible operations
// recover (2 requests, no error) while non-idempotent operations surface the
// 503 immediately without any retry (exactly 1 request).
func TestTransportRetryMatrix(t *testing.T) {
	for _, tc := range retryMatrixCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ojsContentType)
				n := requests.Add(1)
				if n == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]any{"code": "unavailable", "message": "temporary", "retryable": true},
					})
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"job":      map[string]any{"id": "job-1", "type": "a.job", "args": []any{}},
					"jobs":     []any{map[string]any{"id": "job-1", "type": "a.job", "args": []any{}}},
					"workflow": map[string]any{"id": "wf-1", "state": "running"},
					"cron_job": map[string]any{"name": "n", "cron": "* * * * *", "type": "a.job"},
					"checkpoint": map[string]any{
						"job_id": "job-1", "sequence": 1,
						"state": map[string]any{"ojs_go_durable_version": durableCheckpointVersion, "step_index": 0, "attempt": 1},
					},
				})
			}))
			defer server.Close()

			err := tc.call(t, server.URL)

			if tc.retryEligible {
				if err != nil {
					t.Fatalf("%s: expected the retry to recover, got error: %v", tc.name, err)
				}
				if got := requests.Load(); got != 2 {
					t.Errorf("%s: requests = %d, want 2 (one 503 + one retry)", tc.name, got)
				}
			} else {
				if err == nil {
					t.Fatalf("%s: expected the 503 to surface without a retry, got no error", tc.name)
				}
				if got := requests.Load(); got != 1 {
					t.Errorf("%s: requests = %d, want exactly 1 (no automatic retry for a non-idempotent operation)", tc.name, got)
				}
			}
		})
	}
}

// --- Proxy-commit-then-503: the server actually applies the write, but the
// response the client sees is a transient failure, as a proxy between the
// origin and the client might produce after the origin already committed. ---

// TestProxyCommitThen503DoesNotDuplicateEnqueue is the direct scenario the
// finding calls out: an enqueue that the backend committed, observed by the
// client only as a 503, must not be retried into a second job.
func TestProxyCommitThen503DoesNotDuplicateEnqueue(t *testing.T) {
	var committed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		committed.Add(1) // the backend "commits" the enqueue on every request it receives
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusServiceUnavailable) // ...but the proxy always reports failure
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Enqueue(context.Background(), "a.job", Args{})
	if err == nil {
		t.Fatal("expected the 503 to surface as an error")
	}
	if got := committed.Load(); got != 1 {
		t.Fatalf("backend committed %d times, want exactly 1: the SDK must not retry a non-idempotent enqueue", got)
	}
}

// TestProxyCommitThen503DoesNotDuplicateBatchEnqueue covers EnqueueBatch,
// which would duplicate every job in the batch if retried.
func TestProxyCommitThen503DoesNotDuplicateBatchEnqueue(t *testing.T) {
	var committed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		committed.Add(1)
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.EnqueueBatch(context.Background(), []JobRequest{
		{Type: "a.job", Args: Args{}}, {Type: "b.job", Args: Args{}},
	})
	if err == nil {
		t.Fatal("expected the 503 to surface as an error")
	}
	if got := committed.Load(); got != 1 {
		t.Fatalf("backend committed %d times, want exactly 1: a batch enqueue must not be retried", got)
	}
}

// TestProxyCommitThen503DoesNotDuplicateWorkflowCreation covers workflow
// creation, named explicitly by the finding.
func TestProxyCommitThen503DoesNotDuplicateWorkflowCreation(t *testing.T) {
	var committed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		committed.Add(1)
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.CreateWorkflow(context.Background(), Chain(Step{Type: "a.job", Args: Args{}}))
	if err == nil {
		t.Fatal("expected the 503 to surface as an error")
	}
	if got := committed.Load(); got != 1 {
		t.Fatalf("backend committed %d times, want exactly 1: workflow creation must not be retried", got)
	}
}

// TestProxyCommitThen503DoesNotDuplicateFetchReservation proves a worker fetch
// that reserved jobs server-side, observed only as a 503, is not retried into
// a second, redundant reservation.
func TestProxyCommitThen503DoesNotDuplicateFetchReservation(t *testing.T) {
	var reservations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reservations.Add(1)
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
		})
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	w.transport.retryConfig = fastRetryConfig()
	_, err := w.fetchJobs(context.Background(), 5)
	if err == nil {
		t.Fatal("expected the 503 to surface as an error")
	}
	if got := reservations.Load(); got != 1 {
		t.Fatalf("backend reserved %d times, want exactly 1: fetch must not be retried", got)
	}
}

// TestProxyCommitThen503DoesNotDuplicateAckAction proves an ack the server
// already recorded, observed only as a 503, is not retried by the transport
// into a second action. (The worker's own explicit ackJobWithRetry, exercised
// in worker_retry_test.go, is a separate, deliberate application-level
// mechanism scoped to a single already-owned job ID; this test is about the
// transport's automatic per-call retry underneath a single ackJob call.)
func TestProxyCommitThen503DoesNotDuplicateAckAction(t *testing.T) {
	var actions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actions.Add(1)
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
		})
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	w.transport.retryConfig = fastRetryConfig()
	err := w.ackJob(context.Background(), "job-1", nil)
	if err == nil {
		t.Fatal("expected the 503 to surface as an error")
	}
	if got := actions.Load(); got != 1 {
		t.Fatalf("backend actions = %d, want exactly 1: a single ackJob call must not be retried by the transport", got)
	}
}

// TestProxyCommitThenServerErrorStillRetriesIdempotentOperation is the
// contrasting positive case: an operation this SDK has vetted as idempotent
// (queue pause) still gets the useful retry-on-503 behavior.
func TestProxyCommitThenServerErrorStillRetriesIdempotentOperation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		n := requests.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "unavailable", "message": "upstream timeout", "retryable": true},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.PauseQueue(context.Background(), "default"); err != nil {
		t.Fatalf("PauseQueue: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2: pausing a queue is idempotent and should retry through the transient 503", got)
	}
}

func TestPOSTRateLimitWithRetryAfterRetriesThenSucceeds(t *testing.T) {
	tests := []struct {
		name    string
		success map[string]any
		call    func(*Client) error
	}{
		{
			name: "enqueue",
			success: map[string]any{
				"job": map[string]any{"id": "job-1", "type": "a.job", "args": []any{}},
			},
			call: func(client *Client) error {
				_, err := client.Enqueue(context.Background(), "a.job", Args{})
				return err
			},
		},
		{
			name: "workflow",
			success: map[string]any{
				"workflow": map[string]any{"id": "wf-1", "state": "running"},
			},
			call: func(client *Client) error {
				_, err := client.CreateWorkflow(context.Background(), Chain(Step{Type: "a.job", Args: Args{}}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ojsContentType)
				if requests.Add(1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]any{"code": "rate_limited", "message": "slow down", "retryable": true},
					})
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(tt.success)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if err := tt.call(client); err != nil {
				t.Fatalf("%s: expected recovery after 429, got %v", tt.name, err)
			}
			if got := requests.Load(); got != 2 {
				t.Errorf("requests = %d, want 2 (429 plus successful retry)", got)
			}
		})
	}
}

func TestPOSTServiceUnavailableIsNotRetried(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "enqueue",
			call: func(client *Client) error {
				_, err := client.Enqueue(context.Background(), "a.job", Args{})
				return err
			},
		},
		{
			name: "workflow",
			call: func(client *Client) error {
				_, err := client.CreateWorkflow(context.Background(), Chain(Step{Type: "a.job", Args: Args{}}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", ojsContentType)
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "unavailable", "message": "temporary", "retryable": true},
				})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, WithRetryConfig(fastRetryConfig()))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if err := tt.call(client); err == nil {
				t.Fatal("expected 503 to surface")
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("requests = %d, want exactly 1", got)
			}
		})
	}
}

func TestPOSTRateLimitHonorsMaxRetries(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", ojsContentType)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "rate_limited", "message": "slow down", "retryable": true},
		})
	}))
	defer server.Close()

	cfg := fastRetryConfig()
	cfg.MaxRetries = 1
	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Enqueue(context.Background(), "a.job", Args{}); err == nil {
		t.Fatal("expected final 429 after retry budget was exhausted")
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (initial attempt plus one configured retry)", got)
	}
}

func TestPOSTRateLimitHonorsContextDuringRetryAfter(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.MaxBackoff = time.Minute
	tp := newTransport("http://example.invalid", clientConfig{retryConfig: &cfg})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"60"}},
	}
	retry, err := tp.statusRetryDecision(ctx, 0, "/ojs/v1/jobs", false, resp)
	if retry {
		t.Fatal("retry = true after context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("statusRetryDecision error = %v, want context.Canceled", err)
	}
}

// --- Connection-error classification ---

// dialFailureThenSucceedTransport fails the first failCount round trips with
// a pre-dial error (as if the connection could not be established), then
// delegates to next.
type dialFailureThenSucceedTransport struct {
	failCount int
	attempts  atomic.Int32
	next      http.RoundTripper
}

func (rt *dialFailureThenSucceedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := rt.attempts.Add(1)
	if int(n) <= rt.failCount {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return rt.next.RoundTrip(req)
}

// midConnectionFailureThenSucceedTransport fails the first failCount round
// trips with an error that is not provably pre-write (e.g. a connection reset
// while writing, or a response read timeout), then delegates to next.
type midConnectionFailureThenSucceedTransport struct {
	failCount int
	attempts  atomic.Int32
	next      http.RoundTripper
}

func (rt *midConnectionFailureThenSucceedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := rt.attempts.Add(1)
	if int(n) <= rt.failCount {
		return nil, &net.OpError{Op: "write", Net: "tcp", Err: errors.New("connection reset by peer")}
	}
	return rt.next.RoundTrip(req)
}

func jobOKHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{"id": "job-1", "type": "a.job", "args": []any{}},
		})
	}
}

// TestPreDialFailureIsRetriedRegardlessOfOperation proves a connection that
// was never established -- so nothing could have been written -- is retried
// even for a non-idempotent operation like Enqueue.
func TestPreDialFailureIsRetriedRegardlessOfOperation(t *testing.T) {
	server := httptest.NewServer(jobOKHandler())
	defer server.Close()

	rt := &dialFailureThenSucceedTransport{failCount: 2, next: http.DefaultTransport}
	cfg := fastRetryConfig()
	client, err := NewClient(server.URL, WithRetryConfig(cfg), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Enqueue(context.Background(), "a.job", Args{})
	if err != nil {
		t.Fatalf("Enqueue: expected recovery after pre-dial failures, got %v", err)
	}
	if got := rt.attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (2 pre-dial failures + 1 success)", got)
	}
}

// TestMidConnectionFailureIsNotRetriedForNonIdempotentOperation proves a
// connection error that is NOT provably pre-write is not retried for an
// operation with no idempotency mechanism: the server may already have
// received and processed the request.
func TestMidConnectionFailureIsNotRetriedForNonIdempotentOperation(t *testing.T) {
	server := httptest.NewServer(jobOKHandler())
	defer server.Close()

	rt := &midConnectionFailureThenSucceedTransport{failCount: 1, next: http.DefaultTransport}
	cfg := fastRetryConfig()
	client, err := NewClient(server.URL, WithRetryConfig(cfg), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Enqueue(context.Background(), "a.job", Args{})
	if err == nil {
		t.Fatal("expected the mid-connection failure to surface without a retry")
	}
	if got := rt.attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want exactly 1: a non-pre-dial failure must not be retried for enqueue", got)
	}
}

// TestMidConnectionFailureIsRetriedForIdempotentOperation proves the same
// non-pre-dial connection error IS retried when the operation itself is
// already safe to repeat.
func TestMidConnectionFailureIsRetriedForIdempotentOperation(t *testing.T) {
	server := httptest.NewServer(func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ojsContentType)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job": map[string]any{"id": "job-1", "type": "a.job", "args": []any{}},
			})
		}
	}())
	defer server.Close()

	rt := &midConnectionFailureThenSucceedTransport{failCount: 1, next: http.DefaultTransport}
	cfg := fastRetryConfig()
	client, err := NewClient(server.URL, WithRetryConfig(cfg), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("GetJob: expected recovery after a mid-connection failure, got %v", err)
	}
	if got := rt.attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2 (1 failure + 1 retry): GET is safe to retry regardless of failure phase", got)
	}
}

// TestIsPreDialFailureClassification unit-tests the classifier directly
// against the exact error shapes the two connection-error tests above rely on.
func TestIsPreDialFailureClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"dial op error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, true},
		{"write op error", &net.OpError{Op: "write", Net: "tcp", Err: errors.New("broken pipe")}, false},
		{"read op error", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}, false},
		{"wrapped dial error", errWrap{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}}, true},
		{"generic error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPreDialFailure(tt.err); got != tt.want {
				t.Errorf("isPreDialFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// errWrap wraps an error for errors.As unwrapping tests.
type errWrap struct{ err error }

func (e errWrap) Error() string { return "wrapped: " + e.err.Error() }
func (e errWrap) Unwrap() error { return e.err }
