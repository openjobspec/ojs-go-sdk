package ojs

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

// --- Enqueue Options ---

func TestWithPriority(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{WithPriority(10)})
	if cfg.priority != 10 {
		t.Errorf("expected priority=10, got %d", cfg.priority)
	}
}

func TestWithTimeout(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{WithTimeout(30 * time.Second)})
	if cfg.timeoutMS != 30000 {
		t.Errorf("expected timeoutMS=30000, got %d", cfg.timeoutMS)
	}
}

func TestWithDelay(t *testing.T) {
	before := time.Now()
	cfg := resolveEnqueueConfig([]EnqueueOption{WithDelay(5 * time.Minute)})
	after := time.Now()

	if cfg.delayUntil == nil {
		t.Fatal("expected delayUntil to be set")
	}
	expected := before.Add(5 * time.Minute)
	if cfg.delayUntil.Before(expected.Add(-time.Second)) || cfg.delayUntil.After(after.Add(5*time.Minute+time.Second)) {
		t.Errorf("delayUntil %v is not approximately 5 minutes from now", cfg.delayUntil)
	}
}

func TestWithScheduledAt(t *testing.T) {
	scheduled := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	cfg := resolveEnqueueConfig([]EnqueueOption{WithScheduledAt(scheduled)})

	if cfg.delayUntil == nil {
		t.Fatal("expected delayUntil to be set")
	}
	if !cfg.delayUntil.Equal(scheduled) {
		t.Errorf("expected delayUntil=%v, got %v", scheduled, *cfg.delayUntil)
	}
}

func TestWithExpiresAt(t *testing.T) {
	expires := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	cfg := resolveEnqueueConfig([]EnqueueOption{WithExpiresAt(expires)})

	if cfg.expiresAt == nil {
		t.Fatal("expected expiresAt to be set")
	}
	if !cfg.expiresAt.Equal(expires) {
		t.Errorf("expected expiresAt=%v, got %v", expires, *cfg.expiresAt)
	}
}

func TestWithUnique(t *testing.T) {
	policy := UniquePolicy{
		Key:        []string{"email"},
		Period:     5 * time.Minute,
		OnConflict: "reject",
	}
	cfg := resolveEnqueueConfig([]EnqueueOption{WithUnique(policy)})

	if cfg.unique == nil {
		t.Fatal("expected unique policy to be set")
	}
	if cfg.unique.OnConflict != "reject" {
		t.Errorf("expected on_conflict=reject, got %s", cfg.unique.OnConflict)
	}
	if len(cfg.unique.Key) != 1 || cfg.unique.Key[0] != "email" {
		t.Errorf("expected key=[email], got %v", cfg.unique.Key)
	}
}

func TestWithTags(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithTags("onboarding", "email"),
		WithTags("priority"),
	})

	if len(cfg.tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(cfg.tags))
	}
	expected := []string{"onboarding", "email", "priority"}
	for i, tag := range expected {
		if cfg.tags[i] != tag {
			t.Errorf("expected tag[%d]=%s, got %s", i, tag, cfg.tags[i])
		}
	}
}

func TestWithMeta(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithMeta(map[string]any{"source": "api"}),
		WithMeta(map[string]any{"version": "2", "source": "override"}),
	})

	if cfg.meta == nil {
		t.Fatal("expected meta to be set")
	}
	if cfg.meta["source"] != "override" {
		t.Errorf("expected source=override, got %v", cfg.meta["source"])
	}
	if cfg.meta["version"] != "2" {
		t.Errorf("expected version=2, got %v", cfg.meta["version"])
	}
}

func TestWithVisibilityTimeout(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{WithVisibilityTimeout(2 * time.Minute)})
	if cfg.visibilityTimeout != 120000 {
		t.Errorf("expected visibilityTimeout=120000, got %d", cfg.visibilityTimeout)
	}
}

func TestResolveEnqueueConfigDefaults(t *testing.T) {
	cfg := resolveEnqueueConfig(nil)
	if cfg.queue != "default" {
		t.Errorf("expected default queue, got %s", cfg.queue)
	}
	if cfg.priority != 0 {
		t.Errorf("expected priority=0, got %d", cfg.priority)
	}
	if cfg.timeoutMS != 0 {
		t.Errorf("expected timeoutMS=0, got %d", cfg.timeoutMS)
	}
	if cfg.delayUntil != nil {
		t.Error("expected delayUntil=nil")
	}
	if cfg.retry != nil {
		t.Error("expected retry=nil")
	}
	if cfg.unique != nil {
		t.Error("expected unique=nil")
	}
	if cfg.tags != nil {
		t.Error("expected tags=nil")
	}
	if cfg.meta != nil {
		t.Error("expected meta=nil")
	}
}

// --- Worker Options ---

func TestWithWorkerAuth(t *testing.T) {
	cfg := resolveWorkerConfig([]WorkerOption{WithWorkerAuth("secret-token")})
	if cfg.authToken != "secret-token" {
		t.Errorf("expected authToken=secret-token, got %s", cfg.authToken)
	}
}

func TestWithWorkerHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 60 * time.Second}
	cfg := resolveWorkerConfig([]WorkerOption{WithWorkerHTTPClient(custom)})
	if cfg.httpClient != custom {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestWithLogger(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	cfg := resolveWorkerConfig([]WorkerOption{WithLogger(logger)})
	if cfg.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestResolveWorkerConfigDefaults(t *testing.T) {
	cfg := resolveWorkerConfig(nil)
	if cfg.concurrency != 10 {
		t.Errorf("expected concurrency=10, got %d", cfg.concurrency)
	}
	if cfg.gracePeriod != 25*time.Second {
		t.Errorf("expected gracePeriod=25s, got %v", cfg.gracePeriod)
	}
	if cfg.heartbeatInterval != 5*time.Second {
		t.Errorf("expected heartbeatInterval=5s, got %v", cfg.heartbeatInterval)
	}
	if cfg.pollInterval != 1*time.Second {
		t.Errorf("expected pollInterval=1s, got %v", cfg.pollInterval)
	}
	if len(cfg.queues) != 1 || cfg.queues[0] != "default" {
		t.Errorf("expected queues=[default], got %v", cfg.queues)
	}
	if cfg.logger != nil {
		t.Error("expected logger=nil by default")
	}
}

// --- NewJobContextForTest ---

func TestNewJobContextForTest(t *testing.T) {
	job := Job{
		ID:      "test-ctx-1",
		Type:    "email.send",
		Queue:   "email",
		Attempt: 2,
	}
	jc := NewJobContextForTest(job)

	if jc.Job.ID != "test-ctx-1" {
		t.Errorf("expected job ID=test-ctx-1, got %s", jc.Job.ID)
	}
	if jc.Job.Type != "email.send" {
		t.Errorf("expected job Type=email.send, got %s", jc.Job.Type)
	}
	if jc.Attempt != 2 {
		t.Errorf("expected Attempt=2, got %d", jc.Attempt)
	}
	if jc.Queue != "email" {
		t.Errorf("expected Queue=email, got %s", jc.Queue)
	}
	if jc.Context() == nil {
		t.Error("expected non-nil context")
	}
}

// --- Worker log helpers ---

func TestWorkerLogErrorWithLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	w := NewWorker("http://localhost:8080", WithLogger(logger))
	w.logError(nil, "test error message",
		slog.String("job.id", "j-1"),
		slog.String("detail", "something broke"),
	)

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output, got empty string")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test error message")) {
		t.Errorf("expected log to contain 'test error message', got %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("j-1")) {
		t.Errorf("expected log to contain job ID, got %s", output)
	}
}

func TestWorkerLogWarnWithLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	w := NewWorker("http://localhost:8080", WithLogger(logger))
	w.logWarn(nil, "test warning",
		slog.String("error", "connection refused"),
	)

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output, got empty string")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test warning")) {
		t.Errorf("expected log to contain 'test warning', got %s", output)
	}
}

func TestWorkerLogErrorWithoutLogger(t *testing.T) {
	w := NewWorker("http://localhost:8080")
	// Should not panic when logger is nil.
	w.logError(nil, "this should be a no-op")
	w.logWarn(nil, "this should also be a no-op")
}
