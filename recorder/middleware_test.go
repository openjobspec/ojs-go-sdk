package recorder

import (
	"context"
	"errors"
	"testing"
)

func TestMiddleware_RecordsSuccess(t *testing.T) {
	rec := New()
	fn := func(_ context.Context, args any) (any, error) {
		return "ok", nil
	}
	wrapped := Middleware(rec, fn)
	result, err := wrapped(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
	trace := rec.Trace()
	if len(trace) != 1 {
		t.Fatalf("Trace = %d, want 1", len(trace))
	}
	if trace[0].Error != "" {
		t.Errorf("Error = %q, want empty", trace[0].Error)
	}
}

func TestMiddleware_RecordsError(t *testing.T) {
	rec := New()
	fn := func(_ context.Context, args any) (any, error) {
		return nil, errors.New("boom")
	}
	wrapped := Middleware(rec, fn)
	_, err := wrapped(context.Background(), "input")
	if err == nil {
		t.Fatal("expected error")
	}
	trace := rec.Trace()
	if len(trace) != 1 {
		t.Fatalf("Trace = %d, want 1", len(trace))
	}
	if trace[0].Error != "boom" {
		t.Errorf("Error = %q, want boom", trace[0].Error)
	}
}

func TestMiddleware_MultipleInvocations(t *testing.T) {
	rec := New()
	counter := 0
	fn := func(_ context.Context, args any) (any, error) {
		counter++
		return counter, nil
	}
	wrapped := Middleware(rec, fn)
	for i := 0; i < 5; i++ {
		wrapped(context.Background(), i)
	}
	trace := rec.Trace()
	if len(trace) != 5 {
		t.Errorf("Trace = %d, want 5", len(trace))
	}
}

func TestTypedMiddleware_PreservesTypes(t *testing.T) {
	rec := New()
	fn := func(_ context.Context, n int) (string, error) {
		return "result", nil
	}
	wrapped := TypedMiddleware[int, string](rec, fn)
	// The returned function has signature func(context.Context, int) (string, error)
	result, err := wrapped(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if result != "result" {
		t.Errorf("result = %q, want result", result)
	}
	trace := rec.Trace()
	if len(trace) != 1 {
		t.Fatalf("Trace = %d, want 1", len(trace))
	}
	if trace[0].Args != "42" {
		t.Errorf("Args = %q, want 42", trace[0].Args)
	}
}

func TestTypedMiddleware_RecordsError(t *testing.T) {
	rec := New()
	fn := func(_ context.Context, s string) (int, error) {
		return 0, errors.New("typed boom")
	}
	wrapped := TypedMiddleware[string, int](rec, fn)
	_, err := wrapped(context.Background(), "input")
	if err == nil {
		t.Fatal("expected error")
	}
	trace := rec.Trace()
	if len(trace) != 1 {
		t.Fatalf("Trace = %d, want 1", len(trace))
	}
	if trace[0].Error != "typed boom" {
		t.Errorf("Error = %q, want typed boom", trace[0].Error)
	}
}
