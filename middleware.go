package ojs

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
type middlewareChain struct {
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
	c.middleware = append(c.middleware, namedMiddleware{name: name, fn: fn})
}

// Prepend inserts middleware at the beginning of the chain.
func (c *middlewareChain) Prepend(name string, fn MiddlewareFunc) {
	c.middleware = append([]namedMiddleware{{name: name, fn: fn}}, c.middleware...)
}

// InsertBefore inserts middleware immediately before the named middleware.
func (c *middlewareChain) InsertBefore(existing string, name string, fn MiddlewareFunc) {
	for i, m := range c.middleware {
		if m.name == existing {
			c.middleware = append(c.middleware[:i+1], c.middleware[i:]...)
			c.middleware[i] = namedMiddleware{name: name, fn: fn}
			return
		}
	}
	// If not found, append to end.
	c.Add(name, fn)
}

// InsertAfter inserts middleware immediately after the named middleware.
func (c *middlewareChain) InsertAfter(existing string, name string, fn MiddlewareFunc) {
	for i, m := range c.middleware {
		if m.name == existing {
			c.middleware = append(c.middleware[:i+2], c.middleware[i+1:]...)
			c.middleware[i+1] = namedMiddleware{name: name, fn: fn}
			return
		}
	}
	c.Add(name, fn)
}

// Remove removes middleware by name from the chain.
func (c *middlewareChain) Remove(name string) {
	for i, m := range c.middleware {
		if m.name == name {
			c.middleware = append(c.middleware[:i], c.middleware[i+1:]...)
			return
		}
	}
}

// then builds a HandlerFunc by wrapping the handler with the middleware chain.
func (c *middlewareChain) then(handler HandlerFunc) HandlerFunc {
	// Build from inside out: the last middleware wraps the handler first.
	h := handler
	for i := len(c.middleware) - 1; i >= 0; i-- {
		mw := c.middleware[i].fn
		next := h
		h = func(ctx JobContext) error {
			return mw(ctx, next)
		}
	}
	return h
}
