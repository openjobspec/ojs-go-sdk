package ojs

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryConfig configures automatic HTTP request retries for rate-limited responses.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts. Default: 3.
	MaxRetries int

	// MinBackoff is the minimum backoff duration between retries. Default: 500ms.
	MinBackoff time.Duration

	// MaxBackoff is the maximum backoff duration between retries. Default: 30s.
	MaxBackoff time.Duration

	// Enabled controls whether automatic retry on 429 is active. Default: true.
	Enabled bool
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 30 * time.Second,
		Enabled:    true,
	}
}

// retryBackoff calculates the backoff duration for a given attempt.
// If retryAfter is non-zero, it is used (clamped to MaxBackoff).
// Otherwise, exponential backoff with jitter is applied.
func (rc RetryConfig) retryBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > rc.MaxBackoff {
			return rc.MaxBackoff
		}
		return retryAfter
	}

	// Exponential backoff: MinBackoff * 2^attempt
	backoff := float64(rc.MinBackoff) * math.Pow(2, float64(attempt))
	if backoff > float64(rc.MaxBackoff) {
		backoff = float64(rc.MaxBackoff)
	}

	// Decorrelated jitter: multiply by random factor in [0.5, 1.0)
	jitter := 0.5 + rand.Float64()*0.5
	return time.Duration(float64(backoff) * jitter)
}

// shouldRetry returns true if the response is a 429 and retries remain.
func (rc RetryConfig) shouldRetry(statusCode int, attempt int) bool {
	return rc.Enabled && statusCode == http.StatusTooManyRequests && attempt < rc.MaxRetries
}

// sleepWithContext sleeps for the given duration, returning early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// logRetry logs a retry attempt if a logger is available.
func logRetry(logger *slog.Logger, attempt, maxRetries int, backoff time.Duration, path string) {
	if logger == nil {
		return
	}
	logger.Warn("ojs: retrying rate-limited request",
		slog.Int("attempt", attempt+1),
		slog.Int("max_retries", maxRetries),
		slog.Duration("backoff", backoff),
		slog.String("path", path),
	)
}
