package ojs

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Terminate signalling (worker_state.go) ---

func TestLifecycleTerminateSignalRaisedOnLocalShutdown(t *testing.T) {
	l := newWorkerLifecycle()
	select {
	case <-l.terminated():
		t.Fatal("a running worker must not report terminate")
	default:
	}

	l.set(WorkerStateTerminate)
	select {
	case <-l.terminated():
	default:
		t.Fatal("set(terminate) must raise the terminate signal")
	}
}

func TestLifecycleTerminateSignalRaisedByServerDirective(t *testing.T) {
	l := newWorkerLifecycle()
	if !l.applyServerDirective(WorkerStateTerminate) {
		t.Fatal("running -> terminate must be accepted")
	}
	select {
	case <-l.terminated():
	default:
		t.Fatal("an accepted terminate directive must raise the terminate signal")
	}
}

func TestLifecycleQuietDirectiveDoesNotSignalTerminate(t *testing.T) {
	l := newWorkerLifecycle()
	if !l.applyServerDirective(WorkerStateQuiet) {
		t.Fatal("running -> quiet must be accepted")
	}
	select {
	case <-l.terminated():
		t.Fatal("quiet is a fetch-stop state and must not trigger shutdown")
	default:
	}

	// quiet -> terminate is still reachable and does signal.
	if !l.applyServerDirective(WorkerStateTerminate) {
		t.Fatal("quiet -> terminate must be accepted")
	}
	select {
	case <-l.terminated():
	default:
		t.Fatal("quiet -> terminate must raise the terminate signal")
	}
}

func TestLifecycleTerminateSignalIsIdempotent(t *testing.T) {
	l := newWorkerLifecycle()
	l.set(WorkerStateTerminate)
	// Repeated entries must not close the channel twice (which would panic),
	// and a rejected backward directive must not reopen it.
	l.set(WorkerStateTerminate)
	if l.applyServerDirective(WorkerStateRunning) {
		t.Fatal("terminate is absorbing")
	}
	select {
	case <-l.terminated():
	default:
		t.Fatal("terminate signal must stay raised")
	}
}

// --- Server-directed terminate through Start ---

// TestWorkerTerminateDirectiveStopsStart locks the core of the worker protocol
// terminate directive: a heartbeat response of "terminate" must shut the worker
// down on its own, without the caller cancelling anything.
func TestWorkerTerminateDirectiveStopsStart(t *testing.T) {
	s := &workerTestServer{directive: func(int) string { return "terminate" }}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(10*time.Millisecond),
		WithGracePeriod(2*time.Second),
	)
	w.Register("noop", func(JobContext) error { return nil })

	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after a terminate directive")
	}

	if w.State() != WorkerStateTerminate {
		t.Errorf("State() = %s, want terminate", w.State())
	}
}

// TestWorkerTerminateDirectiveTakesTheCancellationDrainPath proves the directive
// enters the *same* graceful path as caller cancellation: the handler context is
// cancelled, and the job that finishes during the drain is still ACKed on the
// shutdown-surviving reporting context.
func TestWorkerTerminateDirectiveTakesTheCancellationDrainPath(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-term", Type: "slow.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	started := make(chan struct{})
	var handlerSawCancel atomic.Bool

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(10*time.Millisecond),
		WithGracePeriod(5*time.Second),
	)
	var once sync.Once
	w.Register("slow.job", func(jc JobContext) error {
		once.Do(func() { close(started) })
		<-jc.Context().Done()
		handlerSawCancel.Store(true)
		return nil
	})

	// Only direct terminate once a job is in flight, so the drain has work to do.
	s.mu.Lock()
	s.directive = func(int) string {
		select {
		case <-started:
			return "terminate"
		default:
			return "running"
		}
	}
	s.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after a terminate directive")
	}

	if !handlerSawCancel.Load() {
		t.Error("a server-directed terminate must cancel the handler context, exactly as caller cancellation does")
	}
	if acked := s.ackedIDs(); len(acked) != 1 || acked[0] != "job-term" {
		t.Fatalf("acked = %v, want [job-term]: the drain path must still report outcomes", acked)
	}
	if nacks := s.nackList(); len(nacks) != 0 {
		t.Fatalf("nacks = %+v, want none", nacks)
	}
}

// TestWorkerTerminateDirectiveForcesNackAtGraceExpiry covers the other half of
// the shared path: a handler that outlives the grace period is NACKed for
// rescheduling, whether shutdown began locally or at the server's direction.
func TestWorkerTerminateDirectiveForcesNackAtGraceExpiry(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-term-stuck", Type: "stuck.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(10*time.Millisecond),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("stuck.job", func(JobContext) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	})

	s.mu.Lock()
	s.directive = func(int) string {
		select {
		case <-started:
			return "terminate"
		default:
			return "running"
		}
	}
	s.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after the grace period")
	}

	nacks := s.nackList()
	if len(nacks) != 1 || nacks[0].JobID != "job-term-stuck" {
		t.Fatalf("nacks = %+v, want exactly one for job-term-stuck", nacks)
	}
	if !nacks[0].Error.Retryable || nacks[0].Error.Code != "worker_shutdown" {
		t.Errorf("nack = %+v, want retryable worker_shutdown", nacks[0].Error)
	}
}

// TestWorkerQuietDirectiveStopsFetchingOnly locks that quiet is not a shutdown:
// the worker stops fetching and keeps running until the caller says otherwise.
func TestWorkerQuietDirectiveStopsFetchingOnly(t *testing.T) {
	s := &workerTestServer{directive: func(int) string { return "quiet" }}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(10*time.Millisecond),
		WithGracePeriod(2*time.Second),
	)
	w.Register("noop", func(JobContext) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	// Wait for the directive to be applied.
	deadline := time.Now().Add(3 * time.Second)
	for w.State() != WorkerStateQuiet {
		if time.Now().After(deadline) {
			t.Fatalf("State() = %s, want quiet", w.State())
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("Start() returned %v while quiet; quiet must not shut the worker down", err)
	case <-time.After(150 * time.Millisecond):
	}

	// Fetching must have stopped.
	before := s.fetchCount()
	time.Sleep(150 * time.Millisecond)
	if after := s.fetchCount(); after != before {
		t.Errorf("fetches went %d -> %d while quiet; a quiet worker must not fetch", before, after)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after cancellation")
	}
}
