package middleware

import (
	"fmt"
	"log/slog"
	"runtime"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// Recovery returns middleware that recovers from panics in downstream
// handlers and converts them to errors. This prevents a single panicking
// job from crashing the entire worker process.
//
// If a logger is provided, the panic value and stack trace are logged
// at ERROR level. Pass nil to disable panic logging.
func Recovery(logger *slog.Logger) ojs.MiddlewareFunc {
	return func(ctx ojs.JobContext, next ojs.HandlerFunc) (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)

				if logger != nil {
					logger.LogAttrs(ctx.Context(), slog.LevelError, "job panicked",
						slog.String("job.type", ctx.Job.Type),
						slog.String("job.id", ctx.Job.ID),
						slog.Any("panic", r),
						slog.String("stack", string(buf[:n])),
					)
				}

				retErr = fmt.Errorf("panic in job %s (id=%s): %v", ctx.Job.Type, ctx.Job.ID, r)
			}
		}()
		return next(ctx)
	}
}
