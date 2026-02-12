package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Dead Letter tests ---

func TestListDeadLetterJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/dead-letter" {
			t.Errorf("expected /ojs/v1/dead-letter, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", q.Get("limit"))
		}
		if q.Get("queue") != "email" {
			t.Errorf("expected queue=email, got %s", q.Get("queue"))
		}

		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"jobs": []map[string]any{
				{"id": "dead-1", "type": "email.send", "state": "discarded", "args": []any{}, "queue": "email"},
				{"id": "dead-2", "type": "email.send", "state": "discarded", "args": []any{}, "queue": "email"},
			},
			"pagination": map[string]any{
				"total": 2, "limit": 10, "offset": 0, "has_more": false,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobs, pagination, err := client.ListDeadLetterJobs(context.Background(), "email", 10, 0)
	if err != nil {
		t.Fatalf("ListDeadLetterJobs() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 dead letter jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "dead-1" {
		t.Errorf("expected first job ID=dead-1, got %s", jobs[0].ID)
	}
	if pagination.Total != 2 {
		t.Errorf("expected pagination total=2, got %d", pagination.Total)
	}
}

func TestListDeadLetterJobsNoQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("queue") != "" {
			t.Error("expected no queue parameter")
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"jobs":       []any{},
			"pagination": map[string]any{"total": 0, "limit": 10, "offset": 0, "has_more": false},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobs, _, err := client.ListDeadLetterJobs(context.Background(), "", 10, 0)
	if err != nil {
		t.Fatalf("ListDeadLetterJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestRetryDeadLetterJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/dead-letter/dead-1/retry" {
			t.Errorf("expected /ojs/v1/dead-letter/dead-1/retry, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id": "dead-1", "type": "email.send", "state": "available",
				"args": []any{map[string]any{"to": "user@example.com"}}, "queue": "email",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	job, err := client.RetryDeadLetterJob(context.Background(), "dead-1")
	if err != nil {
		t.Fatalf("RetryDeadLetterJob() error = %v", err)
	}
	if job.State != JobStateAvailable {
		t.Errorf("expected state available, got %s", job.State)
	}
}

func TestDiscardDeadLetterJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/dead-letter/dead-1" {
			t.Errorf("expected /ojs/v1/dead-letter/dead-1, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.DiscardDeadLetterJob(context.Background(), "dead-1")
	if err != nil {
		t.Fatalf("DiscardDeadLetterJob() error = %v", err)
	}
}

// --- Cron tests ---

func TestListCronJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/cron" {
			t.Errorf("expected /ojs/v1/cron, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"cron_jobs": []map[string]any{
				{
					"name": "daily-report", "cron": "0 9 * * *", "timezone": "UTC",
					"type": "report.generate", "args": []any{}, "status": "active",
				},
			},
			"pagination": map[string]any{"total": 1},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	cronJobs, err := client.ListCronJobs(context.Background())
	if err != nil {
		t.Fatalf("ListCronJobs() error = %v", err)
	}
	if len(cronJobs) != 1 {
		t.Fatalf("expected 1 cron job, got %d", len(cronJobs))
	}
	if cronJobs[0].Name != "daily-report" {
		t.Errorf("expected name=daily-report, got %s", cronJobs[0].Name)
	}
	if cronJobs[0].Cron != "0 9 * * *" {
		t.Errorf("expected cron=0 9 * * *, got %s", cronJobs[0].Cron)
	}
}

func TestRegisterCronJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/cron" {
			t.Errorf("expected /ojs/v1/cron, got %s", r.URL.Path)
		}

		var req cronJobRequestWire
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name != "nightly-cleanup" {
			t.Errorf("expected name=nightly-cleanup, got %s", req.Name)
		}
		if req.Cron != "0 2 * * *" {
			t.Errorf("expected cron=0 2 * * *, got %s", req.Cron)
		}
		if req.Type != "cleanup.run" {
			t.Errorf("expected type=cleanup.run, got %s", req.Type)
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"cron_job": map[string]any{
				"name": "nightly-cleanup", "cron": "0 2 * * *", "timezone": "UTC",
				"type": "cleanup.run", "args": []any{}, "status": "active",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	cronJob, err := client.RegisterCronJob(context.Background(), CronJobRequest{
		Name:     "nightly-cleanup",
		Cron:     "0 2 * * *",
		Timezone: "UTC",
		Type:     "cleanup.run",
		Args:     Args{"retention_days": 30},
		Options:  []EnqueueOption{WithQueue("maintenance")},
	})
	if err != nil {
		t.Fatalf("RegisterCronJob() error = %v", err)
	}
	if cronJob.Name != "nightly-cleanup" {
		t.Errorf("expected name=nightly-cleanup, got %s", cronJob.Name)
	}
}

func TestUnregisterCronJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/cron/nightly-cleanup" {
			t.Errorf("expected /ojs/v1/cron/nightly-cleanup, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.UnregisterCronJob(context.Background(), "nightly-cleanup")
	if err != nil {
		t.Fatalf("UnregisterCronJob() error = %v", err)
	}
}

// --- Manifest test ---

func TestManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ojs/manifest" {
			t.Errorf("expected /ojs/manifest, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"ojs_version": "1.0.0-rc.1",
			"implementation": map[string]any{
				"name": "ojs-backend-redis", "version": "0.1.0",
				"language": "go", "homepage": "https://openjobspec.org",
			},
			"conformance_level": 3,
			"protocols":         []any{"http"},
			"backend":           "redis",
			"capabilities": map[string]any{
				"workflows": true, "cron": true, "unique_jobs": true,
			},
			"extensions": []any{"retry", "workflows", "cron", "unique-jobs"},
			"endpoints": map[string]any{
				"jobs": "/ojs/v1/jobs", "workers": "/ojs/v1/workers",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	manifest, err := client.Manifest(context.Background())
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	if manifest.OJSVersion != "1.0.0-rc.1" {
		t.Errorf("expected ojs_version=1.0.0-rc.1, got %s", manifest.OJSVersion)
	}
	if manifest.Implementation.Name != "ojs-backend-redis" {
		t.Errorf("expected implementation name=ojs-backend-redis, got %s", manifest.Implementation.Name)
	}
	if manifest.ConformanceLevel != 3 {
		t.Errorf("expected conformance_level=3, got %d", manifest.ConformanceLevel)
	}
	if !manifest.Capabilities["workflows"] {
		t.Error("expected capabilities.workflows=true")
	}
	if len(manifest.Extensions) != 4 {
		t.Errorf("expected 4 extensions, got %d", len(manifest.Extensions))
	}
}
