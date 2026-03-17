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

func TestAckJobWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	err := w.ackJobWithRetry(context.Background(), "job-1", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestAckJobWithRetry_RetriesOnFailure(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": "internal_error", "message": "temporary failure"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	err := w.ackJobWithRetry(context.Background(), "job-1", nil)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestAckJobWithRetry_ExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "internal_error", "message": "persistent failure"},
		})
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	err := w.ackJobWithRetry(context.Background(), "job-1", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if got := attempts.Load(); got != int32(ackNackMaxRetries) {
		t.Errorf("expected %d attempts, got %d", ackNackMaxRetries, got)
	}
}

func TestAckJobWithRetry_RespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "internal_error", "message": "fail"},
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w := NewWorker(server.URL)
	err := w.ackJobWithRetry(ctx, "job-1", nil)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestNackJobWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	err := w.nackJobWithRetry(context.Background(), "job-1", "handler_error", "some error", true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNackJobWithRetry_RetriesOnFailure(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		count := attempts.Add(1)
		if count < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": "internal_error", "message": "temporary"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	err := w.nackJobWithRetry(context.Background(), "job-1", "handler_error", "fail", true)
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}

	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestHeartbeatLoopLogsErrors(t *testing.T) {
	// Verify the heartbeat loop calls sendHeartbeat and doesn't panic on errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":{"code":"internal","message":"test"}}`)
	}))
	defer server.Close()

	w := NewWorker(server.URL, WithHeartbeatInterval(50*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should not panic even when heartbeats fail
	w.heartbeatLoop(ctx)
}

func TestFetchBackoffCalculation(t *testing.T) {
	base := 100 * time.Millisecond

	tests := []struct {
		errors   int
		expected time.Duration
	}{
		{0, base},
		{1, base},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{15, maxFetchBackoff}, // capped
	}

	for _, tt := range tests {
		got := fetchBackoff(tt.errors, base)
		if got != tt.expected {
			t.Errorf("fetchBackoff(%d, %v) = %v, want %v", tt.errors, base, got, tt.expected)
		}
	}
}
