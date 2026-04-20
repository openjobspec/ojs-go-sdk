package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// workerTestServer is a minimal OJS worker-protocol server that records the
// bodies it receives on each endpoint.
type workerTestServer struct {
	mu         sync.Mutex
	acks       []ackRequest
	nacks      []nackRequest
	heartbeats []map[string]any
	fetched    bool
	fetches    int
	jobs       []Job

	// directive optionally overrides the lifecycle state returned by the
	// heartbeat endpoint. n is the 1-based heartbeat number. Returning "" keeps
	// the default "running".
	directive func(n int) string
}

func (s *workerTestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case basePath + "/workers/fetch":
			s.mu.Lock()
			s.fetches++
			var out []Job
			if !s.fetched {
				s.fetched = true
				out = s.jobs
			}
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"jobs": out})
		case basePath + "/workers/ack":
			var req ackRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.mu.Lock()
			s.acks = append(s.acks, req)
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case basePath + "/workers/nack":
			var req nackRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.mu.Lock()
			s.nacks = append(s.nacks, req)
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case basePath + "/workers/heartbeat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			s.mu.Lock()
			s.heartbeats = append(s.heartbeats, req)
			n := len(s.heartbeats)
			directive := s.directive
			s.mu.Unlock()
			state := "running"
			if directive != nil {
				if d := directive(n); d != "" {
					state = d
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"state": state})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (s *workerTestServer) ackedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.acks))
	for _, a := range s.acks {
		ids = append(ids, a.JobID)
	}
	return ids
}

func (s *workerTestServer) nackList() []nackRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]nackRequest(nil), s.nacks...)
}

func (s *workerTestServer) fetchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches
}

func (s *workerTestServer) heartbeatList() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.heartbeats...)
}

// TestWorkerAcksJobFinishingDuringGracefulDrain is a regression test: ACK and
// NACK must use a context that outlives the Start context. Previously they
// reused the cancelled context, so every job that completed during the drain
// phase failed to report and was silently re-run by the server.
func TestWorkerAcksJobFinishingDuringGracefulDrain(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-drain", Type: "slow.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	started := make(chan struct{})
	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(5*time.Second),
	)
	var once sync.Once
	w.Register("slow.job", func(jc JobContext) error {
		once.Do(func() { close(started) })
		// Run until the worker begins shutting down, then succeed.
		<-jc.Context().Done()
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
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return")
	}

	acked := s.ackedIDs()
	if len(acked) != 1 || acked[0] != "job-drain" {
		t.Fatalf("acked = %v, want [job-drain]; a job completing during drain must still be ACKed", acked)
	}
}

// TestWorkerNacksUnfinishedJobsWhenGraceExpires locks the grace-period path:
// jobs still running when the grace period ends are NACKed as retryable.
func TestWorkerNacksUnfinishedJobsWhenGraceExpires(t *testing.T) {
	s := &workerTestServer{jobs: []Job{{ID: "job-stuck", Type: "stuck.job", Attempt: 1}}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})

	w := NewWorker(srv.URL,
		WithPollInterval(10*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(100*time.Millisecond),
	)
	var once sync.Once
	w.Register("stuck.job", func(jc JobContext) error {
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
		t.Fatal("Start() did not return after grace period")
	}

	nacks := s.nackList()
	if len(nacks) != 1 {
		t.Fatalf("nacks = %+v, want exactly 1", nacks)
	}
	if nacks[0].JobID != "job-stuck" {
		t.Errorf("nacked job = %s, want job-stuck", nacks[0].JobID)
	}
	if !nacks[0].Error.Retryable {
		t.Error("shutdown NACK must be retryable so the server reschedules the job")
	}
	if nacks[0].Error.Code != "worker_shutdown" {
		t.Errorf("nack code = %s, want worker_shutdown", nacks[0].Error.Code)
	}
}

// TestHeartbeatIncludesLabelsAndCapabilities locks that WithLabels and
// WithWorkerCapabilities actually reach the wire. Both used to be stored on the
// config and never transmitted, making the options silent no-ops.
func TestHeartbeatIncludesLabelsAndCapabilities(t *testing.T) {
	s := &workerTestServer{}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL,
		WithLabels("canary", "v2.1"),
		WithWorkerCapabilities(WorkerCapabilities{Accelerator: "gpu", CPUCores: 32}),
	)
	if err := w.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() = %v", err)
	}

	hbs := s.heartbeatList()
	if len(hbs) != 1 {
		t.Fatalf("heartbeats = %d, want 1", len(hbs))
	}
	hb := hbs[0]

	labels, ok := hb["labels"].([]any)
	if !ok || len(labels) != 2 || labels[0] != "canary" {
		t.Errorf("heartbeat labels = %v, want [canary v2.1]", hb["labels"])
	}
	caps, ok := hb["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("heartbeat capabilities = %v, want an object", hb["capabilities"])
	}
	if caps["accelerator"] != "gpu" {
		t.Errorf("capabilities.accelerator = %v, want gpu", caps["accelerator"])
	}
	if hb["active_job_ids"] == nil {
		t.Error("active_job_ids is required by the worker protocol and must never be null")
	}
}

// TestHeartbeatOmitsUnsetLabelsAndCapabilities keeps the payload byte-identical
// for workers that do not configure these optional fields.
func TestHeartbeatOmitsUnsetLabelsAndCapabilities(t *testing.T) {
	s := &workerTestServer{}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL)
	if err := w.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() = %v", err)
	}

	hbs := s.heartbeatList()
	if len(hbs) != 1 {
		t.Fatalf("heartbeats = %d, want 1", len(hbs))
	}
	if _, present := hbs[0]["labels"]; present {
		t.Error("labels must be omitted when unset")
	}
	if _, present := hbs[0]["capabilities"]; present {
		t.Error("capabilities must be omitted when unset")
	}
}

// TestHeartbeatActiveJobIDsAreSorted locks deterministic heartbeat payloads.
func TestHeartbeatActiveJobIDsAreSorted(t *testing.T) {
	s := &workerTestServer{}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	w := NewWorker(srv.URL)
	for _, id := range []string{"z", "m", "a"} {
		w.active.add(id)
	}
	if err := w.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() = %v", err)
	}

	hbs := s.heartbeatList()
	ids, _ := hbs[0]["active_job_ids"].([]any)
	want := []string{"a", "m", "z"}
	if len(ids) != len(want) {
		t.Fatalf("active_job_ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("active_job_ids = %v, want %v", ids, want)
		}
	}
	if n, _ := hbs[0]["active_jobs"].(float64); int(n) != 3 {
		t.Errorf("active_jobs = %v, want 3", hbs[0]["active_jobs"])
	}
}

// TestWorkerMiddlewareChainConcurrentUse exercises the chain under the race
// detector: Use mutates the chain while running jobs compose it.
func TestWorkerMiddlewareChainConcurrentUse(t *testing.T) {
	c := newMiddlewareChain()
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.AddAutoNamed(func(ctx JobContext, next HandlerFunc) error { return next(ctx) })
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.then(func(JobContext) error { return nil })
		}()
	}
	wg.Wait()

	c.mu.RLock()
	n := len(c.middleware)
	c.mu.RUnlock()
	if n != 25 {
		t.Errorf("chain length = %d, want 25", n)
	}
}

type forcedNACKProbe struct {
	t         *testing.T
	totalJobs int
	hungJobID string

	mu          sync.Mutex
	attempts    map[string]int
	inFlight    int
	maxInFlight int

	allAttempted     chan struct{}
	allAttemptedOnce sync.Once
	hungStarted      chan struct{}
	hungStartedOnce  sync.Once
}

func newForcedNACKProbe(t *testing.T, totalJobs int, hungJobID string) *forcedNACKProbe {
	t.Helper()
	return &forcedNACKProbe{
		t:            t,
		totalJobs:    totalJobs,
		hungJobID:    hungJobID,
		attempts:     make(map[string]int, totalJobs),
		allAttempted: make(chan struct{}),
		hungStarted:  make(chan struct{}),
	}
}

func (p *forcedNACKProbe) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != basePath+"/workers/nack" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var req nackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.t.Errorf("decode NACK: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	p.attempts[req.JobID]++
	p.inFlight++
	if p.inFlight > p.maxInFlight {
		p.maxInFlight = p.inFlight
	}
	if len(p.attempts) == p.totalJobs {
		p.allAttemptedOnce.Do(func() { close(p.allAttempted) })
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.inFlight--
		p.mu.Unlock()
	}()

	if req.JobID == p.hungJobID {
		p.hungStartedOnce.Do(func() { close(p.hungStarted) })
		<-r.Context().Done()
		return
	}
	w.Header().Set("Content-Type", ojsContentType)
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func (p *forcedNACKProbe) assertAttempts(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.attempts) != p.totalJobs {
		t.Fatalf("attempted %d unique jobs, want %d", len(p.attempts), p.totalJobs)
	}
	for i := range p.totalJobs {
		id := fmt.Sprintf("job-%03d", i)
		if p.attempts[id] != 1 {
			t.Errorf("attempts[%s] = %d, want exactly 1", id, p.attempts[id])
		}
	}
	if p.maxInFlight > forcedNACKConcurrency {
		t.Errorf("maximum concurrent NACKs = %d, cap = %d", p.maxInFlight, forcedNACKConcurrency)
	}
	if p.maxInFlight < 2 {
		t.Errorf("maximum concurrent NACKs = %d, want concurrent progress around the hung request", p.maxInFlight)
	}
}

// TestForceNackConcurrent100JobsOneHung verifies that the bounded-concurrent
// NACK sweep gives every claimed job an attempt while one request is hung.
func TestForceNackConcurrent100JobsOneHung(t *testing.T) {
	const nJobs = 100
	probe := newForcedNACKProbe(t, nJobs, "job-050")
	srv := httptest.NewServer(http.HandlerFunc(probe.handler))
	defer srv.Close()

	w := NewWorker(srv.URL)
	for i := range nJobs {
		w.active.add(fmt.Sprintf("job-%03d", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	go func() {
		w.forceNackUnreported(ctx)
		close(done)
	}()

	select {
	case <-probe.hungStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("hung NACK was never attempted")
	}
	select {
	case <-probe.allAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("not every claimed job received a NACK attempt while one request was hung")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forceNackUnreported did not honor shared context cancellation")
	}
	probe.assertAttempts(t)
}

// TestForceNackRaceBarrier exercises the race between handler completion and
// the forced NACK sweep. It starts a job, then triggers shutdown such that the
// handler and the NACK sweep race on the same reportGuard. Exactly one must
// win: no double-report, no panic.
func TestForceNackRaceBarrier(t *testing.T) {
	var ackCount, nackCount atomic.Int64
	var fetched atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case basePath + "/workers/fetch":
			if fetched.CompareAndSwap(false, true) {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []map[string]any{
					{"id": "race-job", "type": "race.job", "attempt": 1},
				}})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case basePath + "/workers/ack":
			ackCount.Add(1)
			json.NewEncoder(w).Encode(map[string]any{})
		case basePath + "/workers/nack":
			nackCount.Add(1)
			json.NewEncoder(w).Encode(map[string]any{})
		case basePath + "/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handlerStarted := make(chan struct{})
	handlerRelease := make(chan struct{})

	w := NewWorker(srv.URL,
		WithPollInterval(5*time.Millisecond),
		WithHeartbeatInterval(time.Hour),
		WithGracePeriod(1*time.Millisecond), // Expire immediately.
	)
	w.Register("race.job", func(jc JobContext) error {
		close(handlerStarted)
		<-handlerRelease // Hold until we trigger the race.
		return nil       // Success: would ACK if not already NACKed.
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	<-handlerStarted
	// Cancel and release simultaneously to create the race window.
	cancel()
	close(handlerRelease)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return")
	}

	total := ackCount.Load() + nackCount.Load()
	if total != 1 {
		t.Errorf("total reports = %d (ack=%d nack=%d), want exactly 1", total, ackCount.Load(), nackCount.Load())
	}
}
