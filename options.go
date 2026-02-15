package ojs

import (
	"log/slog"
	"net/http"
	"time"
)

// --- Enqueue Options ---

// enqueueConfig holds the resolved configuration for an enqueue operation.
type enqueueConfig struct {
	queue             string
	priority          int
	timeoutMS         int
	delayUntil        *time.Time
	expiresAt         *time.Time
	retry             *RetryPolicy
	unique            *UniquePolicy
	tags              []string
	meta              map[string]any
	visibilityTimeout int
}

// EnqueueOption configures job enqueue behavior.
type EnqueueOption func(*enqueueConfig)

// WithQueue sets the target queue for the job. Default: "default".
func WithQueue(queue string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.queue = queue
	}
}

// WithPriority sets the job priority. Higher values = higher priority.
func WithPriority(priority int) EnqueueOption {
	return func(c *enqueueConfig) {
		c.priority = priority
	}
}

// WithTimeout sets the maximum execution time for the job.
func WithTimeout(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		c.timeoutMS = int(d.Milliseconds())
	}
}

// WithDelay schedules the job to run after the specified duration.
func WithDelay(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		t := time.Now().Add(d)
		c.delayUntil = &t
	}
}

// WithScheduledAt schedules the job to run at a specific time.
func WithScheduledAt(t time.Time) EnqueueOption {
	return func(c *enqueueConfig) {
		c.delayUntil = &t
	}
}

// WithExpiresAt sets an expiration time after which the job should be discarded.
func WithExpiresAt(t time.Time) EnqueueOption {
	return func(c *enqueueConfig) {
		c.expiresAt = &t
	}
}

// WithRetry sets a custom retry policy for the job.
func WithRetry(policy RetryPolicy) EnqueueOption {
	return func(c *enqueueConfig) {
		c.retry = &policy
	}
}

// WithUnique sets a deduplication policy for the job.
func WithUnique(policy UniquePolicy) EnqueueOption {
	return func(c *enqueueConfig) {
		c.unique = &policy
	}
}

// WithTags adds tags to the job for filtering and observability.
func WithTags(tags ...string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.tags = append(c.tags, tags...)
	}
}

// WithMeta sets metadata key-value pairs on the job.
func WithMeta(meta map[string]any) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		for k, v := range meta {
			c.meta[k] = v
		}
	}
}

// WithVisibilityTimeout sets the reservation period before the job is reclaimed.
func WithVisibilityTimeout(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		c.visibilityTimeout = int(d.Milliseconds())
	}
}

func resolveEnqueueConfig(opts []EnqueueOption) enqueueConfig {
	cfg := enqueueConfig{
		queue: "default",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// UniquePolicy defines the deduplication policy for a job.
type UniquePolicy struct {
	// Key specifies the args fields used for uniqueness key generation.
	Key []string `json:"key,omitempty"`

	// Period is how long the uniqueness constraint lasts.
	Period time.Duration `json:"-"`

	// OnConflict is the strategy when a duplicate is found.
	// Values: "reject", "replace", "replace_except_schedule", "ignore".
	// Default: "reject".
	OnConflict string `json:"on_conflict,omitempty"`
}

type uniquePolicyWire struct {
	Key        []string `json:"key,omitempty"`
	PeriodMS   int      `json:"period_ms,omitempty"`
	OnConflict string   `json:"on_conflict,omitempty"`
}

func (u UniquePolicy) toWire() *uniquePolicyWire {
	return &uniquePolicyWire{
		Key:        u.Key,
		PeriodMS:   int(u.Period.Milliseconds()),
		OnConflict: u.OnConflict,
	}
}

// --- Client Options ---

// clientConfig holds the resolved configuration for a Client.
type clientConfig struct {
	httpClient *http.Client
	authToken  string
	headers    map[string]string
}

// ClientOption configures the OJS client.
type ClientOption func(*clientConfig)

// WithHTTPClient sets a custom net/http.Client for the OJS client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *clientConfig) {
		c.httpClient = client
	}
}

// WithAuthToken sets a Bearer token for authentication.
func WithAuthToken(token string) ClientOption {
	return func(c *clientConfig) {
		c.authToken = token
	}
}

// WithHeader sets a custom header on all requests.
func WithHeader(key, value string) ClientOption {
	return func(c *clientConfig) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		c.headers[key] = value
	}
}

// --- Worker Options ---

// workerConfig holds the resolved configuration for a Worker.
type workerConfig struct {
	queues            []string
	concurrency       int
	gracePeriod       time.Duration
	heartbeatInterval time.Duration
	labels            []string
	pollInterval      time.Duration
	authToken         string
	httpClient        *http.Client
	logger            *slog.Logger
}

// WorkerOption configures the OJS worker.
type WorkerOption func(*workerConfig)

// WithQueues sets the queues the worker subscribes to.
// The first queue has the highest priority.
func WithQueues(queues ...string) WorkerOption {
	return func(c *workerConfig) {
		c.queues = queues
	}
}

// WithConcurrency sets the maximum number of jobs processed in parallel.
// Default: 10.
func WithConcurrency(n int) WorkerOption {
	return func(c *workerConfig) {
		c.concurrency = n
	}
}

// WithGracePeriod sets the maximum time to wait for active jobs during shutdown.
// Default: 25 seconds.
func WithGracePeriod(d time.Duration) WorkerOption {
	return func(c *workerConfig) {
		c.gracePeriod = d
	}
}

// WithHeartbeatInterval sets the interval between heartbeat requests.
// Default: 5 seconds.
func WithHeartbeatInterval(d time.Duration) WorkerOption {
	return func(c *workerConfig) {
		c.heartbeatInterval = d
	}
}

// WithLabels sets worker labels for filtering and grouping.
func WithLabels(labels ...string) WorkerOption {
	return func(c *workerConfig) {
		c.labels = labels
	}
}

// WithPollInterval sets the interval between fetch requests when no jobs are available.
// Default: 1 second.
func WithPollInterval(d time.Duration) WorkerOption {
	return func(c *workerConfig) {
		c.pollInterval = d
	}
}

// WithWorkerAuth sets a Bearer token for the worker's HTTP requests.
func WithWorkerAuth(token string) WorkerOption {
	return func(c *workerConfig) {
		c.authToken = token
	}
}

// WithWorkerHTTPClient sets a custom net/http.Client for the worker.
func WithWorkerHTTPClient(client *http.Client) WorkerOption {
	return func(c *workerConfig) {
		c.httpClient = client
	}
}

// WithLogger sets a structured logger for the worker's operational events.
// When set, the worker logs ACK/NACK failures, fetch errors, and state
// transitions. Pass nil to disable logging (the default).
func WithLogger(logger *slog.Logger) WorkerOption {
	return func(c *workerConfig) {
		c.logger = logger
	}
}

func resolveWorkerConfig(opts []WorkerOption) workerConfig {
	cfg := workerConfig{
		queues:            []string{"default"},
		concurrency:       10,
		gracePeriod:       25 * time.Second,
		heartbeatInterval: 5 * time.Second,
		pollInterval:      1 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
