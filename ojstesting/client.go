package ojstesting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// FakeClient creates an [ojs.Client] backed by the fake store.
// All Enqueue and EnqueueBatch calls are recorded in-memory and can be
// verified with [AssertEnqueued], [RefuteEnqueued], and [AllEnqueued].
// No real HTTP server is needed.
//
// FakeClient must be called after [Fake]:
//
//	func TestOrderFlow(t *testing.T) {
//	    store := ojstesting.Fake(t)
//	    client := ojstesting.FakeClient(t)
//	    // use client.Enqueue() in production code under test
//	    ojstesting.AssertEnqueued(t, "email.send")
//	}
func FakeClient(t *testing.T, opts ...ojs.ClientOption) *ojs.Client {
	t.Helper()
	s := mustStore(t)

	server := httptest.NewServer(fakeHandler(t, s))
	t.Cleanup(server.Close)

	client, err := ojs.NewClient(server.URL, opts...)
	if err != nil {
		t.Fatalf("ojstesting: FakeClient: %v", err)
	}
	return client
}

// fakeHandler returns an http.Handler that records enqueues in the fake store.
func fakeHandler(t *testing.T, s *FakeStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/openjobspec+json")

		switch r.URL.Path {
		case "/ojs/v1/jobs":
			handleEnqueue(w, r, s)
		case "/ojs/v1/jobs/batch":
			handleBatchEnqueue(w, r, s)
		default:
			// Return empty success for other endpoints (health, queues, etc.)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
	})
}

type fakeEnqueueRequest struct {
	Type    string         `json:"type"`
	Args    []any          `json:"args"`
	Meta    map[string]any `json:"meta,omitempty"`
	Options *struct {
		Queue string `json:"queue,omitempty"`
	} `json:"options,omitempty"`
}

func handleEnqueue(w http.ResponseWriter, r *http.Request, s *FakeStore) {
	var req fakeEnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "invalid_payload", "message": err.Error()},
		})
		return
	}

	queue := "default"
	if req.Options != nil && req.Options.Queue != "" {
		queue = req.Options.Queue
	}

	job := s.RecordEnqueue(req.Type, req.Args, queue, req.Meta)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"job": map[string]any{
			"id":           job.ID,
			"type":         job.Type,
			"state":        job.State,
			"args":         job.Args,
			"queue":        job.Queue,
			"attempt":      job.Attempt,
			"max_attempts":  3,
			"created_at":   job.CreatedAt,
		},
	})
}

func handleBatchEnqueue(w http.ResponseWriter, r *http.Request, s *FakeStore) {
	var req struct {
		Jobs []fakeEnqueueRequest `json:"jobs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "invalid_payload", "message": err.Error()},
		})
		return
	}

	var jobs []map[string]any
	for _, j := range req.Jobs {
		queue := "default"
		if j.Options != nil && j.Options.Queue != "" {
			queue = j.Options.Queue
		}
		job := s.RecordEnqueue(j.Type, j.Args, queue, j.Meta)
		jobs = append(jobs, map[string]any{
			"id":           job.ID,
			"type":         job.Type,
			"state":        job.State,
			"args":         job.Args,
			"queue":        job.Queue,
			"attempt":      job.Attempt,
			"max_attempts":  3,
			"created_at":   job.CreatedAt,
		})
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
}
