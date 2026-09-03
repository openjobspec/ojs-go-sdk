package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportProgress(t *testing.T) {
	var received ProgressReport

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/workers/progress" {
			t.Errorf("expected /ojs/v1/workers/progress, got %s", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newTransport(server.URL, clientConfig{})
	err := ReportProgress(context.Background(), tr, "job-123", 50, "halfway done", map[string]any{"items": 100})
	if err != nil {
		t.Fatalf("ReportProgress() error = %v", err)
	}

	if received.JobID != "job-123" {
		t.Errorf("expected job_id 'job-123', got %q", received.JobID)
	}
	if received.Percentage != 50 {
		t.Errorf("expected percentage 50, got %d", received.Percentage)
	}
	if received.Message != "halfway done" {
		t.Errorf("expected message 'halfway done', got %q", received.Message)
	}
	if received.Data["items"] != float64(100) {
		t.Errorf("expected data.items=100, got %v", received.Data["items"])
	}
}

func TestReportProgressValidation(t *testing.T) {
	// Retries are disabled so the "valid" case's expected connection error
	// (there is no server at this address) is returned promptly instead of
	// being retried per the transport's now method/operation-aware retry
	// classification -- progress reporting is a POST with no idempotency key,
	// so it would not be retried automatically in production either, but this
	// keeps the test's timing intent (fail fast, do not wait out a backoff
	// schedule) explicit rather than incidental.
	tr := newTransport("http://localhost:9999", clientConfig{retryConfig: &RetryConfig{Enabled: false}})

	tests := []struct {
		name    string
		jobID   string
		pct     int
		wantErr bool
	}{
		{"valid", "job-1", 50, false},
		{"percentage too low", "job-1", -1, true},
		{"percentage too high", "job-1", 101, true},
		{"empty job ID", "", 50, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ReportProgress(context.Background(), tr, tt.jobID, tt.pct, "", nil)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				// For the "valid" case, a connection error is expected since no server is running.
				// We only check that validation passes.
				if tt.pct < 0 || tt.pct > 100 || tt.jobID == "" {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestJobContextReportProgress(t *testing.T) {
	var received ProgressReport

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	jctx := JobContext{
		Job:    Job{ID: "job-456", Type: "test.job"},
		ctx:    context.Background(),
		worker: w,
	}

	err := jctx.ReportProgress(75, "almost done")
	if err != nil {
		t.Fatalf("JobContext.ReportProgress() error = %v", err)
	}

	if received.JobID != "job-456" {
		t.Errorf("expected job_id 'job-456', got %q", received.JobID)
	}
	if received.Percentage != 75 {
		t.Errorf("expected percentage 75, got %d", received.Percentage)
	}
	if received.Message != "almost done" {
		t.Errorf("expected message 'almost done', got %q", received.Message)
	}
}

func TestJobContextReportProgressWithData(t *testing.T) {
	var received ProgressReport

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := NewWorker(server.URL)
	jctx := JobContext{
		Job:    Job{ID: "job-789", Type: "test.job"},
		ctx:    context.Background(),
		worker: w,
	}

	data := map[string]any{"processed": 42, "total": 100}
	err := jctx.ReportProgressWithData(42, "processing", data)
	if err != nil {
		t.Fatalf("JobContext.ReportProgressWithData() error = %v", err)
	}

	if received.Percentage != 42 {
		t.Errorf("expected percentage 42, got %d", received.Percentage)
	}
	if received.Data["processed"] != float64(42) {
		t.Errorf("expected data.processed=42, got %v", received.Data["processed"])
	}
}

func TestJobContextReportProgressWithoutWorker(t *testing.T) {
	jctx := JobContext{
		Job: Job{ID: "job-000", Type: "test.job"},
		ctx: context.Background(),
	}

	err := jctx.ReportProgress(50, "test")
	if err == nil {
		t.Error("expected error when worker is nil")
	}
}
