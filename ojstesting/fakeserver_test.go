package ojstesting

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The fake previously discarded the error from json.Encoder.Encode, so a
// response that failed to serialise reached the client as an empty 2xx body and
// surfaced as an unrelated decode error somewhere else. writeJSON commits the
// status only once the payload is known to serialise, and keeps the encoder's
// trailing newline so the bytes on the wire are unchanged for every payload that
// already worked.
func TestWriteJSONWritesStatusAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(t, rec, http.StatusCreated, map[string]any{"status": "ok"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got, want := rec.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestJobResponseShape(t *testing.T) {
	job := FakeJob{
		ID: "job-1", Type: "email.send", State: "available",
		Args: []any{map[string]any{"to": "a@example.com"}}, Queue: "email",
	}
	got := jobResponse(&job)

	for _, key := range []string{"id", "type", "state", "args", "queue", "attempt", "max_attempts", "created_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("jobResponse() is missing key %q", key)
		}
	}
	if got["id"] != "job-1" || got["queue"] != "email" {
		t.Errorf("jobResponse() = %v, want the recorded job's fields", got)
	}
}

func TestQueueOrDefault(t *testing.T) {
	cases := []struct {
		name string
		req  fakeEnqueueRequest
		want string
	}{
		{"no options object", fakeEnqueueRequest{}, "default"},
		{"empty queue", fakeEnqueueRequest{Options: &fakeEnqueueOptions{}}, "default"},
		{"explicit queue", fakeEnqueueRequest{Options: &fakeEnqueueOptions{Queue: "email"}}, "email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.queueOrDefault(); got != tc.want {
				t.Errorf("queueOrDefault() = %q, want %q", got, tc.want)
			}
		})
	}
}
