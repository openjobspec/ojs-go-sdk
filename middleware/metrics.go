package middleware

import (
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// MetricsRecorder is the interface for recording job execution metrics.
// Implement this interface to connect any metrics backend (Prometheus,
// OpenTelemetry, StatsD, etc.).
type MetricsRecorder interface {
	// JobStarted is called when a job begins execution.
	JobStarted(jobType, queue string)

	// JobCompleted is called when a job finishes successfully.
	JobCompleted(jobType, queue string, duration time.Duration)

	// JobFailed is called when a job finishes with an error.
	JobFailed(jobType, queue string, duration time.Duration)
}

// Metrics returns middleware that records job execution metrics via the
// provided [MetricsRecorder]. It tracks job starts, completions, failures,
// and execution duration.
func Metrics(recorder MetricsRecorder) ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		recorder.JobStarted(ctx.Job.Type, ctx.Queue)

		start := time.Now()
		err := next(ctx)
		duration := time.Since(start)

		if err != nil {
			recorder.JobFailed(ctx.Job.Type, ctx.Queue, duration)
		} else {
			recorder.JobCompleted(ctx.Job.Type, ctx.Queue, duration)
		}

		return err
	}
}
