package ojs

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"time"
)

// RetryConfig configures automatic HTTP request retries for rate-limited
// and transient server error responses.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts. Default: 3.
	MaxRetries int

	// MinBackoff is the minimum backoff duration between retries. Default: 500ms.
	MinBackoff time.Duration

	// MaxBackoff is the maximum backoff duration between retries. Default: 30s.
	MaxBackoff time.Duration

	// Enabled controls whether automatic retry on 429 is active. Default: true.
	Enabled bool

	// RetryServerErrors enables retry on 502, 503, 504 responses. Default: true.
	RetryServerErrors bool
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		MinBackoff:        500 * time.Millisecond,
		MaxBackoff:        30 * time.Second,
		Enabled:           true,
		RetryServerErrors: true,
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
	return time.Duration(backoff * jitterFactor())
}

// jitterFactor returns a retry-backoff multiplier in [0.5, 1.0).
//
// The value comes from the system CSPRNG so the SDK seeds no package-level PRNG
// of its own. Cost is irrelevant here: jitter is computed only on the rare
// 429/5xx retry path, immediately before sleeping for hundreds of milliseconds.
func jitterFactor() float64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Unreachable as of Go 1.24, where crypto/rand.Read cannot fail. Full
		// backoff is the safe fallback: it never retries sooner than the
		// unjittered schedule would.
		return 1.0
	}
	// Keep the top 53 bits so the quotient is exactly representable as a
	// float64 in [0, 1).
	return 0.5 + float64(binary.BigEndian.Uint64(b[:])>>11)/(1<<53)*0.5
}

// shouldRetry returns true if the response status code is retryable and retries remain.
// Retryable: 429 (Too Many Requests), 502, 503, 504 (transient server errors).
//
// A 429 with a syntactically valid Retry-After is the narrow exception to the
// operation-safety gate: it is an explicit server instruction to retry after
// throttling and applies to every method. Other retryable statuses remain
// gated by retryEligible because a 5xx response may arrive after a
// non-idempotent POST was committed.
func (rc RetryConfig) shouldRetry(statusCode int, attempt int, retryEligible, retryAfterValid bool) bool {
	if !rc.Enabled || attempt >= rc.MaxRetries {
		return false
	}
	if statusCode == http.StatusTooManyRequests && retryAfterValid {
		return true
	}
	if !retryEligible {
		return false
	}
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	if rc.RetryServerErrors {
		switch statusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

// shouldRetryConnectionError reports whether a transport-level failure --  no
// HTTP response at all, as opposed to an error status code -- is safe to
// retry.
//
// A pre-dial failure (DNS lookup failure, connection refused, or any other
// error establishing the TCP/TLS connection) is safe to retry regardless of
// the operation: the connection was never established, so the request was
// never transmitted and nothing could have been written. Every other
// transport failure -- a write that failed partway, a read timeout waiting
// for the response, a connection reset after the request was sent -- cannot
// be told apart from "the server received and processed the request but the
// response was lost in transit", so it is retried only when retryEligible
// already says the operation itself is safe to repeat regardless of a prior
// attempt's outcome.
func shouldRetryConnectionError(err error, retryEligible bool) bool {
	if isPreDialFailure(err) {
		return true
	}
	return retryEligible
}

// isPreDialFailure reports whether err represents a failure to establish a
// connection -- including the DNS lookup that precedes it -- rather than a
// failure once the connection existed and bytes may have started flowing.
// net/http always surfaces these as a *net.OpError with Op == "dial".
func isPreDialFailure(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
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
	logger.Warn("ojs: retrying request",
		slog.Int("attempt", attempt+1),
		slog.Int("max_retries", maxRetries),
		slog.Duration("backoff", backoff),
		slog.String("path", path),
	)
}
