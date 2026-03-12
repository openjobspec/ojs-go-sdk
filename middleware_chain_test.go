package ojs

import (
	"errors"
	"strings"
	"testing"
)

func makeTrackerMiddleware(name string, trace *[]string) MiddlewareFunc {
	return func(ctx JobContext, next HandlerFunc) error {
		*trace = append(*trace, name+":before")
		err := next(ctx)
		*trace = append(*trace, name+":after")
		return err
	}
}

func dummyHandler(ctx JobContext) error {
	return nil
}

func failingHandler(ctx JobContext) error {
	return errors.New("handler failed")
}

func TestMiddlewareChain_Add(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Add("b", makeTrackerMiddleware("b", &trace))
	chain.Add("c", makeTrackerMiddleware("c", &trace))

	h := chain.then(dummyHandler)
	if err := h(JobContext{}); err != nil {
		t.Fatal(err)
	}

	// Onion model: a wraps b wraps c wraps handler
	expected := "a:before,b:before,c:before,c:after,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("execution order:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestMiddlewareChain_Prepend(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("b", makeTrackerMiddleware("b", &trace))
	chain.Prepend("a", makeTrackerMiddleware("a", &trace))

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,b:before,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("execution order:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestMiddlewareChain_InsertBefore(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Add("c", makeTrackerMiddleware("c", &trace))
	chain.InsertBefore("c", "b", makeTrackerMiddleware("b", &trace))

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,b:before,c:before,c:after,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("execution order:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestMiddlewareChain_InsertBefore_NonExistent(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	// InsertBefore with non-existent anchor falls back to Add
	chain.InsertBefore("nonexistent", "b", makeTrackerMiddleware("b", &trace))

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,b:before,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("expected fallback to Add, got: %s", got)
	}
}

func TestMiddlewareChain_InsertAfter(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Add("c", makeTrackerMiddleware("c", &trace))
	chain.InsertAfter("a", "b", makeTrackerMiddleware("b", &trace))

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,b:before,c:before,c:after,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("execution order:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestMiddlewareChain_InsertAfter_NonExistent(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.InsertAfter("nonexistent", "b", makeTrackerMiddleware("b", &trace))

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,b:before,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("expected fallback to Add, got: %s", got)
	}
}

func TestMiddlewareChain_Remove(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Add("b", makeTrackerMiddleware("b", &trace))
	chain.Add("c", makeTrackerMiddleware("c", &trace))
	chain.Remove("b")

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,c:before,c:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("execution order:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestMiddlewareChain_Remove_NonExistent(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Remove("nonexistent") // should be a no-op

	h := chain.then(dummyHandler)
	_ = h(JobContext{})

	expected := "a:before,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("Remove of non-existent changed chain: %s", got)
	}
}

func TestMiddlewareChain_Empty(t *testing.T) {
	chain := newMiddlewareChain()

	called := false
	handler := func(ctx JobContext) error {
		called = true
		return nil
	}

	h := chain.then(handler)
	if err := h(JobContext{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("handler was not called with empty chain")
	}
}

func TestMiddlewareChain_ErrorPropagation(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("a", makeTrackerMiddleware("a", &trace))
	chain.Add("b", makeTrackerMiddleware("b", &trace))

	h := chain.then(failingHandler)
	err := h(JobContext{})

	if err == nil || err.Error() != "handler failed" {
		t.Errorf("expected 'handler failed', got: %v", err)
	}
	// Both middleware should still complete their after phase
	expected := "a:before,b:before,b:after,a:after"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("error propagation broke middleware order: %s", got)
	}
}

func TestMiddlewareChain_MiddlewareShortCircuit(t *testing.T) {
	chain := newMiddlewareChain()

	var trace []string
	chain.Add("gate", func(ctx JobContext, next HandlerFunc) error {
		trace = append(trace, "gate:reject")
		return errors.New("rejected by gate")
	})
	chain.Add("inner", makeTrackerMiddleware("inner", &trace))

	h := chain.then(dummyHandler)
	err := h(JobContext{})

	if err == nil || err.Error() != "rejected by gate" {
		t.Errorf("expected rejection, got: %v", err)
	}
	// Inner middleware should not execute
	expected := "gate:reject"
	got := strings.Join(trace, ",")
	if got != expected {
		t.Errorf("short-circuit didn't prevent inner middleware: %s", got)
	}
}
