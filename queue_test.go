package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListQueues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/queues" {
			t.Errorf("expected /ojs/v1/queues, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"queues": []map[string]any{
				{"name": "default", "status": "active", "created_at": "2026-01-01T00:00:00Z"},
				{"name": "email", "status": "active", "created_at": "2026-01-01T00:00:00Z"},
			},
			"pagination": map[string]any{
				"total": 2, "limit": 50, "offset": 0, "has_more": false,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	queues, err := client.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues() error = %v", err)
	}
	if len(queues) != 2 {
		t.Fatalf("expected 2 queues, got %d", len(queues))
	}
	if queues[0].Name != "default" {
		t.Errorf("expected first queue name=default, got %s", queues[0].Name)
	}
	if queues[1].Name != "email" {
		t.Errorf("expected second queue name=email, got %s", queues[1].Name)
	}
}

func TestGetQueueStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ojs/v1/queues/email/stats" {
			t.Errorf("expected /ojs/v1/queues/email/stats, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"queue":  "email",
			"status": "active",
			"stats": map[string]any{
				"available":           15,
				"active":              3,
				"scheduled":           2,
				"retryable":           1,
				"discarded":           0,
				"completed_last_hour": 120,
				"failed_last_hour":    5,
				"avg_duration_ms":     245.5,
				"avg_wait_ms":         12.3,
				"throughput_per_second": 2.0,
			},
			"computed_at": "2026-02-12T10:00:00Z",
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	stats, err := client.GetQueueStats(context.Background(), "email")
	if err != nil {
		t.Fatalf("GetQueueStats() error = %v", err)
	}
	if stats.Queue != "email" {
		t.Errorf("expected queue=email, got %s", stats.Queue)
	}
	if stats.Available != 15 {
		t.Errorf("expected available=15, got %d", stats.Available)
	}
	if stats.Active != 3 {
		t.Errorf("expected active=3, got %d", stats.Active)
	}
	if stats.CompletedLastHour != 120 {
		t.Errorf("expected completed_last_hour=120, got %d", stats.CompletedLastHour)
	}
	if stats.ThroughputPerSec != 2.0 {
		t.Errorf("expected throughput=2.0, got %f", stats.ThroughputPerSec)
	}
}

func TestPauseQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/queues/email/pause" {
			t.Errorf("expected /ojs/v1/queues/email/pause, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.PauseQueue(context.Background(), "email")
	if err != nil {
		t.Fatalf("PauseQueue() error = %v", err)
	}
}

func TestResumeQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/queues/email/resume" {
			t.Errorf("expected /ojs/v1/queues/email/resume, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.ResumeQueue(context.Background(), "email")
	if err != nil {
		t.Fatalf("ResumeQueue() error = %v", err)
	}
}

func TestPauseQueueError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "not_found",
				"message": "Queue 'nonexistent' not found.",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.PauseQueue(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent queue")
	}
}
