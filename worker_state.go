package ojs

import (
	"sync"
)

// WorkerState represents the lifecycle state of a worker.
type WorkerState string

const (
	WorkerStateRunning   WorkerState = "running"
	WorkerStateQuiet     WorkerState = "quiet"
	WorkerStateTerminate WorkerState = "terminate"
)

// workerLifecycle owns the worker lifecycle state and the rules governing
// transitions between states, including server-directed transitions received
// in heartbeat responses.
//
// The transition rules are defined by the OJS worker protocol: a worker may
// move running -> quiet -> terminate and may be reactivated from quiet back to
// running, but terminate is absorbing — once a worker is terminating it never
// goes back.
//
// State is a plain field guarded by mu, not an atomic.Value: making terminate
// truly absorbing requires reading the current state and deciding whether a
// transition is legal, then writing the new state, as a single atomic
// operation. A read-then-write pair with no shared lock between them (the
// shape both set and applyServerDirective had before) is a check-then-act
// race: a server-directed "quiet" directive that read the state as "running"
// and a local shutdown call entering "terminate" can interleave so the
// directive's now-stale write clobbers terminate back to "quiet" after the
// worker had already started tearing down. Holding mu across the whole
// check-and-write in both set and applyServerDirective closes that window --
// whichever call observes and claims "terminate" first, every other call
// (concurrent or later) sees it and refuses to move away from it.
type workerLifecycle struct {
	mu    sync.Mutex
	state WorkerState

	// terminateOnce is fired the first time the worker enters terminate, from
	// either a local shutdown request or an accepted server directive, and
	// closes terminating exactly once. It is the single edge the run loop
	// waits on, so a server-directed terminate takes exactly the same
	// shutdown path as caller cancellation.
	terminateOnce sync.Once
	terminating   chan struct{}
}

func newWorkerLifecycle() *workerLifecycle {
	return &workerLifecycle{
		state:       WorkerStateRunning,
		terminating: make(chan struct{}),
	}
}

// current returns the current lifecycle state.
func (l *workerLifecycle) current() WorkerState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.currentLocked()
}

// currentLocked returns the current state, defaulting an unset zero value to
// running. Callers must hold mu. The default exists for a workerLifecycle
// constructed as a bare struct literal rather than through
// newWorkerLifecycle (nothing in this package does that, but current()
// predates this file and previously tolerated it, so the fallback is kept).
func (l *workerLifecycle) currentLocked() WorkerState {
	if l.state == "" {
		return WorkerStateRunning
	}
	return l.state
}

// set moves the lifecycle to s unless the worker has already entered
// terminate, in which case it is a no-op: terminate is absorbing, so no later
// call to set — including another local shutdown call — may move the worker
// back to quiet or running. Used for locally initiated shutdown.
//
// The absorbing check and the write happen under the same lock as
// applyServerDirective's, so the two cannot interleave into a clobbered
// state: see the workerLifecycle doc comment.
func (l *workerLifecycle) set(s WorkerState) {
	l.mu.Lock()
	if l.currentLocked() == WorkerStateTerminate {
		l.mu.Unlock()
		return
	}
	l.state = s
	l.mu.Unlock()

	if s == WorkerStateTerminate {
		l.terminateOnce.Do(func() { close(l.terminating) })
	}
}

// terminated returns a channel closed once the worker has entered terminate.
//
// Only terminate is observable this way: quiet is a fetch-stop state that a
// worker can be released from again, so it must not unblock shutdown.
func (l *workerLifecycle) terminated() <-chan struct{} {
	return l.terminating
}

// applyServerDirective applies a state directive received from the server and
// reports whether the state changed. Transitions that the protocol does not
// permit (in particular any transition away from terminate) are ignored.
//
// The validity check (isValidWorkerTransition) and the state write happen
// under the same lock, atomically: a directive that reads as valid cannot
// still be written after a concurrent call — a local shutdown, or another
// directive — has since moved the worker to terminate, because that
// concurrent call would have to complete its own critical section first, and
// this call re-observes the now-current state before deciding.
func (l *workerLifecycle) applyServerDirective(desired WorkerState) bool {
	l.mu.Lock()
	if !isValidWorkerTransition(l.currentLocked(), desired) {
		l.mu.Unlock()
		return false
	}
	l.state = desired
	l.mu.Unlock()

	if desired == WorkerStateTerminate {
		l.terminateOnce.Do(func() { close(l.terminating) })
	}
	return true
}

// isValidWorkerTransition reports whether the worker protocol permits moving
// from current to desired.
func isValidWorkerTransition(current, desired WorkerState) bool {
	if current == desired {
		return false
	}
	switch current {
	case WorkerStateRunning:
		return desired == WorkerStateQuiet || desired == WorkerStateTerminate
	case WorkerStateQuiet:
		return desired == WorkerStateRunning || desired == WorkerStateTerminate
	case WorkerStateTerminate:
		// terminate is absorbing: backward transitions are not allowed.
		return false
	default:
		return false
	}
}
