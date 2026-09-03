package ojs

import "context"

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
	if jc.ctx == nil {
		return context.Background()
	}
	return jc.ctx
}

// WithContext returns a copy of the JobContext carrying the provided context.
//
// Middleware uses this to propagate a derived context (for example an
// OpenTelemetry span context, or a context with a deadline) to the wrapped
// handler. The returned JobContext shares the same result reference and worker,
// so SetResult and Heartbeat continue to work.
//
// It panics if ctx is nil, matching the convention of the standard library.
func (jc JobContext) WithContext(ctx context.Context) JobContext {
	if ctx == nil {
		panic("ojs: nil context passed to JobContext.WithContext")
	}
	jc.ctx = ctx
	return jc
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
	return jc.worker.sendHeartbeat(jc.Context())
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
