package ojs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ErrWorkerAlreadyStarted is returned by [Worker.Start] when the worker has
// already been started. A Worker is single-use: create a new one to restart.
var ErrWorkerAlreadyStarted = errors.New("ojs: worker already started")

// Worker is an OJS worker that fetches and processes jobs from an OJS server.
// It supports configurable concurrency, middleware, and graceful shutdown.
//
// This file owns worker orchestration: polling, dispatch, concurrency limits,
// and shutdown sequencing. The worker protocol wire format lives in
// worker_protocol.go, lifecycle transition rules in worker_state.go, and
// in-flight job bookkeeping in worker_activejobs.go.
type Worker struct {
	transport *transport
	config    workerConfig
	workerID  string

	handlers   map[string]HandlerFunc
	handlersMu sync.RWMutex

	middleware *middlewareChain

	lifecycle *workerLifecycle
	active    *activeJobSet

	// handlers running in their own goroutines. Tracked separately from the
	// active job set: the set is emptied as soon as a job reports its outcome,
	// while the goroutine itself lives a moment longer, and shutdown must not
	// return while one is still touching worker state.
	handlerWG sync.WaitGroup

	started  atomic.Bool
	stopOnce sync.Once
	stopped  chan struct{}
}

// NewWorker creates a new OJS worker connected to the given server URL.
//
// Example:
//
//	worker := ojs.NewWorker("http://localhost:8080",
//	    ojs.WithQueues("default", "email"),
//	    ojs.WithConcurrency(10),
//	)
func NewWorker(serverURL string, opts ...WorkerOption) *Worker {
	cfg := resolveWorkerConfig(opts)

	return &Worker{
		transport:  newWorkerTransport(serverURL, cfg),
		config:     cfg,
		workerID:   generateWorkerID(),
		handlers:   make(map[string]HandlerFunc),
		middleware: newMiddlewareChain(),
		lifecycle:  newWorkerLifecycle(),
		active:     newActiveJobSet(),
		stopped:    make(chan struct{}),
	}
}

// Register associates a job type with a handler function.
//
// Example:
//
//	worker.Register("email.send", func(ctx ojs.JobContext) error {
//	    to := ctx.Job.Args["to"].(string)
//	    // process...
//	    ctx.SetResult(map[string]any{"messageId": "..."})
//	    return nil
//	})
func (w *Worker) Register(jobType string, handler HandlerFunc) {
	w.handlersMu.Lock()
	defer w.handlersMu.Unlock()
	w.handlers[jobType] = handler
}

// Use adds execution middleware to the worker's middleware chain.
//
// Example:
//
//	worker.Use(func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
//	    log.Printf("Processing %s", ctx.Job.Type)
//	    start := time.Now()
//	    err := next(ctx)
//	    log.Printf("Done in %s", time.Since(start))
//	    return err
//	})
func (w *Worker) Use(fn MiddlewareFunc) {
	w.middleware.AddAutoNamed(fn)
}

// UseNamed adds a named execution middleware to the worker's middleware chain.
func (w *Worker) UseNamed(name string, fn MiddlewareFunc) {
	w.middleware.Add(name, fn)
}

// Start begins fetching and processing jobs. It blocks until the context
// is cancelled or the worker receives a shutdown signal.
//
// A Worker may only be started once; subsequent calls return
// [ErrWorkerAlreadyStarted].
//
// Example:
//
//	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
//	defer cancel()
//	if err := worker.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
func (w *Worker) Start(ctx context.Context) error {
	w.handlersMu.RLock()
	registered := len(w.handlers)
	w.handlersMu.RUnlock()
	if registered == 0 {
		return fmt.Errorf("ojs: no handlers registered")
	}

	if !w.started.CompareAndSwap(false, true) {
		return ErrWorkerAlreadyStarted
	}

	// Heartbeats, ACKs and NACKs use contexts that outlive the caller's ctx so
	// that jobs finishing during the graceful drain phase can still be reported
	// to the server. Using the cancelled ctx here would make every drain-phase
	// ACK/NACK fail immediately, silently re-running completed jobs.
	detached := context.WithoutCancel(ctx)

	// reportCtx is the shutdown-surviving parent for bounded handler-driven
	// ACK/NACK contexts. Grace expiry disables new handler claims through the
	// active-job reporting barrier; already-claimed reports keep this live
	// parent long enough to deliver their one terminal outcome.
	reportCtx, stopHandlerReporting := context.WithCancel(detached)
	defer stopHandlerReporting()

	heartbeatCtx, stopHeartbeats := context.WithCancel(detached)
	defer stopHeartbeats()

	// runCtx governs fetching and handler execution. Deriving it from ctx means
	// caller cancellation and a server-directed terminate converge on exactly
	// one shutdown path: both cancel runCtx here and nothing else differs.
	runCtx, stopWork := context.WithCancel(ctx)
	defer stopWork()

	var heartbeats sync.WaitGroup
	heartbeats.Add(1)
	go func() {
		defer heartbeats.Done()
		w.heartbeatLoop(heartbeatCtx)
	}()

	fetchStopped := make(chan struct{})
	go func() {
		defer close(fetchStopped)
		w.fetchLoop(runCtx, reportCtx)
	}()

	select {
	case <-ctx.Done():
		// Caller-initiated shutdown.
	case <-w.lifecycle.terminated():
		// Server-directed terminate accepted in a heartbeat response.
	}

	w.shutdown(shutdownControls{
		reportCtx:    reportCtx,
		detached:     detached,
		stopWork:     stopWork,
		fetchStopped: fetchStopped,
	})

	// Stop heartbeats only after the drain is over: the server has to keep
	// seeing this worker while it is draining.
	stopHeartbeats()
	heartbeats.Wait()
	return nil
}

const (
	// terminalReportTimeout bounds a handler's ACK/NACK after it has claimed
	// the job's terminal outcome. It is independent of the cancelled handler
	// context and prevents a claimed report from holding shutdown indefinitely.
	terminalReportTimeout = 5 * time.Second

	// forcedReportTimeout bounds the forced NACK sweep issued when the grace
	// period expires.
	forcedReportTimeout = 5 * time.Second
)

// shutdownControls carries the cancellation handles and join points that the
// shutdown sequence operates on. They are grouped because they are only ever
// used together, in a fixed order, by exactly one caller.
type shutdownControls struct {
	// reportCtx licenses handler-driven ACK/NACK.
	reportCtx context.Context
	// detached outlives the caller's context and is the parent of the bounded
	// context used by the forced NACK sweep.
	detached context.Context
	// stopWork cancels fetching and handler execution.
	stopWork context.CancelFunc
	// fetchStopped is closed when the fetch loop goroutine has returned.
	fetchStopped <-chan struct{}
}

// shutdown runs the graceful shutdown sequence shared by caller cancellation
// and a server-directed terminate.
//
// The order is load-bearing:
//  1. enter terminate and close the stop signal, so no further job is dispatched;
//  2. cancel handler work and join the fetch loop, so the set of in-flight jobs
//     is final before anything looks at it;
//  3. drain within the grace period;
//  4. on expiry, disable handler reporting first, then force-NACK only the jobs
//     that have not already claimed their one terminal outcome.
func (w *Worker) shutdown(c shutdownControls) {
	w.lifecycle.set(WorkerStateTerminate)
	w.stopOnce.Do(func() { close(w.stopped) })

	// Cancelling runCtx is what a caller cancellation did implicitly; doing it
	// explicitly is what makes the server-directed path identical.
	c.stopWork()

	// Join the fetch loop before taking any drain decision. While it runs it can
	// still register a job as active, and a drain that started before it stopped
	// could observe an empty set and declare success with a job about to start.
	<-c.fetchStopped

	if w.active.waitDrainedFor(c.reportCtx, w.config.gracePeriod) {
		// Every job reported its own outcome; wait for the goroutines to unwind.
		w.handlerWG.Wait()
		return
	}

	forceCtx, cancel := context.WithTimeout(c.detached, forcedReportTimeout)
	defer cancel()
	w.forceNackUnreported(forceCtx)

	// Reports claimed before the grace-expiry barrier are allowed to finish
	// their one bounded ACK/NACK. Jobs whose handlers had not claimed were
	// forced above, and later handler completions cannot claim at all.
	w.active.waitHandlerReports()

	// Remaining handlers are abandoned rather than waited on: the grace period
	// is the contract, and a handler that ignored cancellation would otherwise
	// hold shutdown open indefinitely. Their jobs are already NACKed above.
}

// State returns the current worker lifecycle state.
func (w *Worker) State() WorkerState {
	return w.lifecycle.current()
}

// fetchLoop is the main loop that fetches and dispatches jobs.
//
// ctx governs fetching and handler execution and is cancelled on shutdown.
// reportCtx outlives ctx and is used only to report job outcomes, so a job that
// finishes during the drain phase can still be ACKed or NACKed.
func (w *Worker) fetchLoop(ctx context.Context, reportCtx context.Context) {
	sem := make(chan struct{}, w.config.concurrency)

	consecutiveErrors := 0

	for {
		if w.shouldStop(ctx) {
			return
		}

		if w.State() != WorkerStateRunning {
			// In quiet or terminate state, stop fetching.
			if !w.pause(ctx, w.config.pollInterval) {
				return
			}
			continue
		}

		// Check if we have capacity.
		free := w.config.concurrency - w.active.count()
		if free <= 0 {
			if !w.pause(ctx, w.config.pollInterval) {
				return
			}
			continue
		}

		jobs, err := w.fetchJobs(ctx, free)
		if err != nil {
			consecutiveErrors++
			w.logWarn(ctx, "failed to fetch jobs",
				slog.String("error", err.Error()),
				slog.Int("consecutive_errors", consecutiveErrors),
			)
			if !w.pause(ctx, fetchBackoff(consecutiveErrors, w.config.pollInterval)) {
				return
			}
			continue
		}

		consecutiveErrors = 0

		if len(jobs) == 0 {
			// No jobs available, wait before polling again.
			if !w.pause(ctx, w.config.pollInterval) {
				return
			}
			continue
		}

		if !w.dispatch(ctx, reportCtx, sem, jobs) {
			return
		}
	}
}

// dispatch hands each fetched job to a goroutine, blocking on the concurrency
// semaphore. It reports false if the worker was asked to stop while waiting for
// a slot, in which case the undispatched jobs are left for the server to
// reclaim via visibility timeout.
func (w *Worker) dispatch(ctx, reportCtx context.Context, sem chan struct{}, jobs []Job) bool {
	// Indexed rather than ranged by value: a Job envelope is 256 bytes and the
	// only copy that has to exist is the one JobContext owns.
	for i := range jobs {
		job := &jobs[i]

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return false
		case <-w.stopped:
			return false
		}

		// Re-check after acquiring the slot and before registering the job as
		// active: waiting for a slot can take arbitrarily long, and a job that
		// is registered or started after shutdown began would either miss the
		// drain snapshot or start work the worker has already promised not to
		// start. Fetched-but-undispatched jobs are simply left for the server to
		// reclaim via the visibility timeout, exactly as an unfetched job is.
		if w.shouldStop(ctx) {
			<-sem
			return false
		}

		guard := w.active.add(job.ID)
		w.handlerWG.Add(1)
		go func() {
			defer func() {
				w.active.remove(job.ID)
				<-sem
				w.handlerWG.Done()
			}()
			w.processJob(ctx, reportCtx, job, guard)
		}()
	}
	return true
}

// shouldStop reports whether the fetch loop must exit now.
func (w *Worker) shouldStop(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-w.stopped:
		return true
	default:
		return false
	}
}

// pause waits for d, returning false if the worker must stop instead.
func (w *Worker) pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-w.stopped:
		return false
	case <-timer.C:
		return true
	}
}

const maxFetchBackoff = 30 * time.Second

// fetchBackoff returns an exponential backoff duration capped at maxFetchBackoff.
func fetchBackoff(consecutiveErrors int, base time.Duration) time.Duration {
	if consecutiveErrors <= 1 {
		return base
	}
	shift := consecutiveErrors - 1
	if shift > 10 {
		shift = 10
	}
	backoff := base * time.Duration(1<<shift)
	if backoff > maxFetchBackoff {
		backoff = maxFetchBackoff
	}
	return backoff
}

// processJob executes a single job through the middleware chain and handler.
// ctx is the handler-visible context; reportCtx is used to report the outcome
// and survives worker shutdown. guard licenses the single terminal ACK/NACK
// this job is allowed to produce; a nil guard means the caller did not register
// the job and reporting is unrestricted.
func (w *Worker) processJob(ctx, reportCtx context.Context, job *Job, guard *reportGuard) {
	w.handlersMu.RLock()
	handler, ok := w.handlers[job.Type]
	w.handlersMu.RUnlock()

	if !ok {
		// No handler registered for this job type. NACK it as non-retryable.
		terminalCtx, finishReport, claimed := w.claimTerminalReport(reportCtx, guard)
		if !claimed {
			return
		}
		defer finishReport()
		if nackErr := w.nackHandlerErrorWithRetry(terminalCtx, job.ID,
			fmt.Sprintf("no handler registered for job type %q", job.Type), false); nackErr != nil {
			w.logError(terminalCtx, "failed to nack unhandled job",
				slog.String("job.id", job.ID),
				slog.String("job.type", job.Type),
				slog.String("error", nackErr.Error()),
			)
		}
		return
	}

	ref := &jobResultRef{}
	jctx := JobContext{
		Job:       *job,
		Attempt:   job.Attempt,
		Queue:     job.Queue,
		ctx:       ctx,
		resultRef: ref,
		worker:    w,
	}

	err := w.runHandler(ctx, &jctx, handler)

	// The outcome is decided; claim the job's one terminal report. Losing the
	// claim means the shutdown sweep already NACKed this job, so reporting again
	// would contradict what the server has already been told.
	terminalCtx, finishReport, claimed := w.claimTerminalReport(reportCtx, guard)
	if !claimed {
		w.logWarn(reportCtx, "job outcome discarded: terminal outcome already reported",
			slog.String("job.id", job.ID),
			slog.String("job.type", job.Type),
		)
		return
	}
	defer finishReport()

	if err != nil {
		// Job failed. NACK it, respecting the error's retryability signal.
		if nackErr := w.nackHandlerErrorWithRetry(terminalCtx, job.ID, err.Error(), isHandlerRetryable(err)); nackErr != nil {
			w.logError(terminalCtx, "failed to nack job after retries",
				slog.String("job.id", job.ID),
				slog.String("job.type", job.Type),
				slog.String("error", nackErr.Error()),
			)
		}
		return
	}

	// Job succeeded. ACK it.
	if ackErr := w.ackJobWithRetry(terminalCtx, job.ID, ref.data); ackErr != nil {
		w.logError(terminalCtx, "failed to ack job after retries",
			slog.String("job.id", job.ID),
			slog.String("job.type", job.Type),
			slog.String("error", ackErr.Error()),
		)
	}
}

// claimTerminalReport acquires the handler side of a job's terminal-report
// guard and gives the winner a shutdown-surviving but bounded context.
func (w *Worker) claimTerminalReport(parent context.Context, guard *reportGuard) (context.Context, func(), bool) {
	if !w.active.claimForHandler(guard) {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(parent, terminalReportTimeout)
	return ctx, func() {
		cancel()
		w.active.finishHandlerReport(guard)
	}, true
}

// runHandler invokes the middleware chain and handler with panic recovery, so a
// single handler crash cannot take down the worker process.
func (w *Worker) runHandler(ctx context.Context, jctx *JobContext, handler HandlerFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in job handler: %v", r)
			w.logError(ctx, "job handler panicked",
				slog.String("job.id", jctx.Job.ID),
				slog.String("job.type", jctx.Job.Type),
				slog.Any("panic", r),
			)
		}
	}()
	return w.middleware.then(handler)(*jctx)
}

// heartbeatLoop sends periodic heartbeats to the OJS server.
func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.config.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.sendHeartbeat(ctx); err != nil {
				w.logWarn(ctx, "heartbeat failed",
					slog.String("worker.id", w.workerID),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// forcedNACKConcurrency caps the number of concurrent NACK requests during
// the forced shutdown sweep. This ensures progress even if one NACK hangs,
// while avoiding overwhelming the server or exhausting connections.
const forcedNACKConcurrency = 10

// forceNackUnreported nacks every in-flight job that has not already claimed a
// terminal outcome, so the server can reschedule it immediately instead of
// waiting for the visibility timeout. Called when the grace period expires.
//
// A fixed worker pool sends NACKs under the shared ctx deadline. Every claimed
// job is handed to nackJob exactly once; if one request hangs, the remaining
// workers continue draining the queue. Jobs whose handler already claimed
// their report were skipped upstream by disableReportingAndClaimUnreported.
//
// Errors are collected by sorted job index and logged in deterministic job-ID
// order after all workers finish, avoiding interleaved concurrent log output.
func (w *Worker) forceNackUnreported(ctx context.Context) {
	ids := w.active.disableReportingAndClaimUnreported()
	if len(ids) == 0 {
		return
	}

	type nackError struct {
		jobID string
		err   error
	}

	results := make([]nackError, len(ids))
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)

	workerCount := min(forcedNACKConcurrency, len(ids))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				jobID := ids[idx]
				if err := w.nackJob(ctx, jobID, "worker_shutdown",
					"worker shutting down: grace period expired", true); err != nil {
					results[idx] = nackError{jobID, err}
				}
			}
		}()
	}
	wg.Wait()

	// Log errors deterministically (ids are already sorted).
	for _, r := range results {
		if r.err != nil {
			w.logError(ctx, "failed to nack job during shutdown",
				slog.String("job.id", r.jobID),
				slog.String("error", r.err.Error()),
			)
		}
	}
}

// generateWorkerID generates a unique worker identifier.
func generateWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("worker_%s_%d_%d", hostname, os.Getpid(), time.Now().UnixNano())
}

// logError logs an error if the worker has a logger configured.
func (w *Worker) logError(ctx context.Context, msg string, attrs ...slog.Attr) {
	if w.config.logger != nil {
		w.config.logger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
	}
}

// logWarn logs a warning if the worker has a logger configured.
func (w *Worker) logWarn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if w.config.logger != nil {
		w.config.logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
	}
}

const ackNackMaxRetries = 3

// ackNackRetryDelay is the pause before retry attempt n (0-indexed).
func ackNackRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt+1) * 500 * time.Millisecond
}

// retryReport runs a report call (ACK or NACK) up to ackNackMaxRetries times.
func (w *Worker) retryReport(ctx context.Context, jobID, op string, call func() error) error {
	var lastErr error
	for attempt := 0; attempt < ackNackMaxRetries; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		lastErr = err
		w.logWarn(ctx, op+" attempt failed, retrying",
			slog.String("job.id", jobID),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(ackNackRetryDelay(attempt)):
		}
	}
	return lastErr
}

// ackJobWithRetry retries ACK up to ackNackMaxRetries times with brief pauses.
func (w *Worker) ackJobWithRetry(ctx context.Context, jobID string, result map[string]any) error {
	return w.retryReport(ctx, jobID, "ack", func() error {
		return w.ackJob(ctx, jobID, result)
	})
}

// nackHandlerErrorWithRetry reports a handler failure, retrying up to
// ackNackMaxRetries times with brief pauses.
//
// The shutdown path deliberately uses the non-retrying nackJob instead: the
// grace period has already expired there, so retrying would extend shutdown
// once per remaining job.
func (w *Worker) nackHandlerErrorWithRetry(ctx context.Context, jobID, message string, retryable bool) error {
	return w.retryReport(ctx, jobID, "nack", func() error {
		return w.nackJob(ctx, jobID, ErrCodeHandlerError, message, retryable)
	})
}
