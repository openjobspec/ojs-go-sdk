package ojs

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerState represents the lifecycle state of a worker.
type WorkerState string

const (
	WorkerStateRunning   WorkerState = "running"
	WorkerStateQuiet     WorkerState = "quiet"
	WorkerStateTerminate WorkerState = "terminate"
)

// jobResultRef is a mutable container for a job's result, shared across
// JobContext copies so that SetResult works through the middleware chain.
type jobResultRef struct {
	data map[string]any
}

// JobContext provides execution-scoped state and capabilities to job handlers.
type JobContext struct {
	// Job is the full job envelope.
	Job Job

	// Attempt is the current attempt number (1-indexed).
	Attempt int

	// Queue is the queue from which the job was fetched.
	Queue string

	// WorkflowID is set if this job is part of a workflow.
	WorkflowID string

	// ParentResults contains results from upstream workflow steps.
	ParentResults map[string]any

	// ctx is the underlying context (cancelled on worker shutdown).
	ctx context.Context

	// resultRef holds a shared reference to the job result so that
	// SetResult works even when JobContext is passed by value.
	resultRef *jobResultRef

	// worker is a reference to the parent worker for heartbeats.
	worker *Worker
}

// Context returns the context.Context for this job execution.
// The context is cancelled when the worker shuts down.
func (jc JobContext) Context() context.Context {
	return jc.ctx
}

// SetResult sets the job's return value.
func (jc JobContext) SetResult(result map[string]any) {
	if jc.resultRef != nil {
		jc.resultRef.data = result
	}
}

// Heartbeat extends the job's visibility timeout.
// Use this for long-running jobs to prevent them from being reclaimed.
func (jc JobContext) Heartbeat() error {
	if jc.worker == nil {
		return nil
	}
	return jc.worker.sendHeartbeat(jc.ctx)
}

// NewJobContextForTest creates a JobContext suitable for use in tests.
// It initialises the internal context to context.Background().
// This is intended only for testing middleware or handlers outside a Worker.
func NewJobContextForTest(job Job) JobContext {
	return JobContext{
		Job:     job,
		Attempt: job.Attempt,
		Queue:   job.Queue,
		ctx:     context.Background(),
	}
}

// Worker is an OJS worker that fetches and processes jobs from an OJS server.
// It supports configurable concurrency, middleware, and graceful shutdown.
type Worker struct {
	transport *transport
	config    workerConfig
	workerID  string

	handlers   map[string]HandlerFunc
	handlersMu sync.RWMutex

	middleware *middlewareChain

	state     atomic.Value // WorkerState
	activeJobs sync.Map   // job ID -> struct{}
	activeCount atomic.Int64

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

	w := &Worker{
		transport:  newWorkerTransport(serverURL, cfg),
		config:     cfg,
		workerID:   generateWorkerID(),
		handlers:   make(map[string]HandlerFunc),
		middleware: newMiddlewareChain(),
		stopped:    make(chan struct{}),
	}
	w.state.Store(WorkerStateRunning)
	return w
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
	w.middleware.Add(fmt.Sprintf("middleware-%d", len(w.middleware.middleware)), fn)
}

// UseNamed adds a named execution middleware to the worker's middleware chain.
func (w *Worker) UseNamed(name string, fn MiddlewareFunc) {
	w.middleware.Add(name, fn)
}

// Start begins fetching and processing jobs. It blocks until the context
// is cancelled or the worker receives a shutdown signal.
//
// Example:
//
//	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
//	defer cancel()
//	if err := worker.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
func (w *Worker) Start(ctx context.Context) error {
	// Validate that at least one handler is registered.
	w.handlersMu.RLock()
	if len(w.handlers) == 0 {
		w.handlersMu.RUnlock()
		return fmt.Errorf("ojs: no handlers registered")
	}
	w.handlersMu.RUnlock()

	var wg sync.WaitGroup

	// Start heartbeat loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.heartbeatLoop(ctx)
	}()

	// Start fetch loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.fetchLoop(ctx)
	}()

	// Wait for context cancellation.
	<-ctx.Done()

	// Begin graceful shutdown.
	w.state.Store(WorkerStateTerminate)

	// Wait for grace period or all active jobs to complete.
	graceDone := make(chan struct{})
	go func() {
		w.waitForActiveJobs()
		close(graceDone)
	}()

	select {
	case <-graceDone:
		// All jobs completed within grace period.
	case <-time.After(w.config.gracePeriod):
		// Grace period expired. Active jobs will be recovered by visibility timeout.
	}

	w.stopOnce.Do(func() {
		close(w.stopped)
	})

	wg.Wait()
	return nil
}

// State returns the current worker lifecycle state.
func (w *Worker) State() WorkerState {
	return w.state.Load().(WorkerState)
}

// fetchLoop is the main loop that fetches and dispatches jobs.
func (w *Worker) fetchLoop(ctx context.Context) {
	sem := make(chan struct{}, w.config.concurrency)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopped:
			return
		default:
		}

		state := w.State()
		if state != WorkerStateRunning {
			// In quiet or terminate state, stop fetching.
			select {
			case <-ctx.Done():
				return
			case <-w.stopped:
				return
			case <-time.After(w.config.pollInterval):
				continue
			}
		}

		// Check if we have capacity.
		if w.activeCount.Load() >= int64(w.config.concurrency) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.config.pollInterval):
				continue
			}
		}

		// Fetch jobs from the server.
		count := w.config.concurrency - int(w.activeCount.Load())
		if count <= 0 {
			count = 1
		}
		jobs, err := w.fetchJobs(ctx, count)
		if err != nil {
			// On error, wait and retry.
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.config.pollInterval):
				continue
			}
		}

		if len(jobs) == 0 {
			// No jobs available, wait before polling again.
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.config.pollInterval):
				continue
			}
		}

		// Dispatch each fetched job to a goroutine.
		for _, job := range jobs {
			job := job // capture loop variable
			sem <- struct{}{}
			w.activeJobs.Store(job.ID, struct{}{})
			w.activeCount.Add(1)

			go func() {
				defer func() {
					<-sem
					w.activeJobs.Delete(job.ID)
					w.activeCount.Add(-1)
				}()
				w.processJob(ctx, job)
			}()
		}
	}
}

// processJob executes a single job through the middleware chain and handler.
func (w *Worker) processJob(ctx context.Context, job Job) {
	w.handlersMu.RLock()
	handler, ok := w.handlers[job.Type]
	w.handlersMu.RUnlock()

	if !ok {
		// No handler registered for this job type. NACK it.
		_ = w.nackJob(ctx, job.ID, "handler_error",
			fmt.Sprintf("no handler registered for job type %q", job.Type))
		return
	}

	ref := &jobResultRef{}
	jctx := JobContext{
		Job:       job,
		Attempt:   job.Attempt,
		Queue:     job.Queue,
		ctx:       ctx,
		resultRef: ref,
		worker:    w,
	}

	// Build the middleware chain around the handler.
	wrapped := w.middleware.then(handler)

	// Execute.
	err := wrapped(jctx)

	if err != nil {
		// Job failed. NACK it.
		_ = w.nackJob(ctx, job.ID, "handler_error", err.Error())
		return
	}

	// Job succeeded. ACK it.
	_ = w.ackJob(ctx, job.ID, ref.data)
}

// fetchJobs fetches jobs from the OJS server.
func (w *Worker) fetchJobs(ctx context.Context, count int) ([]Job, error) {
	req := struct {
		Queues   []string `json:"queues"`
		Count    int      `json:"count"`
		WorkerID string   `json:"worker_id"`
	}{
		Queues:   w.config.queues,
		Count:    count,
		WorkerID: w.workerID,
	}

	var resp struct {
		Jobs []Job `json:"jobs"`
	}
	if err := w.transport.post(ctx, basePath+"/workers/fetch", req, &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// ackJob acknowledges successful completion of a job.
func (w *Worker) ackJob(ctx context.Context, jobID string, result map[string]any) error {
	req := struct {
		JobID  string         `json:"job_id"`
		Result map[string]any `json:"result,omitempty"`
	}{
		JobID:  jobID,
		Result: result,
	}
	return w.transport.post(ctx, basePath+"/workers/ack", req, nil)
}

// nackJob reports job failure to the OJS server.
func (w *Worker) nackJob(ctx context.Context, jobID, code, message string) error {
	req := struct {
		JobID string `json:"job_id"`
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}{
		JobID: jobID,
	}
	req.Error.Code = code
	req.Error.Message = message
	req.Error.Retryable = true
	return w.transport.post(ctx, basePath+"/workers/nack", req, nil)
}

// heartbeatLoop sends periodic heartbeats to the OJS server.
func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.config.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopped:
			return
		case <-ticker.C:
			_ = w.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat sends a single heartbeat to the OJS server.
func (w *Worker) sendHeartbeat(ctx context.Context) error {
	activeJobIDs := w.getActiveJobIDs()

	req := struct {
		WorkerID   string   `json:"worker_id"`
		State      string   `json:"state"`
		ActiveJobs int      `json:"active_jobs"`
		ActiveJobIDs []string `json:"active_job_ids"`
	}{
		WorkerID:     w.workerID,
		State:        string(w.State()),
		ActiveJobs:   len(activeJobIDs),
		ActiveJobIDs: activeJobIDs,
	}

	var resp struct {
		State string `json:"state"`
	}
	if err := w.transport.post(ctx, basePath+"/workers/heartbeat", req, &resp); err != nil {
		// Heartbeat failures are non-fatal. Continue operating.
		return err
	}

	// Process server-directed state changes.
	if resp.State != "" {
		w.handleServerState(WorkerState(resp.State))
	}

	return nil
}

// handleServerState processes a state directive from the server.
func (w *Worker) handleServerState(desired WorkerState) {
	current := w.State()
	switch {
	case current == WorkerStateRunning && desired == WorkerStateQuiet:
		w.state.Store(WorkerStateQuiet)
	case current == WorkerStateRunning && desired == WorkerStateTerminate:
		w.state.Store(WorkerStateTerminate)
	case current == WorkerStateQuiet && desired == WorkerStateTerminate:
		w.state.Store(WorkerStateTerminate)
	case current == WorkerStateQuiet && desired == WorkerStateRunning:
		w.state.Store(WorkerStateRunning)
	}
	// Backward transitions from terminate are not allowed.
}

// getActiveJobIDs returns the IDs of all currently active jobs.
func (w *Worker) getActiveJobIDs() []string {
	var ids []string
	w.activeJobs.Range(func(key, _ any) bool {
		ids = append(ids, key.(string))
		return true
	})
	if ids == nil {
		ids = []string{}
	}
	return ids
}

// waitForActiveJobs blocks until all active jobs have completed.
func (w *Worker) waitForActiveJobs() {
	for w.activeCount.Load() > 0 {
		time.Sleep(100 * time.Millisecond)
	}
}

// generateWorkerID generates a unique worker identifier.
func generateWorkerID() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("worker_%s_%d_%d", hostname, os.Getpid(), time.Now().UnixNano())
}
