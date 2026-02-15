package ojs

import (
	"context"
	"fmt"
	"time"
)

// Client is an OJS client that communicates with an OJS-compliant server
// over HTTP. It provides methods for enqueuing jobs, checking job status,
// cancelling jobs, batch operations, and workflow management.
type Client struct {
	transport *transport
}

// NewClient creates a new OJS client connected to the given server URL.
//
// Example:
//
//	client, err := ojs.NewClient("http://localhost:8080")
func NewClient(serverURL string, opts ...ClientOption) (*Client, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("ojs: server URL is required")
	}
	cfg := clientConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Client{
		transport: newTransport(serverURL, cfg),
	}, nil
}

// --- Wire format types for requests ---

type enqueueRequest struct {
	Type    string       `json:"type"`
	Args    []any        `json:"args"`
	Meta    map[string]any `json:"meta,omitempty"`
	Schema  string       `json:"schema,omitempty"`
	Options *wireOptions `json:"options,omitempty"`
}

type wireOptions struct {
	Queue              string           `json:"queue,omitempty"`
	Priority           int              `json:"priority,omitempty"`
	TimeoutMS          int              `json:"timeout_ms,omitempty"`
	DelayUntil         *time.Time       `json:"delay_until,omitempty"`
	ExpiresAt          *time.Time       `json:"expires_at,omitempty"`
	Retry              *retryPolicyWire `json:"retry,omitempty"`
	Unique             *uniquePolicyWire `json:"unique,omitempty"`
	Tags               []string         `json:"tags,omitempty"`
	VisibilityTimeoutMS int             `json:"visibility_timeout_ms,omitempty"`
}

func buildWireOptions(cfg enqueueConfig) *wireOptions {
	opts := &wireOptions{
		Queue:     cfg.queue,
		Priority:  cfg.priority,
		TimeoutMS: cfg.timeoutMS,
		DelayUntil: cfg.delayUntil,
		ExpiresAt:  cfg.expiresAt,
		Tags:       cfg.tags,
		VisibilityTimeoutMS: cfg.visibilityTimeout,
	}
	if cfg.retry != nil {
		opts.Retry = cfg.retry.toWire()
	}
	if cfg.unique != nil {
		opts.Unique = cfg.unique.toWire()
	}
	return opts
}

// --- Job response wrappers ---

type jobResponse struct {
	Job Job `json:"job"`
}

type batchResponse struct {
	Jobs  []Job `json:"jobs"`
	Count int   `json:"count"`
}

// Enqueue submits a single job for processing.
//
// Example:
//
//	job, err := client.Enqueue(ctx, "email.send",
//	    ojs.Args{"to": "user@example.com"},
//	    ojs.WithQueue("email"),
//	    ojs.WithRetry(ojs.RetryPolicy{MaxAttempts: 5}),
//	)
func (c *Client) Enqueue(ctx context.Context, jobType string, args Args, opts ...EnqueueOption) (*Job, error) {
	if err := validateEnqueueParams(jobType, argsToWire(args)); err != nil {
		return nil, err
	}
	cfg := resolveEnqueueConfig(opts)
	if err := validateQueue(cfg.queue); err != nil {
		return nil, err
	}

	req := enqueueRequest{
		Type: jobType,
		Args: argsToWire(args),
		Meta: cfg.meta,
	}
	req.Options = buildWireOptions(cfg)

	var resp jobResponse
	if err := c.transport.post(ctx, basePath+"/jobs", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// EnqueueBatch submits multiple jobs atomically.
//
// Example:
//
//	jobs, err := client.EnqueueBatch(ctx, []ojs.JobRequest{
//	    {Type: "email.send", Args: ojs.Args{"to": "a@example.com"}},
//	    {Type: "email.send", Args: ojs.Args{"to": "b@example.com"}},
//	})
func (c *Client) EnqueueBatch(ctx context.Context, requests []JobRequest) ([]Job, error) {
	wireJobs := make([]enqueueRequest, len(requests))
	for i, r := range requests {
		if err := validateEnqueueParams(r.Type, argsToWire(r.Args)); err != nil {
			return nil, fmt.Errorf("job[%d]: %w", i, err)
		}
		cfg := resolveEnqueueConfig(r.Options)
		if err := validateQueue(cfg.queue); err != nil {
			return nil, fmt.Errorf("job[%d]: %w", i, err)
		}
		wireJobs[i] = enqueueRequest{
			Type:    r.Type,
			Args:    argsToWire(r.Args),
			Meta:    cfg.meta,
			Options: buildWireOptions(cfg),
		}
	}

	body := struct {
		Jobs []enqueueRequest `json:"jobs"`
	}{Jobs: wireJobs}

	var resp batchResponse
	if err := c.transport.post(ctx, basePath+"/jobs/batch", body, &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// GetJob retrieves the full details of a job by ID.
func (c *Client) GetJob(ctx context.Context, id string) (*Job, error) {
	var resp jobResponse
	path := fmt.Sprintf("%s/jobs/%s", basePath, id)
	if err := c.transport.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// CancelJob cancels a job, preventing it from being processed.
func (c *Client) CancelJob(ctx context.Context, id string) (*Job, error) {
	var resp jobResponse
	path := fmt.Sprintf("%s/jobs/%s", basePath, id)
	if err := c.transport.delete(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// Health checks the health status of the OJS server.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	var resp HealthStatus
	if err := c.transport.get(ctx, basePath+"/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// HealthStatus represents the health of the OJS server.
type HealthStatus struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int    `json:"uptime_seconds"`
	Backend       struct {
		Type      string `json:"type"`
		Status    string `json:"status"`
		LatencyMS int    `json:"latency_ms,omitempty"`
		Error     string `json:"error,omitempty"`
	} `json:"backend"`
}

// Manifest retrieves the server's conformance manifest.
func (c *Client) Manifest(ctx context.Context) (*Manifest, error) {
	var resp Manifest
	// Manifest is at /ojs/manifest (no /v1 prefix).
	if err := c.transport.get(ctx, "/ojs/manifest", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Manifest describes the OJS server's capabilities and conformance level.
type Manifest struct {
	OJSVersion       string            `json:"ojs_version"`
	Implementation   Implementation    `json:"implementation"`
	ConformanceLevel int               `json:"conformance_level"`
	Protocols        []string          `json:"protocols"`
	Backend          string            `json:"backend"`
	Capabilities     map[string]bool   `json:"capabilities"`
	Extensions       []string          `json:"extensions"`
	Endpoints        map[string]string `json:"endpoints"`
}

// Implementation describes the OJS server implementation.
type Implementation struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Language string `json:"language"`
	Homepage string `json:"homepage,omitempty"`
}

// --- Dead Letter operations ---

// DeadLetterJob represents a job in the dead letter queue.
type DeadLetterJob struct {
	Job
}

// ListDeadLetterJobs returns a paginated list of dead letter jobs.
func (c *Client) ListDeadLetterJobs(ctx context.Context, queue string, limit, offset int) ([]Job, *Pagination, error) {
	path := fmt.Sprintf("%s/dead-letter?limit=%d&offset=%d", basePath, limit, offset)
	if queue != "" {
		path += "&queue=" + queue
	}
	var resp struct {
		Jobs       []Job      `json:"jobs"`
		Pagination Pagination `json:"pagination"`
	}
	if err := c.transport.get(ctx, path, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Jobs, &resp.Pagination, nil
}

// RetryDeadLetterJob re-enqueues a dead letter job for another attempt.
func (c *Client) RetryDeadLetterJob(ctx context.Context, id string) (*Job, error) {
	var resp jobResponse
	path := fmt.Sprintf("%s/dead-letter/%s/retry", basePath, id)
	if err := c.transport.post(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// DiscardDeadLetterJob permanently removes a dead letter job.
func (c *Client) DiscardDeadLetterJob(ctx context.Context, id string) error {
	path := fmt.Sprintf("%s/dead-letter/%s", basePath, id)
	return c.transport.delete(ctx, path, nil)
}

// --- Cron operations ---

// CronJob represents a recurring job schedule.
type CronJob struct {
	Name      string         `json:"name"`
	Cron      string         `json:"cron"`
	Timezone  string         `json:"timezone"`
	Type      string         `json:"type"`
	Args      []any          `json:"args"`
	Options   map[string]any `json:"options,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	Status    string         `json:"status"`
	LastRunAt *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt *time.Time     `json:"created_at,omitempty"`
}

// CronJobRequest defines a new cron schedule.
type CronJobRequest struct {
	Name     string         `json:"name"`
	Cron     string         `json:"cron"`
	Timezone string         `json:"timezone,omitempty"`
	Type     string         `json:"type"`
	Args     Args           `json:"-"`
	Meta     map[string]any `json:"meta,omitempty"`
	Options  []EnqueueOption `json:"-"`
}

type cronJobRequestWire struct {
	Name     string         `json:"name"`
	Cron     string         `json:"cron"`
	Timezone string         `json:"timezone,omitempty"`
	Type     string         `json:"type"`
	Args     []any          `json:"args"`
	Meta     map[string]any `json:"meta,omitempty"`
	Options  *wireOptions   `json:"options,omitempty"`
}

// ListCronJobs returns all registered cron job definitions.
func (c *Client) ListCronJobs(ctx context.Context) ([]CronJob, error) {
	var resp struct {
		CronJobs   []CronJob  `json:"cron_jobs"`
		Pagination Pagination `json:"pagination"`
	}
	if err := c.transport.get(ctx, basePath+"/cron", &resp); err != nil {
		return nil, err
	}
	return resp.CronJobs, nil
}

// RegisterCronJob registers a new recurring job schedule.
func (c *Client) RegisterCronJob(ctx context.Context, req CronJobRequest) (*CronJob, error) {
	cfg := resolveEnqueueConfig(req.Options)
	wire := cronJobRequestWire{
		Name:     req.Name,
		Cron:     req.Cron,
		Timezone: req.Timezone,
		Type:     req.Type,
		Args:     argsToWire(req.Args),
		Meta:     req.Meta,
		Options:  buildWireOptions(cfg),
	}
	var resp struct {
		CronJob CronJob `json:"cron_job"`
	}
	if err := c.transport.post(ctx, basePath+"/cron", wire, &resp); err != nil {
		return nil, err
	}
	return &resp.CronJob, nil
}

// UnregisterCronJob removes a cron job schedule by name.
func (c *Client) UnregisterCronJob(ctx context.Context, name string) error {
	path := fmt.Sprintf("%s/cron/%s", basePath, name)
	return c.transport.delete(ctx, path, nil)
}
