package otel

import (
	"fmt"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/openjobspec/ojs-go-sdk/middleware/otel"

// --- Options ---

// Option configures the OTel middleware.
type Option func(*config)

type config struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// WithTracerProvider sets a custom TracerProvider. Defaults to the global provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tracerProvider = tp }
}

// WithMeterProvider sets a custom MeterProvider. Defaults to the global provider.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) { c.meterProvider = mp }
}

// --- Tracing Middleware ---

// Tracing returns middleware that creates a span for every job execution.
//
// Span attributes include:
//   - ojs.job.type
//   - ojs.job.id
//   - ojs.job.queue
//   - ojs.job.attempt
//
// On error, the span status is set to Error with the error message recorded.
func Tracing(opts ...Option) ojs.MiddlewareFunc {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	tp := cfg.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	tracer := tp.Tracer(instrumentationName)

	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		spanCtx, span := tracer.Start(ctx.Context(),
			fmt.Sprintf("ojs.job %s", ctx.Job.Type),
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(jobAttributes(ctx)...),
		)
		_ = spanCtx // span carries its own context; JobContext.ctx is not reassignable
		defer span.End()

		err := next(ctx)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// --- Metrics Middleware ---

// Metrics returns middleware that records job execution metrics via OTel.
//
// Recorded instruments:
//   - ojs.job.started (counter): incremented when a job begins
//   - ojs.job.completed (counter): incremented on success
//   - ojs.job.failed (counter): incremented on failure
//   - ojs.job.duration (histogram, milliseconds): execution duration
func Metrics(opts ...Option) ojs.MiddlewareFunc {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	mp := cfg.meterProvider
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(instrumentationName)

	jobStarted, _ := meter.Int64Counter("ojs.job.started",
		metric.WithDescription("Number of jobs started"),
	)
	jobCompleted, _ := meter.Int64Counter("ojs.job.completed",
		metric.WithDescription("Number of jobs completed successfully"),
	)
	jobFailed, _ := meter.Int64Counter("ojs.job.failed",
		metric.WithDescription("Number of jobs that failed"),
	)
	jobDuration, _ := meter.Float64Histogram("ojs.job.duration",
		metric.WithDescription("Job execution duration in milliseconds"),
		metric.WithUnit("ms"),
	)

	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		attrs := metric.WithAttributes(
			attribute.String("ojs.job.type", ctx.Job.Type),
			attribute.String("ojs.job.queue", ctx.Queue),
		)

		jobStarted.Add(ctx.Context(), 1, attrs)

		start := time.Now()
		err := next(ctx)
		durationMS := float64(time.Since(start).Microseconds()) / 1000.0

		jobDuration.Record(ctx.Context(), durationMS, attrs)

		if err != nil {
			jobFailed.Add(ctx.Context(), 1, attrs)
		} else {
			jobCompleted.Add(ctx.Context(), 1, attrs)
		}

		return err
	}
}

// jobAttributes returns the standard OTel attributes for a job.
func jobAttributes(ctx ojs.JobContext) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("ojs.job.type", ctx.Job.Type),
		attribute.String("ojs.job.id", ctx.Job.ID),
		attribute.String("ojs.job.queue", ctx.Queue),
		attribute.Int("ojs.job.attempt", ctx.Attempt),
	}
}
