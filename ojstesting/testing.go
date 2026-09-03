// Package ojstesting provides test utilities for OJS applications.
//
// It implements the OJS Testing Specification (ojs-testing.md) with
// fake mode, assertion helpers, and queue drain utilities.
//
// # Fake Mode
//
// Use [Fake] to activate the in-memory store and [FakeClient] to get a
// real [ojs.Client] that records enqueues without any HTTP server:
//
//	func TestSignup(t *testing.T) {
//	    _ = ojstesting.Fake(t)
//	    client := ojstesting.FakeClient(t)
//	    signupService(client, "user@example.com")
//	    ojstesting.AssertEnqueued(t, "email.send",
//	        ojstesting.MatchArgs(map[string]any{"to": "user@example.com"}),
//	    )
//	}
package ojstesting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// FakeJob represents a job recorded in fake mode.
type FakeJob struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Queue     string         `json:"queue"`
	Args      []any          `json:"args"`
	Meta      map[string]any `json:"meta,omitempty"`
	State     string         `json:"state"`
	Attempt   int            `json:"attempt"`
	CreatedAt time.Time      `json:"created_at"`
}

// MatchOption is a functional option for matching enqueued jobs.
type MatchOption func(*matchCriteria)

type matchCriteria struct {
	args  []any
	queue string
	meta  map[string]any
	count int // 0 means "at least 1"
}

// MatchArgs requires the job args to deep-equal the given value.
func MatchArgs(args ...any) MatchOption {
	return func(c *matchCriteria) { c.args = args }
}

// MatchQueue requires the job to be in the given queue.
func MatchQueue(queue string) MatchOption {
	return func(c *matchCriteria) { c.queue = queue }
}

// MatchMeta requires the job meta to contain the given keys (partial match).
func MatchMeta(meta map[string]any) MatchOption {
	return func(c *matchCriteria) { c.meta = meta }
}

// MatchCount requires exactly n matching jobs.
func MatchCount(n int) MatchOption {
	return func(c *matchCriteria) { c.count = n }
}

// FakeStore is the in-memory store for fake mode.
type FakeStore struct {
	mu        sync.Mutex
	enqueued  []FakeJob
	performed []FakeJob
	handlers  map[string]func(FakeJob) error
	nextID    int
}

var (
	activeStore *FakeStore
	storeMu     sync.Mutex
)

// Fake activates fake mode for the given test. Returns a cleanup function
// that restores state after the test.
func Fake(t *testing.T) *FakeStore {
	t.Helper()
	storeMu.Lock()
	s := &FakeStore{
		handlers: make(map[string]func(FakeJob) error),
	}
	activeStore = s
	storeMu.Unlock()

	t.Cleanup(func() {
		storeMu.Lock()
		if activeStore == s {
			activeStore = nil
		}
		storeMu.Unlock()
	})

	return s
}

// GetStore returns the active fake store, or nil if not in fake mode.
func GetStore() *FakeStore {
	storeMu.Lock()
	defer storeMu.Unlock()
	return activeStore
}

// RegisterHandler registers a handler function for drain execution.
func (s *FakeStore) RegisterHandler(jobType string, handler func(FakeJob) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[jobType] = handler
}

// RecordEnqueue records a job enqueue in the fake store.
func (s *FakeStore) RecordEnqueue(jobType string, args []any, queue string, meta map[string]any) FakeJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	job := FakeJob{
		ID:        fmt.Sprintf("fake-%06d", s.nextID),
		Type:      jobType,
		Queue:     queue,
		Args:      args,
		Meta:      meta,
		State:     "available",
		Attempt:   0,
		CreatedAt: time.Now(),
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
	s.enqueued = append(s.enqueued, job)
	return job
}

// AssertEnqueued asserts that at least one job of the given type was enqueued.
func AssertEnqueued(t *testing.T, jobType string, opts ...MatchOption) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	criteria := buildCriteria(opts)
	matches := filterJobs(s.enqueued, jobType, criteria)

	if criteria.count > 0 {
		if len(matches) != criteria.count {
			t.Errorf("AssertEnqueued: expected %d job(s) of type %q, found %d%s",
				criteria.count, jobType, len(matches), describeStore(s.enqueued))
		}
	} else if len(matches) == 0 {
		t.Errorf("AssertEnqueued: expected at least one job of type %q, found none%s",
			jobType, describeStore(s.enqueued))
	}
}

// RefuteEnqueued asserts that NO job of the given type was enqueued.
func RefuteEnqueued(t *testing.T, jobType string, opts ...MatchOption) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	criteria := buildCriteria(opts)
	matches := filterJobs(s.enqueued, jobType, criteria)

	if len(matches) > 0 {
		t.Errorf("RefuteEnqueued: expected no jobs of type %q, found %d", jobType, len(matches))
	}
}

// AssertPerformed asserts that at least one job of the given type was executed.
func AssertPerformed(t *testing.T, jobType string, opts ...MatchOption) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	criteria := buildCriteria(opts)
	matches := filterJobs(s.performed, jobType, criteria)

	if len(matches) == 0 {
		t.Errorf("AssertPerformed: expected at least one performed job of type %q, found none", jobType)
	}
}

// AssertCompleted asserts that at least one job of the given type completed successfully.
func AssertCompleted(t *testing.T, jobType string, opts ...MatchOption) {
	t.Helper()
	assertPerformedInState(t, "AssertCompleted", "completed", jobType, "completed", opts)
}

// AssertFailed asserts that at least one job of the given type failed (state = "discarded").
func AssertFailed(t *testing.T, jobType string, opts ...MatchOption) {
	t.Helper()
	assertPerformedInState(t, "AssertFailed", "failed", jobType, "discarded", opts)
}

// assertPerformedInState asserts that at least one performed job of jobType
// reached wantState.
//
// AssertCompleted and AssertFailed differ only in the state they look for and
// the word they use for it, so the search and the "here is what actually
// happened" diagnostic live in one place. label is the human word for
// wantState; assertion is the caller's name, which prefixes the failure.
func assertPerformedInState(t *testing.T, assertion, label, jobType, wantState string, opts []MatchOption) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	states, found := statesOfPerformed(s.performed, jobType, wantState, buildCriteria(opts))
	if found {
		return
	}
	t.Errorf("%s: expected at least one %s job of type %q, got states %v",
		assertion, label, jobType, states)
}

// statesOfPerformed reports whether any job matching jobType and the criteria
// reached wantState, together with the states that were observed instead.
//
// Deciding is separated from reporting so the search itself is testable without
// a failing assertion: the states slice is exactly what the diagnostic prints.
func statesOfPerformed(jobs []FakeJob, jobType, wantState string, c matchCriteria) (states []string, found bool) {
	matches := filterJobs(jobs, jobType, c)
	states = make([]string, 0, len(matches))
	for i := range matches {
		if matches[i].State == wantState {
			return nil, true
		}
		states = append(states, matches[i].State)
	}
	return states, false
}

// AllEnqueued returns all enqueued jobs, optionally filtered by type.
func AllEnqueued(jobType ...string) []FakeJob {
	storeMu.Lock()
	s := activeStore
	storeMu.Unlock()
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(jobType) == 0 {
		result := make([]FakeJob, len(s.enqueued))
		copy(result, s.enqueued)
		return result
	}

	var result []FakeJob
	for i := range s.enqueued {
		if s.enqueued[i].Type == jobType[0] {
			result = append(result, s.enqueued[i])
		}
	}
	return result
}

// ClearAll resets the fake store.
func ClearAll(t *testing.T) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enqueued = nil
	s.performed = nil
}

// Drain processes all available jobs using registered handlers.
func Drain(t *testing.T) {
	t.Helper()
	s := mustStore(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.enqueued {
		job := &s.enqueued[i]
		if job.State != "available" {
			continue
		}

		job.State = "active"
		job.Attempt++

		handler, ok := s.handlers[job.Type]
		if ok {
			if err := handler(*job); err != nil {
				job.State = "discarded"
			} else {
				job.State = "completed"
			}
		} else {
			job.State = "completed"
		}
		s.performed = append(s.performed, *job)
	}
}

// --- helpers ---

func mustStore(t *testing.T) *FakeStore {
	t.Helper()
	storeMu.Lock()
	s := activeStore
	storeMu.Unlock()
	if s == nil {
		t.Fatal("OJS testing: not in fake mode. Call ojstesting.Fake(t) first.")
	}
	return s
}

func buildCriteria(opts []MatchOption) matchCriteria {
	var c matchCriteria
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func filterJobs(jobs []FakeJob, jobType string, c matchCriteria) []FakeJob {
	var result []FakeJob
	for i := range jobs {
		if c.matches(&jobs[i], jobType) {
			result = append(result, jobs[i])
		}
	}
	return result
}

// matches reports whether job satisfies the job type and every criterion that
// was actually set. An unset criterion matches everything, so the zero
// matchCriteria selects purely by type.
func (c matchCriteria) matches(job *FakeJob, jobType string) bool {
	switch {
	case job.Type != jobType:
		return false
	case c.queue != "" && job.Queue != c.queue:
		return false
	case c.args != nil && !jsonEqual(job.Args, c.args):
		return false
	case c.meta != nil && !metaContains(job.Meta, c.meta):
		return false
	default:
		return true
	}
}

func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func metaContains(actual, expected map[string]any) bool {
	for k, v := range expected {
		if !jsonEqual(actual[k], v) {
			return false
		}
	}
	return true
}

// describeStore summarises what was actually enqueued, for appending to a failed
// assertion. The asserted job type is already named by the caller's message, so
// it is deliberately not a parameter here.
//
// The per-type counts are sorted: an unordered map walk made the same failure
// print differently on every run, which is unreadable in a diff and impossible
// to assert on.
func describeStore(jobs []FakeJob) string {
	if len(jobs) == 0 {
		return "\n  No jobs were enqueued at all."
	}
	types := make(map[string]int)
	for i := range jobs {
		types[jobs[i].Type]++
	}
	parts := make([]string, 0, len(types))
	for t, c := range types {
		parts = append(parts, fmt.Sprintf("%s (%d)", t, c))
	}
	sort.Strings(parts)
	return "\n  Enqueued: " + strings.Join(parts, ", ")
}
