package ojs

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// activeJobSet tracks the jobs a worker currently has in flight.
//
// It owns two things that used to be scattered across the worker: the identity
// of in-flight jobs (reported to the server in every heartbeat) and the drain
// signal used during graceful shutdown. Callers never observe a count that
// disagrees with the tracked ID set, because both are updated under one lock.
type activeJobSet struct {
	mu    sync.Mutex
	ids   map[string]*reportGuard
	empty chan struct{} // closed and replaced whenever the set becomes empty

	reportMu          sync.RWMutex
	reportingDisabled bool
	reportWG          sync.WaitGroup
}

// reportGuard serialises the single terminal outcome a job is allowed to
// report. Exactly one of the handler goroutine and the shutdown force-NACK path
// may win it, so a job can never be both ACKed by its handler and NACKed by the
// grace-period sweep.
type reportGuard struct {
	claimed atomic.Bool
}

// claim reserves the terminal report. It returns true at most once per guard.
//
// A nil guard is unguarded and always reports: that keeps a directly
// constructed job execution (tests, and any path that never registered the job
// as active) behaving exactly as it did before serialisation existed.
func (g *reportGuard) claim() bool {
	if g == nil {
		return true
	}
	return g.claimed.CompareAndSwap(false, true)
}

// claimForHandler reserves a job's terminal report while handler reporting is
// still enabled. The read lock makes disabling reporting a barrier: once
// disableReportingAndClaimUnreported returns, no handler can acquire a new
// terminal-report claim.
func (s *activeJobSet) claimForHandler(g *reportGuard) bool {
	if g == nil {
		return true
	}

	s.reportMu.RLock()
	defer s.reportMu.RUnlock()
	if s.reportingDisabled || !g.claim() {
		return false
	}
	s.reportWG.Add(1)
	return true
}

// finishHandlerReport releases the reporting join point acquired by
// claimForHandler.
func (s *activeJobSet) finishHandlerReport(g *reportGuard) {
	if g != nil {
		s.reportWG.Done()
	}
}

// waitHandlerReports waits for every handler that claimed a terminal outcome
// before reporting was disabled. It must only be called after
// disableReportingAndClaimUnreported, which prevents concurrent WaitGroup adds.
func (s *activeJobSet) waitHandlerReports() {
	s.reportWG.Wait()
}

func newActiveJobSet() *activeJobSet {
	s := &activeJobSet{ids: make(map[string]*reportGuard)}
	s.empty = make(chan struct{})
	close(s.empty) // starts empty
	return s
}

// add registers a job as in flight and returns its terminal-report guard.
//
// Re-adding an id keeps the existing guard, so a duplicate registration can
// never hand out a second licence to report the same job.
func (s *activeJobSet) add(id string) *reportGuard {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.ids[id]; ok {
		return g
	}
	if len(s.ids) == 0 {
		// Transitioning from empty to non-empty: install a fresh, open channel.
		s.empty = make(chan struct{})
	}
	g := &reportGuard{}
	s.ids[id] = g
	return g
}

// remove deregisters a job, releasing any waiter once the set drains.
func (s *activeJobSet) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; !ok {
		return
	}
	delete(s.ids, id)
	if len(s.ids) == 0 {
		close(s.empty)
	}
}

// count returns the number of in-flight jobs.
func (s *activeJobSet) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ids)
}

// idsSnapshot returns the in-flight job IDs in a stable (sorted) order.
//
// Ordering is deterministic so that heartbeat payloads are reproducible and
// assertable; the previous sync.Map traversal produced a different order on
// every call.
func (s *activeJobSet) idsSnapshot() []string {
	s.mu.Lock()
	ids := make([]string, 0, len(s.ids))
	for id := range s.ids {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	sort.Strings(ids)
	return ids
}

// drained returns a channel that is closed once no jobs are in flight.
//
// The channel is sampled under the same lock that mutates the set, so a caller
// that observes a non-empty set is guaranteed to be handed the channel that
// will be closed when that set drains.
func (s *activeJobSet) drained() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.empty
}

// waitDrained blocks until the set is empty, the context is cancelled, or the
// timeout channel fires. It reports whether the set fully drained.
func (s *activeJobSet) waitDrained(ctx context.Context, timeout <-chan struct{}) bool {
	select {
	case <-s.drained():
		return true
	case <-timeout:
		return false
	case <-ctx.Done():
		return false
	}
}

// waitDrainedFor blocks until the set is empty, the context is cancelled, or
// the grace period elapses. It reports whether the set fully drained.
//
// The timer is owned here rather than by a watchdog goroutine in the caller:
// there is no state to share, and a goroutine that outlives the wait is exactly
// the leak the drain path used to have.
func (s *activeJobSet) waitDrainedFor(ctx context.Context, grace time.Duration) bool {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-s.drained():
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// disableReportingAndClaimUnreported first prevents handlers from acquiring
// new terminal-report claims, then claims every still-unreported in-flight job
// for the shutdown sweep and returns those IDs in stable order.
//
// The reporting write lock is the grace-expiry barrier. A handler that acquired
// the read lock first is an already-claimed report and is allowed to finish on
// its own bounded context. Every later handler completion is disabled before
// the forced NACK claims are taken.
func (s *activeJobSet) disableReportingAndClaimUnreported() []string {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	s.reportingDisabled = true

	s.mu.Lock()
	guards := make(map[string]*reportGuard, len(s.ids))
	for id, g := range s.ids {
		guards[id] = g
	}
	s.mu.Unlock()

	claimed := make([]string, 0, len(guards))
	for id, g := range guards {
		if g.claim() {
			claimed = append(claimed, id)
		}
	}
	sort.Strings(claimed)
	return claimed
}
