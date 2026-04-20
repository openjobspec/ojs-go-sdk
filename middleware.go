package ojs

import (
	"fmt"
	"slices"
	"sync"
)

// HandlerFunc is a function that processes a job.
type HandlerFunc func(JobContext) error

// MiddlewareFunc is a function that wraps a HandlerFunc with cross-cutting concerns.
// It follows the standard Go middleware pattern (onion model).
//
// Example:
//
//	func loggingMiddleware(ctx ojs.JobContext, next ojs.HandlerFunc) error {
//	    log.Printf("Starting %s", ctx.Job.Type)
//	    err := next(ctx)
//	    log.Printf("Done %s", ctx.Job.Type)
//	    return err
//	}
type MiddlewareFunc func(ctx JobContext, next HandlerFunc) error

// middlewareChain holds an ordered list of middleware.
//
// A chain is mutated by the goroutine that configures a Worker and read by
// every goroutine that executes a job, so all access is guarded by mu.
type middlewareChain struct {
	mu         sync.RWMutex
	middleware []namedMiddleware
}

// namedMiddleware associates a name with a middleware for identification.
type namedMiddleware struct {
	name string
	fn   MiddlewareFunc
}

func newMiddlewareChain() *middlewareChain {
	return &middlewareChain{}
}

// Add appends middleware to the end of the chain.
func (c *middlewareChain) Add(name string, fn MiddlewareFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(name, fn)
}

// add appends without locking. Callers must hold mu.
func (c *middlewareChain) add(name string, fn MiddlewareFunc) {
	c.middleware = append(c.middleware, namedMiddleware{name: name, fn: fn})
}

// AddAutoNamed appends middleware under a generated positional name.
// The name is derived under the lock so concurrent callers cannot collide.
func (c *middlewareChain) AddAutoNamed(fn MiddlewareFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(fmt.Sprintf("middleware-%d", len(c.middleware)), fn)
}

// Prepend inserts middleware at the beginning of the chain.
func (c *middlewareChain) Prepend(name string, fn MiddlewareFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.middleware = append([]namedMiddleware{{name: name, fn: fn}}, c.middleware...)
}

// InsertBefore inserts middleware immediately before the named middleware.
func (c *middlewareChain) InsertBefore(existing string, name string, fn MiddlewareFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, m := range c.middleware {
		if m.name == existing {
			c.middleware = slices.Insert(c.middleware, i, namedMiddleware{name: name, fn: fn})
			return
		}
	}
	// If not found, append to end.
	c.add(name, fn)
}

// InsertAfter inserts middleware immediately after the named middleware.
func (c *middlewareChain) InsertAfter(existing string, name string, fn MiddlewareFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, m := range c.middleware {
		if m.name == existing {
			c.middleware = slices.Insert(c.middleware, i+1, namedMiddleware{name: name, fn: fn})
			return
		}
	}
	c.add(name, fn)
}

// Remove removes middleware by name from the chain.
func (c *middlewareChain) Remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, m := range c.middleware {
		if m.name == name {
			c.middleware = slices.Delete(c.middleware, i, i+1)
			return
		}
	}
}

// then builds a HandlerFunc by wrapping the handler with the middleware chain.
func (c *middlewareChain) then(handler HandlerFunc) HandlerFunc {
	c.mu.RLock()
	// Snapshot under the read lock so a concurrent Add cannot mutate the slice
	// while the chain is being composed.
	chain := slices.Clone(c.middleware)
	c.mu.RUnlock()

	// Build from inside out: the last middleware wraps the handler first.
	h := handler
	for i := len(chain) - 1; i >= 0; i-- {
		mw := chain[i].fn
		next := h
		h = func(ctx JobContext) error {
			return mw(ctx, next)
		}
	}
	return h
}
