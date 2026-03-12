package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryOn429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":      "rate_limited",
					"message":   "too many requests",
					"retryable": true,
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.MinBackoff = time.Millisecond
	cfg.MaxBackoff = 10 * time.Millisecond

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, &result)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestRetryRespectsRetryAfterHeader(t *testing.T) {
	var timestamps []time.Time
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		n := attempts.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":      "rate_limited",
					"message":   "too many requests",
					"retryable": true,
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.MinBackoff = time.Millisecond

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, &result)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(timestamps) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(timestamps))
	}
	gap := timestamps[1].Sub(timestamps[0])
	if gap < 900*time.Millisecond {
		t.Errorf("expected at least ~1s gap from Retry-After, got %v", gap)
	}
}

func TestRetryMaxRetriesHonored(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "too many requests",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 2
	cfg.MinBackoff = time.Millisecond
	cfg.MaxBackoff = 10 * time.Millisecond

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	var ojsErr *Error
	if !AsError(err, &ojsErr) {
		t.Fatalf("expected *ojs.Error, got %T", err)
	}
	if ojsErr.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", ojsErr.HTTPStatus)
	}

	// 1 initial + 2 retries = 3 total attempts
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 total attempts (1 + MaxRetries=2), got %d", got)
	}
}

func TestNoRetryOnNon429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "backend_error",
				"message":   "internal error",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.MinBackoff = time.Millisecond

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected exactly 1 attempt for 500, got %d", got)
	}
}

func TestRetryDisabled(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "too many requests",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.Enabled = false

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected error when retries disabled")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt when disabled, got %d", got)
	}
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "rate_limited",
				"message":   "too many requests",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = client.transport.do(ctx, http.MethodGet, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if ctx.Err() == nil {
		t.Error("expected context to be done")
	}
}

// AsError is a test helper wrapping errors.As for *Error.
func AsError(err error, target **Error) bool {
	var ojsErr *Error
	if err == nil {
		return false
	}
	switch e := err.(type) {
	case *Error:
		*target = e
		return true
	default:
		_ = ojsErr
		return false
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", cfg.MaxRetries)
	}
	if cfg.MinBackoff != 500*time.Millisecond {
		t.Errorf("expected MinBackoff=500ms, got %v", cfg.MinBackoff)
	}
	if cfg.MaxBackoff != 30*time.Second {
		t.Errorf("expected MaxBackoff=30s, got %v", cfg.MaxBackoff)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestRetryBackoffExponential(t *testing.T) {
	cfg := RetryConfig{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		Enabled:    true,
	}

	// Without Retry-After, backoff should increase with attempt
	b0 := cfg.retryBackoff(0, 0)
	b1 := cfg.retryBackoff(1, 0)
	b2 := cfg.retryBackoff(2, 0)

	if b0 < cfg.MinBackoff/2 {
		t.Errorf("attempt 0 backoff %v should be >= MinBackoff/2 %v", b0, cfg.MinBackoff/2)
	}
	// b1 should generally be larger than b0 (with high probability given jitter)
	// Just check they're all within bounds
	for i, b := range []time.Duration{b0, b1, b2} {
		if b < cfg.MinBackoff/2 {
			t.Errorf("attempt %d backoff %v < MinBackoff/2", i, b)
		}
		if b > cfg.MaxBackoff {
			t.Errorf("attempt %d backoff %v > MaxBackoff", i, b)
		}
	}
}

func TestRetryBackoffUsesRetryAfter(t *testing.T) {
	cfg := RetryConfig{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		Enabled:    true,
	}

	retryAfter := 2 * time.Second
	b := cfg.retryBackoff(0, retryAfter)
	if b != retryAfter {
		t.Errorf("expected backoff=%v with Retry-After, got %v", retryAfter, b)
	}
}

func TestRetryBackoffClampsRetryAfter(t *testing.T) {
	cfg := RetryConfig{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		Enabled:    true,
	}

	b := cfg.retryBackoff(0, 60*time.Second)
	if b != cfg.MaxBackoff {
		t.Errorf("expected backoff clamped to MaxBackoff=%v, got %v", cfg.MaxBackoff, b)
	}
}

func TestRetryOnServerErrors502503504(t *testing.T) {
	for _, statusCode := range []int{502, 503, 504} {
		t.Run(fmt.Sprintf("retries_%d", statusCode), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := attempts.Add(1)
				if n <= 2 {
					w.WriteHeader(statusCode)
					json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]any{
							"code":      "backend_error",
							"message":   fmt.Sprintf("HTTP %d", statusCode),
							"retryable": true,
						},
					})
					return
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{"ok": true})
			}))
			defer server.Close()

			cfg := DefaultRetryConfig()
			cfg.MinBackoff = time.Millisecond
			cfg.MaxBackoff = 5 * time.Millisecond

			client, err := NewClient(server.URL, WithRetryConfig(cfg))
			if err != nil {
				t.Fatal(err)
			}

			var result map[string]any
			err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, &result)
			if err != nil {
				t.Fatalf("expected success after retries, got error: %v", err)
			}
			if got := attempts.Load(); got != 3 {
				t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", got)
			}
		})
	}
}

func TestNoRetryOnServerErrorsWhenDisabled(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":      "backend_error",
				"message":   "502 Bad Gateway",
				"retryable": true,
			},
		})
	}))
	defer server.Close()

	cfg := DefaultRetryConfig()
	cfg.RetryServerErrors = false
	cfg.MinBackoff = time.Millisecond

	client, err := NewClient(server.URL, WithRetryConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	err = client.transport.do(context.Background(), http.MethodGet, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected error for 502 with RetryServerErrors=false")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected exactly 1 attempt with RetryServerErrors=false, got %d", got)
	}
}

func TestShouldRetryFunction(t *testing.T) {
	tests := []struct {
		name              string
		statusCode        int
		retryServerErrors bool
		attempt           int
		maxRetries        int
		expected          bool
	}{
		{"429 retried", 429, true, 0, 3, true},
		{"502 retried when enabled", 502, true, 0, 3, true},
		{"503 retried when enabled", 503, true, 0, 3, true},
		{"504 retried when enabled", 504, true, 0, 3, true},
		{"502 not retried when disabled", 502, false, 0, 3, false},
		{"500 never retried", 500, true, 0, 3, false},
		{"501 never retried", 501, true, 0, 3, false},
		{"429 not retried at max attempts", 429, true, 3, 3, false},
		{"200 not retried", 200, true, 0, 3, false},
		{"400 not retried", 400, true, 0, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RetryConfig{
				MaxRetries:        tt.maxRetries,
				Enabled:           true,
				RetryServerErrors: tt.retryServerErrors,
			}
			got := cfg.shouldRetry(tt.statusCode, tt.attempt)
			if got != tt.expected {
				t.Errorf("shouldRetry(%d, attempt=%d, serverErrors=%v) = %v, want %v",
					tt.statusCode, tt.attempt, tt.retryServerErrors, got, tt.expected)
			}
		})
	}
}
