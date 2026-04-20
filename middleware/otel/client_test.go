package otel

import (
	"context"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingWithPropagation_ExtractsContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	mw := TracingWithPropagation(WithTracerProvider(tp))

	// Simulate a job with propagated trace context in metadata.
	job := ojs.Job{
		ID:    "job-prop-1",
		Type:  "email.send",
		Queue: "email",
		Meta: map[string]any{
			"otel_context": map[string]any{
				"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
		},
	}

	jctx := ojs.NewJobContextForTest(job)
	jctx.Queue = "email"
	jctx.Attempt = 1

	err := mw(jctx, func(ctx ojs.JobContext) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "ojs.process email.send" {
		t.Errorf("span name = %q, want %q", span.Name, "ojs.process email.send")
	}

	assertAttr(t, span.Attributes, "ojs.job.type", "email.send")
	assertAttr(t, span.Attributes, "ojs.job.id", "job-prop-1")
}

func TestTracingWithPropagation_NoContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	mw := TracingWithPropagation(WithTracerProvider(tp))

	// Job with no trace context in metadata.
	jctx := ojs.NewJobContextForTest(ojs.Job{
		ID:   "job-noprop",
		Type: "data.process",
	})

	err := mw(jctx, func(ctx ojs.JobContext) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Name != "ojs.process data.process" {
		t.Errorf("unexpected span name: %s", spans[0].Name)
	}
}
