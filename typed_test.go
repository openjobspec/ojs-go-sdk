package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type testEmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
}

func TestRegisterTyped_Success(t *testing.T) {
	var receivedTo, receivedSubject string
	var ackReceived atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if ackReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "typed-1", "type": "email.send", "state": "active",
							"args":  []any{map[string]any{"to": "test@example.com", "subject": "Hello"}},
							"queue": "default", "attempt": 1, "max_attempts": 3,
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/ack":
			ackReceived.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})
		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)

	RegisterTyped(worker, "email.send", func(ctx JobContext, args testEmailArgs) error {
		receivedTo = args.To
		receivedSubject = args.Subject
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if ackReceived.Load() < 1 {
		t.Fatal("expected ACK to be sent")
	}
	if receivedTo != "test@example.com" {
		t.Errorf("expected to=test@example.com, got %s", receivedTo)
	}
	if receivedSubject != "Hello" {
		t.Errorf("expected subject=Hello, got %s", receivedSubject)
	}
}

func TestRegisterTyped_HandlerError(t *testing.T) {
	var nackReceived atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		switch r.URL.Path {
		case "/ojs/v1/workers/fetch":
			if nackReceived.Load() == 0 {
				json.NewEncoder(w).Encode(map[string]any{
					"jobs": []map[string]any{
						{
							"id": "typed-2", "type": "email.send", "state": "active",
							"args":  []any{map[string]any{"to": "test@example.com"}},
							"queue": "default", "attempt": 1, "max_attempts": 3,
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
			}
		case "/ojs/v1/workers/nack":
			nackReceived.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})
		case "/ojs/v1/workers/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"state": "running"})
		}
	}))
	defer server.Close()

	worker := NewWorker(server.URL,
		WithConcurrency(1),
		WithPollInterval(50*time.Millisecond),
		WithHeartbeatInterval(100*time.Millisecond),
	)

	RegisterTyped(worker, "email.send", func(ctx JobContext, args testEmailArgs) error {
		return fmt.Errorf("send failed")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = worker.Start(ctx)

	if nackReceived.Load() < 1 {
		t.Fatal("expected NACK to be sent for handler error")
	}
}

func TestRegisterTyped_Unit(t *testing.T) {
	// Test the args unmarshaling directly without a full worker lifecycle.
	worker := NewWorker("http://localhost:9999")

	var got testEmailArgs
	RegisterTyped(worker, "email.send", func(ctx JobContext, args testEmailArgs) error {
		got = args
		return nil
	})

	// Simulate what processJob does: look up handler, call it.
	worker.handlersMu.RLock()
	handler := worker.handlers["email.send"]
	worker.handlersMu.RUnlock()

	ctx := NewJobContextForTest(Job{
		Type: "email.send",
		Args: Args{"to": "unit@test.com", "subject": "Test"},
	})

	err := handler(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.To != "unit@test.com" {
		t.Errorf("expected to=unit@test.com, got %s", got.To)
	}
	if got.Subject != "Test" {
		t.Errorf("expected subject=Test, got %s", got.Subject)
	}
}

func TestRegisterTyped_NilArgs(t *testing.T) {
	// Job with nil/empty args should still work with a zero-value struct.
	worker := NewWorker("http://localhost:9999")

	var called bool
	RegisterTyped(worker, "test.empty", func(ctx JobContext, args testEmailArgs) error {
		called = true
		return nil
	})

	worker.handlersMu.RLock()
	handler := worker.handlers["test.empty"]
	worker.handlersMu.RUnlock()

	ctx := NewJobContextForTest(Job{
		Type: "test.empty",
		Args: Args{},
	})

	err := handler(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

type testNestedArgs struct {
	User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	Priority int `json:"priority"`
}

func TestRegisterTyped_NestedStruct(t *testing.T) {
	worker := NewWorker("http://localhost:9999")

	var got testNestedArgs
	RegisterTyped(worker, "user.create", func(ctx JobContext, args testNestedArgs) error {
		got = args
		return nil
	})

	worker.handlersMu.RLock()
	handler := worker.handlers["user.create"]
	worker.handlersMu.RUnlock()

	ctx := NewJobContextForTest(Job{
		Type: "user.create",
		Args: Args{
			"user":     map[string]any{"name": "Alice", "email": "alice@example.com"},
			"priority": float64(5), // JSON numbers are float64
		},
	})

	err := handler(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.User.Name != "Alice" {
		t.Errorf("expected user.name=Alice, got %s", got.User.Name)
	}
	if got.Priority != 5 {
		t.Errorf("expected priority=5, got %d", got.Priority)
	}
}
