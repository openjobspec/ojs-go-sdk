// Package main demonstrates OJS middleware usage for worker-side job processing.
//
// Middleware wraps job handlers in an onion model: first-registered middleware
// executes outermost. Each middleware calls next(ctx) to pass control inward.
//
// Execution order for [logging, metrics, tracing]:
//
//	logging.before → metrics.before → tracing.before → handler → tracing.after → metrics.after → logging.after
//
// Prerequisites:
//   - An OJS-compatible server running at http://localhost:8080
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
	"github.com/openjobspec/ojs-go-sdk/middleware"
)

// ---------------------------------------------------------------------------
// Custom metrics collector (simple in-memory example)
// ---------------------------------------------------------------------------

type inMemoryMetrics struct {
	completed atomic.Int64
	failed    atomic.Int64
}

func (m *inMemoryMetrics) JobStarted(jobType, queue string) {
	slog.Debug("metrics: job started", "type", jobType, "queue", queue)
}

func (m *inMemoryMetrics) JobCompleted(jobType, queue string, duration time.Duration) {
	m.completed.Add(1)
	slog.Info("metrics: job completed",
		"type", jobType, "queue", queue, "duration_ms", duration.Milliseconds(),
		"total_completed", m.completed.Load())
}

func (m *inMemoryMetrics) JobFailed(jobType, queue string, duration time.Duration) {
	m.failed.Add(1)
	slog.Warn("metrics: job failed",
		"type", jobType, "queue", queue, "duration_ms", duration.Milliseconds(),
		"total_failed", m.failed.Load())
}

// ---------------------------------------------------------------------------
// Custom trace-context middleware
// ---------------------------------------------------------------------------

// traceContext restores distributed trace context from job metadata.
// In production, this would integrate with OpenTelemetry or a similar library.
func traceContext() ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		traceID, _ := ctx.Job.Meta["trace_id"].(string)
		if traceID != "" {
			slog.Info("trace: restoring context", "trace_id", traceID, "job_id", ctx.Job.ID)
		} else {
			// Generate a trace ID if none was propagated.
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			traceID = fmt.Sprintf("%x", b)
			slog.Info("trace: generated new context", "trace_id", traceID, "job_id", ctx.Job.ID)
		}
		return next(ctx)
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	// ------------------------------------------------------------------
	// Client-side: inject trace context when enqueuing
	// ------------------------------------------------------------------
	// Note: The Go SDK does not have a client-side middleware chain.
	// Instead, wrap enqueue calls with a helper function.

	client, err := ojs.NewClient("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	// Helper that injects trace context before enqueuing.
	enqueueWithTrace := func(ctx context.Context, jobType string, args ojs.Args, opts ...ojs.EnqueueOption) (*ojs.Job, error) {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		traceID := fmt.Sprintf("%x", b)

		// Merge trace metadata into the job.
		opts = append(opts, ojs.WithMeta(map[string]any{
			"trace_id":        traceID,
			"enqueued_by":     "my-service",
			"enqueued_at_utc": time.Now().UTC().Format(time.RFC3339),
		}))

		slog.Info("enqueue: submitting job", "type", jobType, "trace_id", traceID)
		job, err := client.Enqueue(ctx, jobType, args, opts...)
		if err != nil {
			return nil, err
		}
		slog.Info("enqueue: job created", "id", job.ID, "state", job.State)
		return job, nil
	}

	bgCtx := context.Background()
	job, err := enqueueWithTrace(bgCtx, "email.send",
		ojs.Args{"to": "user@example.com", "subject": "Hello from middleware example"},
		ojs.WithQueue("email"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Enqueued job %s with trace metadata\n", job.ID)

	// ------------------------------------------------------------------
	// Worker-side: composable middleware chain
	// ------------------------------------------------------------------

	worker := ojs.NewWorker("http://localhost:8080",
		ojs.WithQueues("email", "default"),
		ojs.WithConcurrency(10),
		ojs.WithGracePeriod(25*time.Second),
	)

	// 1. Recovery middleware (innermost safety net — catches panics)
	worker.UseNamed("recovery", middleware.Recovery(logger))

	// 2. Logging middleware (structured logging of start/complete/fail)
	worker.UseNamed("logging", middleware.Logging(logger))

	// 3. Metrics middleware (duration tracking and counters)
	metrics := &inMemoryMetrics{}
	worker.UseNamed("metrics", middleware.Metrics(metrics))

	// 4. Trace-context middleware (restore distributed trace from job meta)
	worker.UseNamed("trace-context", traceContext())

	// ------------------------------------------------------------------
	// Register handlers
	// ------------------------------------------------------------------

	worker.Register("email.send", func(ctx ojs.JobContext) error {
		to, _ := ctx.Job.Args["to"].(string)
		subject, _ := ctx.Job.Args["subject"].(string)
		slog.Info("handler: sending email", "to", to, "subject", subject)
		time.Sleep(100 * time.Millisecond)

		ctx.SetResult(map[string]any{
			"messageId": fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"delivered": true,
		})
		return nil
	})

	worker.Register("report.generate", func(ctx ojs.JobContext) error {
		reportType, _ := ctx.Job.Args["type"].(string)
		slog.Info("handler: generating report", "report_type", reportType, "attempt", ctx.Attempt)

		for i := 0; i < 3; i++ {
			time.Sleep(200 * time.Millisecond)
			if err := ctx.Heartbeat(); err != nil {
				slog.Warn("heartbeat error", "error", err)
			}
		}

		ctx.SetResult(map[string]any{"path": "/reports/daily.csv"})
		return nil
	})

	// ------------------------------------------------------------------
	// Start worker (blocks until SIGTERM/SIGINT)
	// ------------------------------------------------------------------

	sigCtx, cancel := signal.NotifyContext(bgCtx, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	log.Println("Starting worker with middleware chain: recovery → logging → metrics → trace-context")
	if err := worker.Start(sigCtx); err != nil {
		log.Fatal(err)
	}
	log.Println("Worker stopped.")
}
