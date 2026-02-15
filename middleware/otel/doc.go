// Package otel provides OpenTelemetry middleware for OJS workers.
//
// This package integrates with the OpenTelemetry SDK to provide distributed
// tracing and metrics for job processing. It creates a span per job execution
// and records metrics via the OTel metrics API.
//
// # Tracing
//
// The [Tracing] middleware creates a span for every job execution with
// standard OJS attributes (job.type, job.id, job.queue, job.attempt):
//
//	worker.UseNamed("tracing", otel.Tracing(
//	    otel.WithTracerProvider(tp),
//	))
//
// # Metrics
//
// The [Metrics] middleware records job execution counters and duration
// histograms via the OTel metrics API:
//
//	worker.UseNamed("metrics", otel.Metrics(
//	    otel.WithMeterProvider(mp),
//	))
package otel
