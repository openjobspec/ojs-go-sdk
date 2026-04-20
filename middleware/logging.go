package middleware

import (
	"log/slog"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// Logging returns middleware that logs job execution using the provided
// [slog.Logger]. Each job execution produces two log entries: one at start
// (DEBUG level) and one at completion (INFO on success, ERROR on failure).
//
// Log attributes include job.type, job.id, job.queue, job.attempt, and
// duration_ms (on completion).
func Logging(logger *slog.Logger) ojs.MiddlewareFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		attrs := []slog.Attr{
			slog.String("job.type", ctx.Job.Type),
			slog.String("job.id", ctx.Job.ID),
			slog.String("job.queue", ctx.Queue),
			slog.Int("job.attempt", ctx.Attempt),
		}

		logger.LogAttrs(ctx.Context(), slog.LevelDebug, "job started", attrs...)

		start := time.Now()
		err := next(ctx)
		duration := time.Since(start)

		attrs = append(attrs, slog.Float64("duration_ms", float64(duration.Microseconds())/1000.0))

		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
			logger.LogAttrs(ctx.Context(), slog.LevelError, "job failed", attrs...)
		} else {
			logger.LogAttrs(ctx.Context(), slog.LevelInfo, "job completed", attrs...)
		}

		return err
	}
}
