package otel

import (
	"context"
	"fmt"

	ojs "github.com/openjobspec/ojs-go-sdk"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracedClient wraps an ojs.Client with OpenTelemetry instrumentation.
// Every Enqueue, EnqueueBatch, GetJob, and CancelJob call creates a span
// and injects trace context into the job's metadata for end-to-end tracing.
type TracedClient struct {
	inner  *ojs.Client
	tracer trace.Tracer
	prop   propagation.TextMapPropagator
}

// NewTracedClient wraps an existing client with tracing instrumentation.
func NewTracedClient(client *ojs.Client, opts ...Option) *TracedClient {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	tp := cfg.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return &TracedClient{
		inner:  client,
		tracer: tp.Tracer(instrumentationName),
		prop:   otel.GetTextMapPropagator(),
	}
}

// Enqueue submits a job with tracing. Trace context is propagated via
// the job's metadata so the consumer span can be linked to this producer span.
func (c *TracedClient) Enqueue(ctx context.Context, jobType string, args ojs.Args, opts ...ojs.EnqueueOption) (*ojs.Job, error) {
	ctx, span := c.tracer.Start(ctx,
		fmt.Sprintf("ojs.enqueue %s", jobType),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("ojs.job.type", jobType),
		),
	)
	defer span.End()

	// Inject trace context into metadata for propagation to the worker.
	carrier := make(propagation.MapCarrier)
	c.prop.Inject(ctx, carrier)
	traceMeta := make(map[string]any, len(carrier))
	for _, k := range carrier.Keys() {
		traceMeta[k] = carrier.Get(k)
	}
	if len(traceMeta) > 0 {
		opts = append(opts, ojs.WithMeta(map[string]any{
			"otel_context": traceMeta,
		}))
	}

	job, err := c.inner.Enqueue(ctx, jobType, args, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.String("ojs.job.id", job.ID))
	span.SetStatus(codes.Ok, "")
	return job, nil
}

// EnqueueBatch submits multiple jobs with tracing.
func (c *TracedClient) EnqueueBatch(ctx context.Context, requests []ojs.JobRequest) ([]ojs.Job, error) {
	ctx, span := c.tracer.Start(ctx,
		"ojs.enqueue_batch",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.Int("ojs.batch.size", len(requests)),
		),
	)
	defer span.End()

	jobs, err := c.inner.EnqueueBatch(ctx, requests)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.Int("ojs.batch.count", len(jobs)))
	span.SetStatus(codes.Ok, "")
	return jobs, nil
}

// GetJob retrieves a job with tracing.
func (c *TracedClient) GetJob(ctx context.Context, id string) (*ojs.Job, error) {
	ctx, span := c.tracer.Start(ctx,
		"ojs.get_job",
		trace.WithAttributes(attribute.String("ojs.job.id", id)),
	)
	defer span.End()

	job, err := c.inner.GetJob(ctx, id)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return job, nil
}

// CancelJob cancels a job with tracing.
func (c *TracedClient) CancelJob(ctx context.Context, id string) (*ojs.Job, error) {
	ctx, span := c.tracer.Start(ctx,
		"ojs.cancel_job",
		trace.WithAttributes(attribute.String("ojs.job.id", id)),
	)
	defer span.End()

	job, err := c.inner.CancelJob(ctx, id)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return job, nil
}

// Unwrap returns the underlying ojs.Client.
func (c *TracedClient) Unwrap() *ojs.Client {
	return c.inner
}

// --- Context Propagation for Worker ---

// TracingWithPropagation returns worker middleware that extracts propagated
// trace context from the job's metadata. This links the consumer span to the
// producer span, creating an end-to-end distributed trace:
//
//	Go producer (enqueue) → OJS server → Python worker (process)
//
// Use this instead of Tracing() when you want cross-service trace linking.
func TracingWithPropagation(opts ...Option) ojs.MiddlewareFunc {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	tp := cfg.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	tracer := tp.Tracer(instrumentationName)
	prop := otel.GetTextMapPropagator()

	return func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		parentCtx := ctx.Context()

		// Extract trace context from job metadata if present.
		if ctx.Job.Meta != nil {
			if otelCtxRaw, ok := ctx.Job.Meta["otel_context"]; ok {
				if otelCtxMap, ok := otelCtxRaw.(map[string]any); ok {
					carrier := make(propagation.MapCarrier)
					for k, v := range otelCtxMap {
						if s, ok := v.(string); ok {
							carrier.Set(k, s)
						}
					}
					parentCtx = prop.Extract(parentCtx, carrier)
				}
			}
		}

		_, span := tracer.Start(parentCtx,
			fmt.Sprintf("ojs.process %s", ctx.Job.Type),
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(jobAttributes(ctx)...),
		)
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
