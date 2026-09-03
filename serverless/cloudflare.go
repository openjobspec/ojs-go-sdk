package serverless

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// CloudflareOption configures the CloudflareHandler.
type CloudflareOption func(*CloudflareHandler)

// WithCloudflareOJSURL sets the OJS server URL for callback operations.
func WithCloudflareOJSURL(url string) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.ojsURL = url
	}
}

// WithCloudflareLogger sets a custom slog logger.
func WithCloudflareLogger(logger *slog.Logger) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.logger = logger
	}
}

// WithCloudflareTimeout sets the maximum duration for processing a single job.
func WithCloudflareTimeout(d time.Duration) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.timeout = d
	}
}

// WithCloudflareMaxBodySize sets the maximum allowed request body size in bytes.
func WithCloudflareMaxBodySize(n int64) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.maxBodySize = n
	}
}

// WithCloudflareHandlerOptions applies shared HTTP push authentication settings.
func WithCloudflareHandlerOptions(options HandlerOptions) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.applyHandlerOptions(options)
	}
}

// WithCloudflarePushSigningSecrets replaces the secrets accepted for OJS HTTP
// push signatures.
func WithCloudflarePushSigningSecrets(secrets ...string) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.setPushSigningSecrets(secrets)
	}
}

// WithCloudflarePushFreshnessWindow sets the permitted timestamp clock skew.
func WithCloudflarePushFreshnessWindow(window time.Duration) CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.pushFreshnessWindow = window
	}
}

// WithCloudflareInsecureAllowUnsignedPushForLocalDevelopment disables HTTP
// push authentication. It must only be used for local development and tests.
func WithCloudflareInsecureAllowUnsignedPushForLocalDevelopment() CloudflareOption {
	return func(h *CloudflareHandler) {
		h.inner.insecureAllowUnsignedPushForLocalDevelopment = true
	}
}

// CloudflareHandler processes OJS jobs delivered via HTTP requests to a
// Cloudflare Worker. It implements http.Handler so it can be used directly
// with any HTTP server or serverless platform that accepts standard Go
// HTTP handlers.
//
// Usage with Cloudflare Workers (via Go WASM or HTTP-compatible runtimes):
//
//	handler := serverless.NewCloudflareHandler(
//	    serverless.WithCloudflareOJSURL("https://ojs.example.com"),
//	)
//
//	handler.Register("email.send", func(ctx context.Context, job serverless.JobEvent) error {
//	    // Process the job
//	    return nil
//	})
//
//	http.Handle("/ojs/worker", handler)
type CloudflareHandler struct {
	inner *LambdaHandler
}

// NewCloudflareHandler creates a new handler for Cloudflare Workers and other
// HTTP-based serverless platforms.
func NewCloudflareHandler(opts ...CloudflareOption) *CloudflareHandler {
	h := &CloudflareHandler{
		inner: NewLambdaHandler(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register associates a handler function with a job type.
func (h *CloudflareHandler) Register(jobType string, handler HandlerFunc) {
	h.inner.Register(jobType, handler)
}

// ServeHTTP implements http.Handler. It accepts OJS push delivery requests
// (job payloads POSTed by an OJS server) and dispatches them to registered
// handlers. This makes CloudflareHandler directly usable as an HTTP handler.
func (h *CloudflareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.inner.HandleHTTP().ServeHTTP(w, r)
}

// HandleFetchEvent processes a single job event from a Cloudflare Worker
// FetchEvent-style invocation where the request body contains a raw OJS job.
// Unlike ServeHTTP (which expects PushDeliveryRequest wrapping), this method
// reads the job directly from the request body.
func (h *CloudflareHandler) HandleFetchEvent(w http.ResponseWriter, r *http.Request) {
	h.inner.servePush(w, r, pushBinding{
		decode:       decodeBareJob,
		decodeErrMsg: "failed to decode job event",
	})
}

// HandleJob processes a single OJS job directly, without HTTP transport.
// Use this when you have already deserialized the job from the request.
func (h *CloudflareHandler) HandleJob(ctx context.Context, job JobEvent) error {
	return h.inner.processJob(ctx, job)
}
