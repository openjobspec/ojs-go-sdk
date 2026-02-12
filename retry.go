package ojs

import "time"

// RetryPolicy defines the retry behavior for a job.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of times a job will be attempted.
	// Default: 3.
	MaxAttempts int `json:"max_attempts,omitempty"`

	// InitialInterval is the delay before the first retry.
	// Default: 1 second.
	InitialInterval time.Duration `json:"-"`

	// BackoffCoefficient is the multiplier applied to the interval between retries.
	// Default: 2.0 (exponential backoff).
	BackoffCoefficient float64 `json:"backoff_coefficient,omitempty"`

	// MaxInterval is the maximum delay between retries.
	// Default: 5 minutes.
	MaxInterval time.Duration `json:"-"`

	// Jitter adds randomization to retry intervals to prevent thundering herd.
	// Default: true.
	Jitter *bool `json:"jitter,omitempty"`

	// NonRetryableErrors is a list of error code prefixes that should not be retried.
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

// retryPolicyWire is the wire representation of RetryPolicy using milliseconds.
type retryPolicyWire struct {
	MaxAttempts        int      `json:"max_attempts,omitempty"`
	InitialIntervalMS  int      `json:"initial_interval_ms,omitempty"`
	BackoffCoefficient float64  `json:"backoff_coefficient,omitempty"`
	MaxIntervalMS      int      `json:"max_interval_ms,omitempty"`
	Jitter             *bool    `json:"jitter,omitempty"`
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

func (r RetryPolicy) toWire() *retryPolicyWire {
	return &retryPolicyWire{
		MaxAttempts:        r.MaxAttempts,
		InitialIntervalMS:  int(r.InitialInterval.Milliseconds()),
		BackoffCoefficient: r.BackoffCoefficient,
		MaxIntervalMS:      int(r.MaxInterval.Milliseconds()),
		Jitter:             r.Jitter,
		NonRetryableErrors: r.NonRetryableErrors,
	}
}

func retryPolicyFromWire(w *retryPolicyWire) *RetryPolicy {
	if w == nil {
		return nil
	}
	return &RetryPolicy{
		MaxAttempts:        w.MaxAttempts,
		InitialInterval:    time.Duration(w.InitialIntervalMS) * time.Millisecond,
		BackoffCoefficient: w.BackoffCoefficient,
		MaxInterval:        time.Duration(w.MaxIntervalMS) * time.Millisecond,
		Jitter:             w.Jitter,
		NonRetryableErrors: w.NonRetryableErrors,
	}
}

// DefaultRetryPolicy returns the OJS default retry policy.
func DefaultRetryPolicy() RetryPolicy {
	jitter := true
	return RetryPolicy{
		MaxAttempts:        3,
		InitialInterval:    1 * time.Second,
		BackoffCoefficient: 2.0,
		MaxInterval:        5 * time.Minute,
		Jitter:             &jitter,
	}
}
