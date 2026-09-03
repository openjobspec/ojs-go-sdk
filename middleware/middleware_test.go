package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func newTestContext(jobType, jobID, queue string, attempt int) ojs.JobContext {
	return ojs.NewJobContextForTest(ojs.Job{
		ID:      jobID,
		Type:    jobType,
		Queue:   queue,
		Attempt: attempt,
	})
}

// --- Logging tests ---

func TestLogging_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mw := Logging(logger)
	ctx := newTestContext("email.send", "j-1", "email", 1)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %s", len(lines), output)
	}

	// Check start log.
	var startEntry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &startEntry); err != nil {
		t.Fatalf("failed to parse start log: %v", err)
	}
	if startEntry["msg"] != "job started" {
		t.Errorf("expected 'job started', got %v", startEntry["msg"])
	}
	if startEntry["job.type"] != "email.send" {
		t.Errorf("expected job.type=email.send, got %v", startEntry["job.type"])
	}

	// Check completion log.
	var endEntry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &endEntry); err != nil {
		t.Fatalf("failed to parse end log: %v", err)
	}
	if endEntry["msg"] != "job completed" {
		t.Errorf("expected 'job completed', got %v", endEntry["msg"])
	}
	if endEntry["level"] != "INFO" {
		t.Errorf("expected level=INFO, got %v", endEntry["level"])
	}
	if _, ok := endEntry["duration_ms"]; !ok {
		t.Error("expected duration_ms in completion log")
	}
}

func TestLogging_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mw := Logging(logger)
	ctx := newTestContext("email.send", "j-2", "default", 2)

	handlerErr := errors.New("smtp connection failed")
	err := mw(ctx, func(ctx ojs.JobContext) error {
		return handlerErr
	})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	var endEntry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &endEntry); err != nil {
		t.Fatalf("failed to parse end log: %v", err)
	}
	if endEntry["msg"] != "job failed" {
		t.Errorf("expected 'job failed', got %v", endEntry["msg"])
	}
	if endEntry["level"] != "ERROR" {
		t.Errorf("expected level=ERROR, got %v", endEntry["level"])
	}
	if endEntry["error"] != "smtp connection failed" {
		t.Errorf("expected error message, got %v", endEntry["error"])
	}
}

func TestLogging_NilLogger(t *testing.T) {
	// Should not panic with nil logger.
	mw := Logging(nil)
	ctx := newTestContext("test", "j-3", "default", 1)
	err := mw(ctx, func(ctx ojs.JobContext) error { return nil })
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- Recovery tests ---

func TestRecovery_NoPanic(t *testing.T) {
	mw := Recovery(nil)
	ctx := newTestContext("test", "j-4", "default", 1)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRecovery_PanicString(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mw := Recovery(logger)
	ctx := newTestContext("test.panic", "j-5", "default", 1)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		panic("something went wrong")
	})
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("expected panic message in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "test.panic") {
		t.Errorf("expected job type in error, got %v", err)
	}

	// Should have logged the panic.
	if !strings.Contains(buf.String(), "job panicked") {
		t.Error("expected panic log entry")
	}
}

func TestRecovery_PanicError(t *testing.T) {
	mw := Recovery(nil)
	ctx := newTestContext("test", "j-6", "default", 1)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		panic(fmt.Errorf("runtime error"))
	})
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !strings.Contains(err.Error(), "runtime error") {
		t.Errorf("expected panic message, got %v", err)
	}
}

func TestRecovery_HandlerError(t *testing.T) {
	mw := Recovery(nil)
	ctx := newTestContext("test", "j-7", "default", 1)
	handlerErr := errors.New("handler failed")

	err := mw(ctx, func(ctx ojs.JobContext) error {
		return handlerErr
	})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got %v", err)
	}
}

// --- Metrics tests ---

type testRecorder struct {
	mu        sync.Mutex
	started   []metricsEvent
	completed []metricsEvent
	failed    []metricsEvent
}

type metricsEvent struct {
	jobType  string
	queue    string
	duration time.Duration
}

func (r *testRecorder) JobStarted(jobType, queue string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, metricsEvent{jobType: jobType, queue: queue})
}

func (r *testRecorder) JobCompleted(jobType, queue string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completed = append(r.completed, metricsEvent{jobType: jobType, queue: queue, duration: duration})
}

func (r *testRecorder) JobFailed(jobType, queue string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, metricsEvent{jobType: jobType, queue: queue, duration: duration})
}

func TestMetrics_Success(t *testing.T) {
	rec := &testRecorder{}
	mw := Metrics(rec)
	ctx := newTestContext("email.send", "j-8", "email", 1)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		time.Sleep(1 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.started) != 1 {
		t.Fatalf("expected 1 started event, got %d", len(rec.started))
	}
	if rec.started[0].jobType != "email.send" {
		t.Errorf("expected jobType=email.send, got %s", rec.started[0].jobType)
	}
	if rec.started[0].queue != "email" {
		t.Errorf("expected queue=email, got %s", rec.started[0].queue)
	}

	if len(rec.completed) != 1 {
		t.Fatalf("expected 1 completed event, got %d", len(rec.completed))
	}
	if rec.completed[0].duration < 1*time.Millisecond {
		t.Errorf("expected duration >= 1ms, got %v", rec.completed[0].duration)
	}

	if len(rec.failed) != 0 {
		t.Errorf("expected 0 failed events, got %d", len(rec.failed))
	}
}

func TestMetrics_Failure(t *testing.T) {
	rec := &testRecorder{}
	mw := Metrics(rec)
	ctx := newTestContext("data.process", "j-9", "default", 3)

	err := mw(ctx, func(ctx ojs.JobContext) error {
		return errors.New("processing failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.started) != 1 {
		t.Fatalf("expected 1 started event, got %d", len(rec.started))
	}
	if len(rec.failed) != 1 {
		t.Fatalf("expected 1 failed event, got %d", len(rec.failed))
	}
	if rec.failed[0].jobType != "data.process" {
		t.Errorf("expected jobType=data.process, got %s", rec.failed[0].jobType)
	}
	if len(rec.completed) != 0 {
		t.Errorf("expected 0 completed events, got %d", len(rec.completed))
	}
}
