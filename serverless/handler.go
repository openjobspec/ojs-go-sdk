package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultTimeout is the default maximum duration for processing a single job.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxBodySize is the default maximum HTTP request body size (1 MB).
	DefaultMaxBodySize int64 = 1 << 20

	// DefaultPushFreshnessWindow is the maximum permitted clock skew for signed
	// HTTP push requests.
	DefaultPushFreshnessWindow = 5 * time.Minute
)

// JobEvent represents an OJS job delivered to a serverless function.
type JobEvent struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Queue    string          `json:"queue"`
	Args     json.RawMessage `json:"args"`
	Attempt  int             `json:"attempt"`
	Meta     json.RawMessage `json:"meta,omitempty"`
	Priority int             `json:"priority,omitempty"`
}

// HandlerFunc is a function that processes an OJS job in a serverless context.
type HandlerFunc func(ctx context.Context, job JobEvent) error

// Option configures the LambdaHandler.
type Option func(*LambdaHandler)

// HandlerOptions contains shared HTTP push authentication settings.
//
// Push endpoints fail closed unless at least one signing secret is configured
// or InsecureAllowUnsignedPushForLocalDevelopment is explicitly enabled.
type HandlerOptions struct {
	PushSigningSecrets                           []string
	PushFreshnessWindow                          time.Duration
	InsecureAllowUnsignedPushForLocalDevelopment bool
}

// WithHandlerOptions applies shared HTTP push authentication settings.
func WithHandlerOptions(options HandlerOptions) Option {
	return func(h *LambdaHandler) {
		h.applyHandlerOptions(options)
	}
}

// WithOJSURL sets the OJS server URL for callback operations.
func WithOJSURL(url string) Option {
	return func(h *LambdaHandler) {
		h.ojsURL = url
	}
}

// WithLogger sets a custom slog logger.
func WithLogger(logger *slog.Logger) Option {
	return func(h *LambdaHandler) {
		h.logger = logger
	}
}

// WithTimeout sets the maximum duration for processing a single job.
// If the handler does not complete within this duration, the context is
// cancelled. Default is 30 seconds. Set to 0 to disable.
func WithTimeout(d time.Duration) Option {
	return func(h *LambdaHandler) {
		h.timeout = d
	}
}

// WithMaxBodySize sets the maximum allowed HTTP request body size in bytes.
// Requests exceeding this limit are rejected. Default is 1 MB.
func WithMaxBodySize(n int64) Option {
	return func(h *LambdaHandler) {
		h.maxBodySize = n
	}
}

// WithPushSigningSecrets replaces the secrets accepted for OJS HTTP push
// signatures. Supplying both the current and previous secret permits rotation
// without downtime.
func WithPushSigningSecrets(secrets ...string) Option {
	return func(h *LambdaHandler) {
		h.setPushSigningSecrets(secrets)
	}
}

// WithPushFreshnessWindow sets the permitted past or future clock skew for OJS
// HTTP push timestamps. The default is five minutes. A non-positive value is
// treated as an invalid configuration and causes push requests to fail closed.
func WithPushFreshnessWindow(window time.Duration) Option {
	return func(h *LambdaHandler) {
		h.pushFreshnessWindow = window
	}
}

// WithInsecureAllowUnsignedPushForLocalDevelopment disables HTTP push
// authentication. It must only be used for local development and tests.
func WithInsecureAllowUnsignedPushForLocalDevelopment() Option {
	return func(h *LambdaHandler) {
		h.insecureAllowUnsignedPushForLocalDevelopment = true
	}
}

// LambdaHandler processes OJS jobs delivered via SQS or HTTP push.
//
// This file owns the job registry and the execution policy applied to every
// invocation (timeout and panic isolation). The transport adapters live
// alongside it: SQS in sqs.go and OJS push delivery in push.go.
type LambdaHandler struct {
	handlers    map[string]HandlerFunc
	mu          sync.RWMutex
	ojsURL      string
	logger      *slog.Logger
	timeout     time.Duration
	maxBodySize int64
	initialized time.Time

	pushSigningSecrets                           [][]byte
	pushFreshnessWindow                          time.Duration
	insecureAllowUnsignedPushForLocalDevelopment bool
}

// NewLambdaHandler creates a new serverless handler with the given options.
func NewLambdaHandler(opts ...Option) *LambdaHandler {
	h := &LambdaHandler{
		handlers:            make(map[string]HandlerFunc),
		logger:              slog.Default(),
		timeout:             DefaultTimeout,
		maxBodySize:         DefaultMaxBodySize,
		initialized:         time.Now(),
		pushFreshnessWindow: DefaultPushFreshnessWindow,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *LambdaHandler) applyHandlerOptions(options HandlerOptions) {
	h.setPushSigningSecrets(options.PushSigningSecrets)
	if options.PushFreshnessWindow != 0 {
		h.pushFreshnessWindow = options.PushFreshnessWindow
	}
	h.insecureAllowUnsignedPushForLocalDevelopment =
		options.InsecureAllowUnsignedPushForLocalDevelopment
}

func (h *LambdaHandler) setPushSigningSecrets(secrets []string) {
	copied := make([][]byte, 0, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		copied = append(copied, []byte(secret))
	}
	h.pushSigningSecrets = copied
}

// Register associates a handler function with a job type.
func (h *LambdaHandler) Register(jobType string, handler HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[jobType] = handler
}

// Initialized returns the time when the handler was created. This can be used
// to measure cold start latency in serverless environments by comparing
// this value against the first request timestamp.
func (h *LambdaHandler) Initialized() time.Time {
	return h.initialized
}

// HandleHTTP returns an http.HandlerFunc for OJS push delivery.
// The OJS server POSTs job payloads to this endpoint. Request bodies
// exceeding MaxBodySize are rejected.
func (h *LambdaHandler) HandleHTTP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.servePush(w, r, pushBinding{
			decode:       decodePushDelivery,
			decodeErrMsg: "failed to decode request body",
		})
	}
}

// DirectResponse is the result of processing a single job via direct invocation.
type DirectResponse struct {
	Status string `json:"status"` // "completed" or "failed"
	JobID  string `json:"job_id"`
	Error  string `json:"error,omitempty"`
}

// HandleDirect processes a single OJS job event from a direct Lambda invocation.
// Use this when a Lambda function is invoked directly (not via SQS or HTTP push).
func (h *LambdaHandler) HandleDirect(ctx context.Context, event JobEvent) (DirectResponse, error) {
	if event.ID == "" || event.Type == "" {
		return DirectResponse{
			Status: pushStatusFailed,
			Error:  "job id and type are required",
		}, nil
	}

	if err := h.processJob(ctx, event); err != nil {
		h.logger.Error("job processing failed",
			"job_id", event.ID,
			"job_type", event.Type,
			"error", err,
		)
		return DirectResponse{
			Status: pushStatusFailed,
			JobID:  event.ID,
			Error:  err.Error(),
		}, nil
	}

	h.logger.Info("job completed",
		"job_id", event.ID,
		"job_type", event.Type,
	)
	return DirectResponse{
		Status: pushStatusCompleted,
		JobID:  event.ID,
	}, nil
}

// processJob looks up the handler for a job and runs it under the configured
// execution policy.
func (h *LambdaHandler) processJob(ctx context.Context, job JobEvent) error {
	h.mu.RLock()
	handler, ok := h.handlers[job.Type]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler registered for job type: %s", job.Type)
	}

	if h.timeout > 0 {
		timedCtx, cancel := context.WithTimeout(ctx, h.timeout)
		defer cancel()
		ctx = timedCtx
	}

	return invokeHandler(ctx, handler, job)
}

// invokeHandler runs a handler and converts a panic into an ordinary error.
//
// Without this, one panicking handler takes down the whole function invocation:
// an SQS batch would fail every message in it rather than just the offending
// one, and a direct invocation would surface as a platform crash instead of a
// job failure.
func invokeHandler(ctx context.Context, handler HandlerFunc, job JobEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in job handler for %s: %v", job.Type, r)
		}
	}()
	return handler(ctx, job)
}
