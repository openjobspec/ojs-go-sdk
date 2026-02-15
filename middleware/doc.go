// Package middleware provides pre-built middleware for OJS workers.
//
// All middleware in this package follows the OJS MiddlewareFunc signature
// and can be added to a worker via [ojs.Worker.Use] or [ojs.Worker.UseNamed].
//
// # Logging
//
// The [Logging] middleware emits structured log entries via [log/slog] for every
// job execution, including job type, ID, attempt, duration, and error (if any):
//
//	worker.UseNamed("logging", middleware.Logging(slog.Default()))
//
// # Recovery
//
// The [Recovery] middleware catches panics in downstream handlers and converts
// them to errors, preventing a single job from crashing the worker:
//
//	worker.UseNamed("recovery", middleware.Recovery(slog.Default()))
//
// # Metrics
//
// The [Metrics] middleware reports job execution metrics via the [MetricsRecorder]
// interface. Implement this interface to connect your preferred metrics backend
// (Prometheus, OpenTelemetry, StatsD, etc.):
//
//	worker.UseNamed("metrics", middleware.Metrics(myPrometheusRecorder))
package middleware
