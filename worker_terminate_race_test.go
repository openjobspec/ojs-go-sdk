package ojs

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file proves terminate is genuinely absorbing under concurrency: no
// number of racing quiet/running server directives, nor a racing local
// shutdown call, can ever move workerLifecycle back out of terminate once any
// one of them has claimed it. Before the mutex-guarded rewrite in
// worker_state.go, set and applyServerDirective each read the current state
// and later wrote a new one with no shared critical section between the two
// steps, so a directive that read the state as "running" and decided "quiet"
// was a legal transition could still write "quiet" after a concurrent local
// shutdown call had already entered "terminate" -- silently reverting it.

// TestWorkerLifecycleTerminateAbsorbsConcurrentDirectives hammers a single
// workerLifecycle with many goroutines racing applyServerDirective(quiet),
// applyServerDirective(running), and the local set(terminate) that a real
// shutdown issues, repeated across many iterations to maximize the chance any
// reintroduced check-then-act race is caught -- especially under `-race`,
// which instruments every one of these iterations for low-level memory races
// on top of the higher-level logical race this test targets.
func TestWorkerLifecycleTerminateAbsorbsConcurrentDirectives(t *testing.T) {
	const iterations = 500
	const goroutinesPerIteration = 60

	for iter := 0; iter < iterations; iter++ {
		l := newWorkerLifecycle()

		var ready sync.WaitGroup
		var start sync.WaitGroup
		ready.Add(goroutinesPerIteration)
		start.Add(1)

		var wg sync.WaitGroup
		for g := 0; g < goroutinesPerIteration; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				ready.Done()
				start.Wait() // maximize contention: every goroutine starts together

				switch g % 3 {
				case 0:
					l.applyServerDirective(WorkerStateQuiet)
				case 1:
					l.applyServerDirective(WorkerStateRunning)
				case 2:
					l.set(WorkerStateTerminate)
				}
			}(g)
		}

		ready.Wait()
		start.Done()
		wg.Wait()

		if got := l.current(); got != WorkerStateTerminate {
			t.Fatalf("iteration %d: state = %s, want terminate (terminate did not absorb a concurrent directive)", iter, got)
		}
		select {
		case <-l.terminated():
		default:
			t.Fatalf("iteration %d: terminated() channel not closed despite state=terminate", iter)
		}
	}
}

// TestWorkerLifecycleTerminateAbsorbsDirectivesArrivingAfter proves the other
// half: once terminate has already been reached, every later directive --
// arbitrarily many of them, from arbitrarily many goroutines -- is rejected
// and the state never moves away from terminate again.
func TestWorkerLifecycleTerminateAbsorbsDirectivesArrivingAfter(t *testing.T) {
	l := newWorkerLifecycle()
	l.set(WorkerStateTerminate)

	const goroutines = 200
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var ok bool
			switch g % 2 {
			case 0:
				ok = l.applyServerDirective(WorkerStateQuiet)
			case 1:
				ok = l.applyServerDirective(WorkerStateRunning)
			}
			if ok {
				accepted.Add(1)
			}
		}(g)
	}
	wg.Wait()

	if n := accepted.Load(); n != 0 {
		t.Fatalf("%d directives were accepted after terminate; want 0", n)
	}
	if got := l.current(); got != WorkerStateTerminate {
		t.Fatalf("state = %s, want terminate", got)
	}
}

// TestWorkerLifecycleConcurrentSetTerminateIsIdempotent proves many
// goroutines racing to be the one that enters terminate never panic (a
// sync.Once double-close would) and never leave the state anywhere but
// terminate.
func TestWorkerLifecycleConcurrentSetTerminateIsIdempotent(t *testing.T) {
	l := newWorkerLifecycle()
	const goroutines = 300
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			l.set(WorkerStateTerminate)
		}()
	}
	wg.Wait()

	if got := l.current(); got != WorkerStateTerminate {
		t.Fatalf("state = %s, want terminate", got)
	}
	select {
	case <-l.terminated():
	default:
		t.Fatal("terminated() channel not closed")
	}
}

// TestWorkerLifecycleNeverObservesTerminateThenNonTerminate drives many
// interleavings of applyServerDirective(quiet/running) against a single
// set(terminate) arriving partway through a burst, and asserts a stronger
// property than the final state alone: current() must never be observed
// transitioning FROM terminate TO anything else at any point during the
// race, by continuously sampling it from a separate goroutine while the
// storm runs.
func TestWorkerLifecycleNeverObservesTerminateThenNonTerminate(t *testing.T) {
	const iterations = 100
	for iter := 0; iter < iterations; iter++ {
		l := newWorkerLifecycle()

		stopSampling := make(chan struct{})
		var sawTerminate atomic.Bool
		var regressed atomic.Bool
		var samplerDone sync.WaitGroup
		samplerDone.Add(1)
		go func() {
			defer samplerDone.Done()
			for {
				select {
				case <-stopSampling:
					return
				default:
				}
				if l.current() == WorkerStateTerminate {
					sawTerminate.Store(true)
				} else if sawTerminate.Load() {
					// Once terminate was observed, no later sample may be
					// anything else.
					regressed.Store(true)
				}
			}
		}()

		var wg sync.WaitGroup
		const goroutines = 40
		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			go func(g int) {
				defer wg.Done()
				switch g % 3 {
				case 0:
					l.applyServerDirective(WorkerStateQuiet)
				case 1:
					l.applyServerDirective(WorkerStateRunning)
				case 2:
					l.set(WorkerStateTerminate)
				}
			}(g)
		}
		wg.Wait()
		close(stopSampling)
		samplerDone.Wait()

		if regressed.Load() {
			t.Fatalf("iteration %d: observed the lifecycle move away from terminate after reaching it", iter)
		}
		if got := l.current(); got != WorkerStateTerminate {
			t.Fatalf("iteration %d: final state = %s, want terminate", iter, got)
		}
	}
}

// --- End-to-end: a real Worker.Start under a heartbeat storm of directives ---

// TestWorkerStateNeverRegressesUnderDirectiveStorm drives a real worker
// through Start with a heartbeat interval fast enough to fire many times
// before the grace period elapses, alternating quiet/running directives
// before eventually terminating, while State() is polled concurrently from
// the test goroutine. The observed state must never regress from terminate
// once reached, and Start must return.
func TestWorkerStateNeverRegressesUnderDirectiveStorm(t *testing.T) {
	var heartbeatCount atomic.Int64
	s := &workerTestServer{directive: func(n int) string {
		heartbeatCount.Store(int64(n))
		switch {
		case n < 5:
			return "quiet"
		case n < 10:
			return "running"
		default:
			return "terminate"
		}
	}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL,
		WithPollInterval(2*time.Millisecond),
		WithHeartbeatInterval(2*time.Millisecond),
		WithGracePeriod(2*time.Second),
	)
	w.Register("noop", func(JobContext) error { return nil })

	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	stopSampling := make(chan struct{})
	var sawTerminate atomic.Bool
	var regressed atomic.Bool
	var samplerDone sync.WaitGroup
	samplerDone.Add(1)
	go func() {
		defer samplerDone.Done()
		for {
			select {
			case <-stopSampling:
				return
			default:
			}
			if w.State() == WorkerStateTerminate {
				sawTerminate.Store(true)
			} else if sawTerminate.Load() {
				regressed.Store(true)
			}
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		close(stopSampling)
		samplerDone.Wait()
		t.Fatal("Start() did not return after the directive storm settled on terminate")
	}
	close(stopSampling)
	samplerDone.Wait()

	if regressed.Load() {
		t.Fatal("State() was observed moving away from terminate after reaching it")
	}
	if got := w.State(); got != WorkerStateTerminate {
		t.Fatalf("final State() = %s, want terminate", got)
	}
}
