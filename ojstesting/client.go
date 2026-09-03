package ojstesting

import (
	"bytes"
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
			handleEnqueue(t, w, r, s)
		case "/ojs/v1/jobs/batch":
			handleBatchEnqueue(t, w, r, s)
		default:
			// Return empty success for other endpoints (health, queues, etc.)
			writeJSON(t, w, http.StatusOK, map[string]any{"status": "ok"})
		}
	})
}

// writeJSON serialises payload and writes it with the given status.
//
// The response is encoded before the status line is committed so an encoding
// failure can still be reported as a 500 instead of an empty 2xx body: a test
// double that silently truncates its response makes the test under it fail with
// an unrelated decode error somewhere else. The failure is also surfaced on t,
// because it is a defect in the fake, not in the code under test.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		t.Errorf("ojstesting: encoding %d response: %v", status, err)
		http.Error(w, "ojstesting: response encoding failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Errorf("ojstesting: writing %d response: %v", status, err)
	}
}

type fakeEnqueueRequest struct {
	Type    string              `json:"type"`
	Args    []any               `json:"args"`
	Meta    map[string]any      `json:"meta,omitempty"`
	Options *fakeEnqueueOptions `json:"options,omitempty"`
}

// fakeEnqueueOptions is the subset of the OJS enqueue options object the fake
// interprets. Named rather than anonymous so it can be constructed directly in
// this package's own tests.
type fakeEnqueueOptions struct {
	Queue string `json:"queue,omitempty"`
}

func handleEnqueue(t *testing.T, w http.ResponseWriter, r *http.Request, s *FakeStore) {
	t.Helper()

	var req fakeEnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": "invalid_payload", "message": err.Error()},
		})
		return
	}

	job := s.RecordEnqueue(req.Type, req.Args, req.queueOrDefault(), req.Meta)

	writeJSON(t, w, http.StatusCreated, map[string]any{
		"job": jobResponse(&job),
	})
}

// queueOrDefault resolves the queue an enqueue request targets, applying the
// OJS default when the request carries no queue override.
func (r fakeEnqueueRequest) queueOrDefault() string {
	if r.Options != nil && r.Options.Queue != "" {
		return r.Options.Queue
	}
	return "default"
}

// jobResponse renders a recorded job in the OJS job wire shape.
func jobResponse(job *FakeJob) map[string]any {
	return map[string]any{
		"id":           job.ID,
		"type":         job.Type,
		"state":        job.State,
		"args":         job.Args,
		"queue":        job.Queue,
		"attempt":      job.Attempt,
		"max_attempts": 3,
		"created_at":   job.CreatedAt,
	}
}

func handleBatchEnqueue(t *testing.T, w http.ResponseWriter, r *http.Request, s *FakeStore) {
	t.Helper()

	var req struct {
		Jobs []fakeEnqueueRequest `json:"jobs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": "invalid_payload", "message": err.Error()},
		})
		return
	}

	var jobs []map[string]any
	for i := range req.Jobs {
		j := &req.Jobs[i]
		job := s.RecordEnqueue(j.Type, j.Args, j.queueOrDefault(), j.Meta)
		jobs = append(jobs, jobResponse(&job))
	}

	writeJSON(t, w, http.StatusCreated, map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
}
