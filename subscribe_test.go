package ojs

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadSSEStreamSingleEvent(t *testing.T) {
	body := "event: job.completed\ndata: {\"id\":\"123\"}\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	var events []Event
	readSSEStream(context.Background(), resp, func(e Event) {
		events = append(events, e)
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "job.completed" {
		t.Errorf("expected type job.completed, got %s", events[0].Type)
	}
	raw := events[0].Data["raw"].(string)
	if raw != `{"id":"123"}` {
		t.Errorf("expected data {\"id\":\"123\"}, got %s", raw)
	}
}

func TestReadSSEStreamMultilineData(t *testing.T) {
	body := "event: job.updated\ndata: {\"id\":\"123\",\ndata: \"status\":\"completed\"}\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	var events []Event
	readSSEStream(context.Background(), resp, func(e Event) {
		events = append(events, e)
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	raw := events[0].Data["raw"].(string)
	expected := "{\"id\":\"123\",\n\"status\":\"completed\"}"
	if raw != expected {
		t.Errorf("expected multiline data %q, got %q", expected, raw)
	}
}

func TestReadSSEStreamCommentLinesIgnored(t *testing.T) {
	body := ": this is a comment\nevent: test\ndata: hello\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	var events []Event
	readSSEStream(context.Background(), resp, func(e Event) {
		events = append(events, e)
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data["raw"].(string) != "hello" {
		t.Errorf("expected 'hello', got %s", events[0].Data["raw"])
	}
}

func TestReadSSEStreamMultipleEvents(t *testing.T) {
	body := "event: a\ndata: first\n\nevent: b\ndata: second\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	var events []Event
	readSSEStream(context.Background(), resp, func(e Event) {
		events = append(events, e)
	})

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "a" || events[1].Type != "b" {
		t.Errorf("expected types a,b got %s,%s", events[0].Type, events[1].Type)
	}
}

func TestReadSSEStreamContextCancellation(t *testing.T) {
	// Infinite stream that should be cancelled
	body := "event: tick\ndata: 1\n\nevent: tick\ndata: 2\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var count int

	done := make(chan struct{})
	go func() {
		readSSEStream(ctx, resp, func(e Event) {
			mu.Lock()
			count++
			if count >= 1 {
				cancel()
			}
			mu.Unlock()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readSSEStream did not stop after context cancellation")
	}
}

func TestReadSSEStreamEmptyDataField(t *testing.T) {
	body := "event: ping\ndata\n\n"
	resp := &http.Response{
		Body: readCloserFrom(body),
	}

	var events []Event
	readSSEStream(context.Background(), resp, func(e Event) {
		events = append(events, e)
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data["raw"].(string) != "" {
		t.Errorf("expected empty data, got %q", events[0].Data["raw"])
	}
}

// readCloserFrom creates an io.ReadCloser from a string for testing.
func readCloserFrom(s string) *readCloserString {
	return &readCloserString{Reader: strings.NewReader(s)}
}

type readCloserString struct {
	*strings.Reader
}

func (r *readCloserString) Close() error { return nil }
