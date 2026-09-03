package serverless

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// VercelOption configures the VercelHandler.
type VercelOption func(*VercelHandler)

// WithVercelOJSURL sets the OJS server URL for callback operations.
func WithVercelOJSURL(url string) VercelOption {
	return func(h *VercelHandler) {
		h.inner.ojsURL = url
	}
}

// WithVercelLogger sets a custom slog logger.
func WithVercelLogger(logger *slog.Logger) VercelOption {
	return func(h *VercelHandler) {
		h.inner.logger = logger
	}
}

// WithVercelTimeout sets the maximum duration for processing a single job.
func WithVercelTimeout(d time.Duration) VercelOption {
	return func(h *VercelHandler) {
		h.inner.timeout = d
	}
}

// WithVercelMaxBodySize sets the maximum allowed request body size in bytes.
func WithVercelMaxBodySize(n int64) VercelOption {
	return func(h *VercelHandler) {
		h.inner.maxBodySize = n
	}
}

// WithVercelHandlerOptions applies shared HTTP push authentication settings.
func WithVercelHandlerOptions(options HandlerOptions) VercelOption {
	return func(h *VercelHandler) {
		h.inner.applyHandlerOptions(options)
	}
}

// WithVercelPushSigningSecrets replaces the secrets accepted for OJS HTTP push
// signatures.
func WithVercelPushSigningSecrets(secrets ...string) VercelOption {
	return func(h *VercelHandler) {
		h.inner.setPushSigningSecrets(secrets)
	}
}

// WithVercelPushFreshnessWindow sets the permitted timestamp clock skew.
func WithVercelPushFreshnessWindow(window time.Duration) VercelOption {
	return func(h *VercelHandler) {
		h.inner.pushFreshnessWindow = window
	}
}

// WithVercelInsecureAllowUnsignedPushForLocalDevelopment disables HTTP push
// authentication. It must only be used for local development and tests.
func WithVercelInsecureAllowUnsignedPushForLocalDevelopment() VercelOption {
	return func(h *VercelHandler) {
		h.inner.insecureAllowUnsignedPushForLocalDevelopment = true
	}
}

// VercelHandler wraps an OJS worker for Vercel serverless functions.
// It implements [net/http.Handler] and processes OJS push delivery requests.
//
// Usage with Vercel Go functions:
//
//	handler := serverless.NewVercelHandler(
//	    serverless.WithVercelOJSURL("https://ojs.example.com"),
//	)
//
//	handler.Register("email.send", func(ctx context.Context, job serverless.JobEvent) error {
//	    // Process the job
//	    return nil
//	})
//
//	// In api/worker.go:
//	func Handler(w http.ResponseWriter, r *http.Request) {
//	    handler.ServeHTTP(w, r)
//	}
type VercelHandler struct {
	inner *LambdaHandler
}

// NewVercelHandler creates a new handler for Vercel serverless functions.
func NewVercelHandler(opts ...VercelOption) *VercelHandler {
	h := &VercelHandler{
		inner: NewLambdaHandler(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register associates a handler function with a job type.
func (h *VercelHandler) Register(jobType string, handler HandlerFunc) {
	h.inner.Register(jobType, handler)
}

// ServeHTTP implements [net/http.Handler]. It accepts OJS push delivery requests
// and dispatches them to registered handlers. The Vercel request ID is propagated
// via the request context when the X-Vercel-Id header is present.
func (h *VercelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.inner.servePush(w, r, pushBinding{
		decode:         decodePushDelivery,
		decodeErrMsg:   "failed to decode request body",
		requestContext: vercelRequestContext,
	})
}

// vercelRequestContext propagates the Vercel request ID for tracing.
func vercelRequestContext(r *http.Request) context.Context {
	ctx := r.Context()
	if vercelID := r.Header.Get("X-Vercel-Id"); vercelID != "" {
		ctx = context.WithValue(ctx, vercelRequestIDKey, vercelID)
	}
	return ctx
}

// HandleJob processes a single OJS job directly, without HTTP transport.
func (h *VercelHandler) HandleJob(ctx context.Context, job JobEvent) error {
	return h.inner.processJob(ctx, job)
}

type contextKey string

const vercelRequestIDKey contextKey = "vercel-request-id"

// VercelRequestID extracts the Vercel request ID from the context, if present.
// This is set automatically by [VercelHandler.ServeHTTP] from the X-Vercel-Id header.
func VercelRequestID(ctx context.Context) string {
	v, _ := ctx.Value(vercelRequestIDKey).(string)
	return v
}
