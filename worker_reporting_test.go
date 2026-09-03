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

// --- reportGuard (worker_activejobs.go) ---

func TestReportGuardClaimsOnce(t *testing.T) {
	g := &reportGuard{}
	if !g.claim() {
		t.Fatal("first claim must win")
	}
	if g.claim() {
		t.Fatal("second claim must lose: a job reports exactly one terminal outcome")
	}
}

func TestReportGuardNilIsUnguarded(t *testing.T) {
	var g *reportGuard
	for i := 0; i < 2; i++ {
		if !g.claim() {
			t.Fatalf("claim %d: a nil guard must not suppress reporting for unregistered job executions", i)
		}
	}
}

func TestReportGuardConcurrentClaimsElectOneWinner(t *testing.T) {
	g := &reportGuard{}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g.claim() {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("winners = %d, want exactly 1", got)
	}
}

func TestActiveJobSetClaimUnreportedSkipsClaimedJobs(t *testing.T) {
	s := newActiveJobSet()
	handled := s.add("job-handled")
	s.add("job-stuck-b")
	s.add("job-stuck-a")

	// The handler of job-handled reached its outcome first.
	if !handled.claim() {
		t.Fatal("handler claim must win")
	}

	got := s.disableReportingAndClaimUnreported()
	want := []string{"job-stuck-a", "job-stuck-b"}
	if len(got) != len(want) {
		t.Fatalf("claimUnreported = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claimUnreported = %v, want %v (stable order)", got, want)
		}
	}

	// A second sweep claims nothing: every guard is spent.
	if again := s.disableReportingAndClaimUnreported(); len(again) != 0 {
		t.Fatalf("second sweep claimed %v, want none", again)
	}
}

func TestActiveJobSetAddIsIdempotentPerGuard(t *testing.T) {
	s := newActiveJobSet()
	first := s.add("job-1")
	second := s.add("job-1")
	if first != second {
		t.Fatal("re-adding a job must not hand out a second terminal-report licence")
	}
	if s.count() != 1 {
		t.Fatalf("count = %d, want 1", s.count())
	}
}

func TestActiveJobSetWaitDrainedForTimesOut(t *testing.T) {
	s := newActiveJobSet()
	s.add("stuck")
	if s.waitDrainedFor(context.Background(), 20*time.Millisecond) {
		t.Fatal("waitDrainedFor must report false when the grace period elapses first")
	}

	s.remove("stuck")
	if !s.waitDrainedFor(context.Background(), time.Second) {
		t.Fatal("waitDrainedFor must report true once the set is empty")
	}
}

func TestActiveJobSetWaitDrainedForRespectsContext(t *testing.T) {
	s := newActiveJobSet()
	s.add("stuck")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.waitDrainedFor(ctx, time.Hour) {
		t.Fatal("waitDrainedFor must report false when the context is cancelled")
	}
}

func waitForTestSignal(t *testing.T, signal <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}

func waitForWorkerResult(t *testing.T, done <-chan error, timeout time.Duration, message string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(timeout):
		t.Fatal(message)
	}
}

func assertWorkerStillRunning(t *testing.T, done <-chan error, duration time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("Start returned before the already-claimed report completed: %v", err)
	case <-time.After(duration):
	}
}

// --- Terminal reporting through the worker ---

// TestWorkerReportsExactlyOneOutcomePerJobOnSuccessAtDeadline covers the success
// side of the deadline: the handler finishes inside the grace period, so the
// server sees one ACK and never a shutdown NACK for the same job.
func TestWorkerReportsExactlyOneOutcomePerJobOnSuccessAtDeadline(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-deadline-ok", Type: "slow.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	started := make(chan struct{})
	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(3*time.Second),
	)
	var once sync.Once
	w.Register("slow.job", func(jc JobContext) error {
		once.Do(func() { close(started) })
		<-jc.Context().Done()
		// Finish well inside the grace period.
		time.Sleep(20 * time.Millisecond)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Start() did not return")
	}

	if acked := s.ackedIDs(); len(acked) != 1 || acked[0] != "job-deadline-ok" {
		t.Fatalf("acked = %v, want exactly [job-deadline-ok]", acked)
	}
	if nacks := s.nackList(); len(nacks) != 0 {
		t.Fatalf("nacks = %+v, want none: a job that ACKed must never also be NACKed", nacks)
	}
}

// TestWorkerReportsExactlyOneOutcomePerJobOnFailureAtDeadline covers the failure
// side: the handler outruns the grace period, the shutdown sweep NACKs the job,
// and the handler's later completion is discarded rather than ACKed on top of it.
func TestWorkerReportsExactlyOneOutcomePerJobOnFailureAtDeadline(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-deadline-late", Type: "late.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("late.job", func(JobContext) error {
		once.Do(func() { close(started) })
		<-release
		close(finished)
		return nil // succeeds, but far too late
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after the grace period")
	}

	nacks := s.nackList()
	if len(nacks) != 1 || nacks[0].JobID != "job-deadline-late" {
		t.Fatalf("nacks = %+v, want exactly one forced NACK", nacks)
	}

	// Let the handler complete after shutdown and give it room to try to ACK.
	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never finished")
	}
	time.Sleep(200 * time.Millisecond)

	if acked := s.ackedIDs(); len(acked) != 0 {
		t.Fatalf("acked = %v, want none: the forced NACK already claimed this job's one terminal outcome", acked)
	}
	if got := s.nackList(); len(got) != 1 {
		t.Fatalf("nacks = %+v, want exactly one after the handler completed", got)
	}
}

// TestWorkerGraceExpiryLetsAnAlreadyClaimedReportFinish locks the boundary
// between handler reporting and the forced sweep. Once the handler has claimed
// its ACK, grace expiry must not cancel that request or let Start return before
// the bounded report finishes.
func TestWorkerGraceExpiryLetsAnAlreadyClaimedReportFinish(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-claimed", Type: "fast.job", Attempt: 1}}}
	ackStarted := make(chan struct{})
	releaseAck := make(chan struct{})
	var ackOnce sync.Once

	baseHandler := s.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == basePath+"/workers/ack" {
			ackOnce.Do(func() { close(ackStarted) })
			select {
			case <-releaseAck:
			case <-r.Context().Done():
				return
			}
		}
		baseHandler(w, r)
	}))
	defer srv.Close()

	started := make(chan struct{})
	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("fast.job", func(jc JobContext) error {
		once.Do(func() { close(started) })
		<-jc.Context().Done()
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	waitForTestSignal(t, started, 3*time.Second, "handler never started")
	cancel()

	waitForTestSignal(t, ackStarted, 3*time.Second, "handler never claimed and started its ACK")
	assertWorkerStillRunning(t, done, 150*time.Millisecond)

	close(releaseAck)
	waitForWorkerResult(t, done, 3*time.Second, "Start did not return after the claimed ACK completed")

	if acked := s.ackedIDs(); len(acked) != 1 || acked[0] != "job-claimed" {
		t.Fatalf("acked = %v, want exactly [job-claimed]", acked)
	}
	if nacks := s.nackList(); len(nacks) != 0 {
		t.Fatalf("nacks = %+v, want none for an already-claimed ACK", nacks)
	}
}

// TestWorkerGraceExpiryDisablesLateHandlerClaims deterministically holds the
// active-set snapshot lock across grace expiry. A handler completing in that
// window must lose to the forced NACK rather than claim a cancelled reporting
// context and leave the job with no terminal outcome.
func TestWorkerGraceExpiryDisablesLateHandlerClaims(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-late-claim", Type: "late.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("late.job", func(JobContext) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}

	cancel()

	deadline := time.After(3 * time.Second)
	for w.State() != WorkerStateTerminate {
		select {
		case <-deadline:
			t.Fatal("worker never entered terminate")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// Let shutdown join the fetch loop and sample the active drain channel, then
	// hold the snapshot lock across the grace deadline.
	time.Sleep(20 * time.Millisecond)
	w.active.mu.Lock()
	time.Sleep(150 * time.Millisecond)
	close(release)
	w.active.mu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after the forced NACK")
	}

	if acked := s.ackedIDs(); len(acked) != 0 {
		t.Fatalf("acked = %v, want none: reporting was disabled before the late completion", acked)
	}
	nacks := s.nackList()
	if len(nacks) != 1 || nacks[0].JobID != "job-late-claim" {
		t.Fatalf("nacks = %+v, want exactly one forced NACK", nacks)
	}
}

// TestWorkerHandlerOutcomeAfterForcedNackIsDiscarded drives the exact race at
// the unit level: the shutdown sweep claims the job, then the handler goroutine
// reaches its outcome and must report nothing at all.
func TestWorkerHandlerOutcomeAfterForcedNackIsDiscarded(t *testing.T) {
	s := &workerTestServer{}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL)
	w.Register("t", func(JobContext) error { return nil })

	guard := w.active.add("job-raced")
	w.forceNackUnreported(context.Background())

	// The handler finishes afterwards with the same guard.
	w.processJob(context.Background(), context.Background(),
		&Job{ID: "job-raced", Type: "t", Attempt: 1}, guard)

	if acked := s.ackedIDs(); len(acked) != 0 {
		t.Fatalf("acked = %v, want none", acked)
	}
	if nacks := s.nackList(); len(nacks) != 1 || nacks[0].JobID != "job-raced" {
		t.Fatalf("nacks = %+v, want exactly the forced NACK", nacks)
	}
}

// TestWorkerUnhandledJobTypeClaimsItsTerminalReport locks the no-handler path
// into the same single-outcome rule.
func TestWorkerUnhandledJobTypeClaimsItsTerminalReport(t *testing.T) {
	s := &workerTestServer{}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL)
	w.Register("known", func(JobContext) error { return nil })

	guard := w.active.add("job-unknown")
	w.processJob(context.Background(), context.Background(),
		&Job{ID: "job-unknown", Type: "missing.type", Attempt: 1}, guard)

	nacks := s.nackList()
	if len(nacks) != 1 || nacks[0].Error.Retryable {
		t.Fatalf("nacks = %+v, want one non-retryable NACK", nacks)
	}

	// The guard is spent, so the shutdown sweep must not NACK it a second time.
	w.forceNackUnreported(context.Background())
	if got := s.nackList(); len(got) != 1 {
		t.Fatalf("nacks = %+v, want exactly one after the shutdown sweep", got)
	}
}

// --- Shutdown ordering (feature 3) ---

// TestWorkerDoesNotDispatchJobsFetchedBeforeStop is the fetched-but-undispatched
// barrier: jobs that were fetched but never got a concurrency slot before
// shutdown must not start, and must not be reported at all — they are left for
// the server to reclaim by visibility timeout, exactly like unfetched jobs.
func TestWorkerDoesNotDispatchJobsFetchedBeforeStop(t *testing.T) {
	s := &workerTestServer{jobs: []Job{
		{ID: "job-0", Type: "blocking.job", Attempt: 1},
		{ID: "job-1", Type: "blocking.job", Attempt: 1},
		{ID: "job-2", Type: "blocking.job", Attempt: 1},
	}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	var starts atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)

	w := NewWorker(srv.URL,
		WithConcurrency(1),
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("blocking.job", func(JobContext) error {
		starts.Add(1)
		once.Do(func() { close(started) })
		<-release
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return")
	}
	time.Sleep(100 * time.Millisecond)

	if got := starts.Load(); got != 1 {
		t.Fatalf("handler starts = %d, want 1: a job fetched but not dispatched must never run", got)
	}

	nacks := s.nackList()
	if len(nacks) != 1 || nacks[0].JobID != "job-0" {
		t.Fatalf("nacks = %+v, want exactly one for the dispatched job", nacks)
	}
	if acked := s.ackedIDs(); len(acked) != 0 {
		t.Fatalf("acked = %v, want none", acked)
	}
}

// TestWorkerStartWaitsForHandlerGoroutines locks the drain barrier: when the
// worker drains inside its grace period, Start must not return while a handler
// goroutine is still running.
func TestWorkerStartWaitsForHandlerGoroutines(t *testing.T) {
	jobs := make([]Job, 0, 8)
	for i := 0; i < 8; i++ {
		jobs = append(jobs, Job{ID: fmt.Sprintf("job-%d", i), Type: "drain.job", Attempt: 1})
	}
	s := &workerTestServer{jobs: jobs}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	var running, maxSeen atomic.Int32
	started := make(chan struct{})

	w := NewWorker(srv.URL,
		WithConcurrency(8),
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(5*time.Second),
	)
	var once sync.Once
	w.Register("drain.job", func(jc JobContext) error {
		n := running.Add(1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		once.Do(func() { close(started) })
		<-jc.Context().Done()
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	// Let all eight get dispatched.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Start() did not return")
	}

	if got := running.Load(); got != 0 {
		t.Fatalf("%d handler goroutines were still running when Start returned", got)
	}
	if maxSeen.Load() == 0 {
		t.Fatal("no handler ran")
	}
	if acked := s.ackedIDs(); len(acked) != int(maxSeen.Load()) {
		t.Fatalf("acked = %v, want one ACK per dispatched job (%d)", acked, maxSeen.Load())
	}
	if nacks := s.nackList(); len(nacks) != 0 {
		t.Fatalf("nacks = %+v, want none: every job drained in time", nacks)
	}
}

// TestWorkerShutdownIsRaceFree exercises the whole fetch/dispatch/drain/report
// sequence concurrently; it is meaningful under -race.
func TestWorkerShutdownIsRaceFree(t *testing.T) {
	jobs := make([]Job, 0, 20)
	for i := 0; i < 20; i++ {
		jobs = append(jobs, Job{ID: fmt.Sprintf("race-%d", i), Type: "race.job", Attempt: 1})
	}
	s := &workerTestServer{jobs: jobs}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL,
		WithConcurrency(8),
		WithPollInterval(time.Millisecond),
		WithHeartbeatInterval(2*time.Millisecond),
		WithGracePeriod(50*time.Millisecond),
	)
	w.Register("race.job", func(jc JobContext) error {
		select {
		case <-jc.Context().Done():
		case <-time.After(5 * time.Millisecond):
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	// Every dispatched job must be reported exactly once, never twice.
	seen := map[string]int{}
	for _, id := range s.ackedIDs() {
		seen[id]++
	}
	for _, n := range s.nackList() {
		seen[n.JobID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s reported %d times, want exactly 1", id, n)
		}
	}
}
