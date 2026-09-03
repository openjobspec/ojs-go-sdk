package otel

import (
	"context"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestTracingPropagatesSpanContextToHandler is a regression test: the job span
// context was created and then discarded, so any span a handler started became
// a disconnected root instead of a child of the job span.
func TestTracingPropagatesSpanContextToHandler(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	mw := Tracing(WithTracerProvider(tp))

	var childSpanCtx trace.SpanContext
	handler := func(jc ojs.JobContext) error {
		// A handler-created span must land under the job span.
		_, span := tp.Tracer("test").Start(jc.Context(), "handler.work")
		childSpanCtx = span.SpanContext()
		span.End()
		return nil
	}

	jctx := ojs.NewJobContextForTest(ojs.Job{ID: "job-1", Type: "email.send", Queue: "email"})
	if err := mw(jctx, handler); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("exported %d spans, want 2 (job span + handler span)", len(spans))
	}

	var jobSpan, childSpan tracetest.SpanStub
	for _, s := range spans {
		switch s.Name {
		case "handler.work":
			childSpan = s
		default:
			jobSpan = s
		}
	}

	if !childSpan.SpanContext.IsValid() {
		t.Fatal("handler span was not recorded")
	}
	if childSpan.Parent.SpanID() != jobSpan.SpanContext.SpanID() {
		t.Errorf("handler span parent = %s, want the job span %s",
			childSpan.Parent.SpanID(), jobSpan.SpanContext.SpanID())
	}
	if childSpanCtx.TraceID() != jobSpan.SpanContext.TraceID() {
		t.Errorf("handler span trace = %s, want the job trace %s",
			childSpanCtx.TraceID(), jobSpan.SpanContext.TraceID())
	}
}

// TestTracingHandlerSeesLiveSpan verifies the handler can annotate the job span
// through the propagated context.
func TestTracingHandlerSeesLiveSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	mw := Tracing(WithTracerProvider(tp))

	var recording bool
	handler := func(jc ojs.JobContext) error {
		recording = trace.SpanFromContext(jc.Context()).IsRecording()
		return nil
	}

	jctx := ojs.NewJobContextForTest(ojs.Job{ID: "job-1", Type: "a.job"})
	if err := mw(jctx, handler); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
	if !recording {
		t.Error("handler context must carry the live job span")
	}
}
