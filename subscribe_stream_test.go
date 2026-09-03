package ojs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sseServer streams a fixed script of SSE lines and records the requests it saw.
type sseServer struct {
	mu       sync.Mutex
	requests []*http.Request
	lines    []string
	hold     bool // keep the connection open after the script
	status   int
}

func (s *sseServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r)
		lines := append([]string(nil), s.lines...)
		hold, status := s.hold, s.status
		s.mu.Unlock()

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, line := range lines {
			fmt.Fprintf(w, "%s\n", line)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if hold {
			<-r.Context().Done()
		}
	}
}

func (s *sseServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *sseServer) lastRequest() *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return nil
	}
	return s.requests[len(s.requests)-1]
}

func TestSubscribeDeliversEvents(t *testing.T) {
	s := &sseServer{lines: []string{
		"event: job.completed", `data: {"id":"j1"}`, "",
		"event: job.failed", `data: {"id":"j2"}`, "",
	}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var mu sync.Mutex
	var got []Event
	done := make(chan struct{})
	sub, err := client.Subscribe(context.Background(), "queue:default", func(e Event) {
		mu.Lock()
		got = append(got, e)
		n := len(got)
		mu.Unlock()
		if n == 2 {
			close(done)
		}
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	if got[0].Type != "job.completed" || got[1].Type != "job.failed" {
		t.Errorf("event types = %q, %q", got[0].Type, got[1].Type)
	}
	if got[0].Source != "sse" {
		t.Errorf("source = %q, want sse", got[0].Source)
	}
}

func TestSubscribeSendsAuthAndAcceptHeaders(t *testing.T) {
	s := &sseServer{lines: []string{`data: {}`, ""}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL, WithAuthToken("tok-123"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sub, err := client.Subscribe(context.Background(), "job:j1", func(Event) {})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	deadline := time.Now().Add(2 * time.Second)
	for s.requestCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	req := s.lastRequest()
	if req == nil {
		t.Fatal("no request recorded")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := req.URL.Query().Get("channel"); got != "job:j1" {
		t.Errorf("channel = %q, want job:j1 (escaped on the wire, decoded by the server)", got)
	}
}

func TestSubscribeJobAndQueueChannelNames(t *testing.T) {
	s := &sseServer{lines: []string{`data: {}`, ""}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	cases := []struct {
		name string
		call func() (*Subscription, error)
		want string
	}{
		{"job", func() (*Subscription, error) {
			return client.SubscribeJob(context.Background(), "j-1", func(Event) {})
		}, "job:j-1"},
		{"queue", func() (*Subscription, error) {
			return client.SubscribeQueue(context.Background(), "email", func(Event) {})
		}, "queue:email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := s.requestCount()
			sub, err := tc.call()
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			defer sub.Cancel()
			deadline := time.Now().Add(2 * time.Second)
			for s.requestCount() == before && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if got := s.lastRequest().URL.Query().Get("channel"); got != tc.want {
				t.Errorf("channel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubscribeRejectsNonOKStatus(t *testing.T) {
	s := &sseServer{status: http.StatusForbidden}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Subscribe(context.Background(), "queue:default", func(Event) {}); err == nil {
		t.Error("Subscribe must fail when the server rejects the stream")
	}
}

// TestSubscriptionCancelStopsStream also covers Cancel being safe to call twice.
func TestSubscriptionCancelStopsStream(t *testing.T) {
	s := &sseServer{lines: []string{`data: {}`, ""}, hold: true}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sub, err := client.Subscribe(context.Background(), "queue:default", func(Event) {})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	done := make(chan struct{})
	go func() {
		sub.Cancel()
		sub.Cancel() // must be idempotent
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel() did not return")
	}
}

// TestSubscribeWithReconnectResumesAfterStreamEnd covers the reconnect loop and
// the Last-Event-ID resume header.
func TestSubscribeWithReconnectResumesAfterStreamEnd(t *testing.T) {
	s := &sseServer{lines: []string{"id: 42", `data: {"n":1}`, ""}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var events atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	sub := client.SubscribeWithReconnect(ctx, "queue:default", func(Event) {
		events.Add(1)
	})

	// The stream ends immediately, so the client must reconnect at least once.
	deadline := time.Now().Add(4 * time.Second)
	for s.requestCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.requestCount() < 2 {
		t.Fatalf("requests = %d, want the client to reconnect", s.requestCount())
	}
	if events.Load() == 0 {
		t.Error("no events delivered before reconnect")
	}
	if got := s.lastRequest().Header.Get("Last-Event-ID"); got != "42" {
		t.Errorf("Last-Event-ID = %q, want 42 on the resumed connection", got)
	}

	cancel()
	done := make(chan struct{})
	go func() { sub.Cancel(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnecting subscription did not stop")
	}
}

func TestSubscribeOnceReportsServerError(t *testing.T) {
	s := &sseServer{status: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.subscribeOnce(context.Background(), "queue:default", func(Event) {}, "")
	if err == nil {
		t.Error("subscribeOnce must report a non-200 response")
	}
}
