package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAgentClient(t *testing.T) {
	c, err := NewAgentClient("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewAgentClientInvalidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"bad scheme", "ftp://example.com"},
		{"no scheme", "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAgentClient(tt.url)
			if err == nil {
				t.Fatal("expected error for invalid URL")
			}
		})
	}
}

func TestFork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/agent/jobs/job-1/fork" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body ForkOptions
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body.AtTurn != 3 {
			t.Errorf("expected at_turn=3, got %d", body.AtTurn)
		}
		if body.BranchName != "experiment" {
			t.Errorf("expected branch_name=experiment, got %s", body.BranchName)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ForkResult{
			BranchID:  "br-123",
			ContentID: "sha-abc",
		})
	}))
	defer server.Close()

	client, err := NewAgentClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res, err := client.Fork(context.Background(), "job-1", ForkOptions{
		AtTurn:     3,
		BranchName: "experiment",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BranchID != "br-123" {
		t.Errorf("expected branch_id=br-123, got %s", res.BranchID)
	}
	if res.ContentID != "sha-abc" {
		t.Errorf("expected content_id=sha-abc, got %s", res.ContentID)
	}
}

func TestMerge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/agent/jobs/job-2/merge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body MergeOptions
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body.BranchA != "br-1" || body.BranchB != "br-2" {
			t.Errorf("unexpected branches: %s, %s", body.BranchA, body.BranchB)
		}
		if body.Strategy != MergeOurs {
			t.Errorf("expected strategy=ours, got %s", body.Strategy)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MergeResult{
			MergedID:  "merged-99",
			Conflicts: []string{"turn-5"},
		})
	}))
	defer server.Close()

	client, err := NewAgentClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res, err := client.Merge(context.Background(), "job-2", MergeOptions{
		BranchA:  "br-1",
		BranchB:  "br-2",
		Strategy: MergeOurs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MergedID != "merged-99" {
		t.Errorf("expected merged_id=merged-99, got %s", res.MergedID)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0] != "turn-5" {
		t.Errorf("unexpected conflicts: %v", res.Conflicts)
	}
}

func TestPause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/agent/jobs/job-3/pause" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.Reason != "human review needed" {
			t.Errorf("expected reason='human review needed', got %q", body.Reason)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewAgentClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Pause(context.Background(), "job-3", "human review needed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/agent/jobs/job-4/resume" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body ResumeDecision
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if !body.Approved {
			t.Error("expected approved=true")
		}
		if body.Comment != "looks good" {
			t.Errorf("expected comment='looks good', got %q", body.Comment)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewAgentClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.Resume(context.Background(), "job-4", ResumeDecision{
		Approved: true,
		Comment:  "looks good",
		Metadata: map[string]any{"reviewer": "alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForkNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewAgentClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Fork(context.Background(), "missing-job", ForkOptions{})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestMergeConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client, _ := NewAgentClient(server.URL)
	_, err := client.Merge(context.Background(), "j1", MergeOptions{
		BranchA:  "main",
		BranchB:  "feature",
		Strategy: MergeOurs,
	})
	if !errors.Is(err, ErrBranchConflict) {
		t.Fatalf("expected ErrBranchConflict, got %v", err)
	}
}

func TestResumeNotPaused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	client, _ := NewAgentClient(server.URL)
	err := client.Resume(context.Background(), "j1", ResumeDecision{Approved: true})
	if !errors.Is(err, ErrAgentNotPaused) {
		t.Fatalf("expected ErrAgentNotPaused, got %v", err)
	}
}

func TestReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(ReplayResult{
			Steps: 10,
			Divergences: []Divergence{
				{Turn: 3, Expected: "ok", Actual: "err"},
			},
		})
	}))
	defer server.Close()

	client, _ := NewAgentClient(server.URL)
	res, err := client.Replay(context.Background(), "j1", ReplayOptions{FromTurn: 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps != 10 {
		t.Errorf("Steps = %d, want 10", res.Steps)
	}
	if len(res.Divergences) != 1 {
		t.Fatalf("Divergences = %d, want 1", len(res.Divergences))
	}
	if res.Divergences[0].Turn != 3 {
		t.Errorf("Divergence Turn = %d, want 3", res.Divergences[0].Turn)
	}
}

func TestServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := NewAgentClient(server.URL)
	err := client.Pause(context.Background(), "j1", "test")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer server.Close()

	client, _ := NewAgentClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.Pause(ctx, "j1", "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWithHTTPClient(t *testing.T) {
	client, err := NewAgentClient("http://localhost:8080", WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatal(err)
	}
	_ = client
}
