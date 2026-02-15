package otel

import (
	"context"
	"fmt"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracing_Success(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	mw := Tracing(WithTracerProvider(tp))

	handler := func(ctx ojs.JobContext) error { return nil }

	jctx := ojs.NewJobContextForTest(ojs.Job{
		ID:    "job-123",
		Type:  "email.send",
		Queue: "email",
	})
	jctx.Queue = "email"
	jctx.Attempt = 1

	err := mw(jctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "ojs.job email.send" {
		t.Errorf("span name = %q, want %q", span.Name, "ojs.job email.send")
	}

	assertAttr(t, span.Attributes, "ojs.job.type", "email.send")
	assertAttr(t, span.Attributes, "ojs.job.id", "job-123")
	assertAttr(t, span.Attributes, "ojs.job.queue", "email")
}

func TestTracing_Error(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())

	mw := Tracing(WithTracerProvider(tp))

	handler := func(ctx ojs.JobContext) error {
		return fmt.Errorf("processing failed")
	}

	jctx := ojs.NewJobContextForTest(ojs.Job{
		ID:   "job-456",
		Type: "data.process",
	})

	err := mw(jctx, handler)
	if err == nil {
		t.Fatal("expected error")
	}

	tp.ForceFlush(context.Background())
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected error status code, got %d", spans[0].Status.Code)
	}
	if len(spans[0].Events) == 0 {
		t.Error("expected error event to be recorded")
	}
}

func TestMetrics_Success(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(context.Background())

	mw := Metrics(WithMeterProvider(mp))

	handler := func(ctx ojs.JobContext) error { return nil }

	jctx := ojs.NewJobContextForTest(ojs.Job{
		ID:    "job-m1",
		Type:  "email.send",
		Queue: "default",
	})
	jctx.Queue = "default"

	err := mw(jctx, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	metrics := flattenMetrics(rm)
	assertMetricExists(t, metrics, "ojs.job.started")
	assertMetricExists(t, metrics, "ojs.job.completed")
	assertMetricExists(t, metrics, "ojs.job.duration")
}

func TestMetrics_Error(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer mp.Shutdown(context.Background())

	mw := Metrics(WithMeterProvider(mp))

	handler := func(ctx ojs.JobContext) error {
		return fmt.Errorf("failed")
	}

	jctx := ojs.NewJobContextForTest(ojs.Job{
		ID:   "job-m2",
		Type: "data.process",
	})

	err := mw(jctx, handler)
	if err == nil {
		t.Fatal("expected error")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	metrics := flattenMetrics(rm)
	assertMetricExists(t, metrics, "ojs.job.started")
	assertMetricExists(t, metrics, "ojs.job.failed")
	assertMetricExists(t, metrics, "ojs.job.duration")
}

func TestTracing_DefaultProvider(t *testing.T) {
	mw := Tracing()
	jctx := ojs.NewJobContextForTest(ojs.Job{Type: "test"})
	err := mw(jctx, func(ctx ojs.JobContext) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetrics_DefaultProvider(t *testing.T) {
	mw := Metrics()
	jctx := ojs.NewJobContextForTest(ojs.Job{Type: "test"})
	err := mw(jctx, func(ctx ojs.JobContext) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- helpers ---

func assertAttr(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			if a.Value.AsString() != expected {
				t.Errorf("attr %s = %q, want %q", key, a.Value.AsString(), expected)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func flattenMetrics(rm metricdata.ResourceMetrics) map[string]metricdata.Metrics {
	result := make(map[string]metricdata.Metrics)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			result[m.Name] = m
		}
	}
	return result
}

func assertMetricExists(t *testing.T, metrics map[string]metricdata.Metrics, name string) {
	t.Helper()
	if _, ok := metrics[name]; !ok {
		t.Errorf("metric %q not found in collected metrics", name)
	}
}
