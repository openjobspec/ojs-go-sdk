package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// --- Lifecycle state machine (worker_state.go) ---

func TestWorkerLifecycleTransitions(t *testing.T) {
	cases := []struct {
		from, to WorkerState
		want     bool
	}{
		{WorkerStateRunning, WorkerStateQuiet, true},
		{WorkerStateRunning, WorkerStateTerminate, true},
		{WorkerStateRunning, WorkerStateRunning, false},
		{WorkerStateQuiet, WorkerStateRunning, true},
		{WorkerStateQuiet, WorkerStateTerminate, true},
		{WorkerStateQuiet, WorkerStateQuiet, false},
		// terminate is absorbing.
		{WorkerStateTerminate, WorkerStateRunning, false},
		{WorkerStateTerminate, WorkerStateQuiet, false},
		{WorkerStateTerminate, WorkerStateTerminate, false},
	}
	for _, tc := range cases {
		l := newWorkerLifecycle()
		l.set(tc.from)
		if got := l.applyServerDirective(tc.to); got != tc.want {
			t.Errorf("%s -> %s: applyServerDirective = %v, want %v", tc.from, tc.to, got, tc.want)
		}
		wantState := tc.from
		if tc.want {
			wantState = tc.to
		}
		if l.current() != wantState {
			t.Errorf("%s -> %s: state = %s, want %s", tc.from, tc.to, l.current(), wantState)
		}
	}
}

func TestWorkerLifecycleUnknownDirectiveIgnored(t *testing.T) {
	l := newWorkerLifecycle()
	if l.applyServerDirective(WorkerState("bogus")) {
		t.Error("unknown server directive should be ignored")
	}
	if l.current() != WorkerStateRunning {
		t.Errorf("state = %s, want running", l.current())
	}
}

// --- Active job set (worker_activejobs.go) ---

func TestActiveJobSetIDsAreSorted(t *testing.T) {
	s := newActiveJobSet()
	for _, id := range []string{"job-c", "job-a", "job-b"} {
		s.add(id)
	}
	got := s.idsSnapshot()
	want := []string{"job-a", "job-b", "job-c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (stable order required for heartbeat payloads)", got, want)
		}
	}
}

func TestActiveJobSetCountAndRemove(t *testing.T) {
	s := newActiveJobSet()
	if s.count() != 0 {
		t.Fatalf("count = %d, want 0", s.count())
	}
	s.add("a")
	s.add("a") // idempotent
	if s.count() != 1 {
		t.Errorf("count = %d, want 1", s.count())
	}
	s.remove("missing") // no-op
	s.remove("a")
	if s.count() != 0 {
		t.Errorf("count = %d, want 0 after remove", s.count())
	}
}

func TestActiveJobSetDrainSignalling(t *testing.T) {
	s := newActiveJobSet()

	// Starts drained.
	select {
	case <-s.drained():
	default:
		t.Fatal("empty set should report drained")
	}

	s.add("a")
	select {
	case <-s.drained():
		t.Fatal("non-empty set must not report drained")
	default:
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		s.remove("a")
	}()

	never := make(chan struct{})
	if !s.waitDrained(context.Background(), never) {
		t.Fatal("waitDrained should report true once the set empties")
	}
}

func TestActiveJobSetWaitDrainedRespectsTimeout(t *testing.T) {
	s := newActiveJobSet()
	s.add("stuck")

	expired := make(chan struct{})
	close(expired)

	if s.waitDrained(context.Background(), expired) {
		t.Error("waitDrained should report false when the timeout fires first")
	}
}

func TestActiveJobSetConcurrentUse(t *testing.T) {
	s := newActiveJobSet()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			s.add(id)
			_ = s.idsSnapshot()
			_ = s.count()
			s.remove(id)
		}(i)
	}
	wg.Wait()
	if s.count() != 0 {
		t.Errorf("count = %d, want 0", s.count())
	}
}

// --- Start re-entrancy ---

func TestWorkerStartTwiceReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}, "state": "running"})
	}))
	defer srv.Close()

	w := NewWorker(srv.URL, WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(10*time.Millisecond), WithGracePeriod(50*time.Millisecond))
	w.Register("t", func(JobContext) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)
	if err := w.Start(context.Background()); err != ErrWorkerAlreadyStarted {
		t.Errorf("second Start() = %v, want ErrWorkerAlreadyStarted", err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("first Start() = %v, want nil", err)
	}
}

func TestWorkerStartWithoutHandlersFails(t *testing.T) {
	w := NewWorker("http://localhost:8080")
	if err := w.Start(context.Background()); err == nil {
		t.Error("Start() without handlers should fail")
	}
	// The failed Start must not consume the single-use latch.
	if w.started.Load() {
		t.Error("a rejected Start must not mark the worker as started")
	}
}
