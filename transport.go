package ojs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ojsContentType     = "application/openjobspec+json"
	ojsVersion         = "1.0"
	sdkVersion         = "0.2.0"
	defaultUserAgent   = "ojs-go-sdk/" + sdkVersion
	basePath           = "/ojs/v1"
	maxResponseBodyLen = 10 << 20 // 10 MB
)

// transport is a thin HTTP wrapper for OJS API communication.
type transport struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string
	userAgent   string
	headers     map[string]string
	retryConfig RetryConfig
	logger      *slog.Logger
}

const defaultHTTPTimeout = 30 * time.Second

// defaultHTTPClient is a package-level HTTP client shared across all OJS
// clients that don't provide their own. This enables connection pooling
// and avoids creating a new transport per NewClient() call.
var defaultHTTPClient = &http.Client{
	Timeout: defaultHTTPTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
	},
}

func newTransport(baseURL string, cfg clientConfig) *transport {
	rc := DefaultRetryConfig()
	if cfg.retryConfig != nil {
		rc = *cfg.retryConfig
	}
	return &transport{
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  cfg.resolveHTTPClient(),
		authToken:   cfg.authToken,
		userAgent:   cfg.userAgent,
		headers:     cfg.headers,
		retryConfig: rc,
		logger:      cfg.logger,
	}
}

// resolveHTTPClient picks the *http.Client a transport should use.
//
// The three cases are genuinely distinct and must stay distinguishable:
// an explicitly supplied client wins outright; an explicit zero timeout
// (httpTimeoutSet with httpTimeout == 0) means "no timeout at all" and is not
// the same as leaving the timeout unconfigured, which keeps the shared default
// client so connection pooling is preserved across every ojs.Client.
func (c clientConfig) resolveHTTPClient() *http.Client {
	switch {
	case c.httpClient != nil:
		return c.httpClient
	case c.httpTimeout > 0:
		return &http.Client{
			Timeout:   c.httpTimeout,
			Transport: defaultHTTPClient.Transport,
		}
	case c.httpTimeoutSet:
		return &http.Client{Transport: defaultHTTPClient.Transport} // no timeout
	default:
		return defaultHTTPClient
	}
}

func newWorkerTransport(baseURL string, cfg workerConfig) *transport {
	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &transport{
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  client,
		authToken:   cfg.authToken,
		retryConfig: DefaultRetryConfig(),
		logger:      cfg.logger,
	}
}

// retryEligibleForMethod reports whether automatic retry is ever appropriate
// for the given HTTP method, independent of which specific operation it
// performs.
//
// GET/HEAD are unconditionally safe: they have no side effects. DELETE is
// treated as eligible because every DELETE this SDK issues -- job/workflow
// cancellation, checkpoint/dead-letter/cron removal -- is a "make sure X is
// gone" operation that reaches the same end state whether it runs once or
// twice. PUT would be as well, for the same reason, if this SDK used it.
//
// POST defaults to NOT eligible: most POST operations here create, reserve, or
// append something -- enqueue, batch enqueue, workflow creation, worker
// fetch/ack/nack/heartbeat, dead-letter retry, cron registration, durable
// checkpoint save -- and carry no idempotency key, so a lost response after
// the server already committed the write must not trigger a silent duplicate.
// The rare POST operation this SDK has specifically vetted as idempotent
// (queue pause/resume) opts in explicitly via postIdempotent; this default is
// not weakened to accommodate it.
func retryEligibleForMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodPut:
		return true
	default:
		return false
	}
}

// do executes an HTTP request and decodes the JSON response.
// It automatically retries on 429 (Too Many Requests) and, for eligible
// operations, transient 5xx and connection failures, respecting the
// Retry-After header when present. Retry eligibility is derived from method
// per retryEligibleForMethod; use postIdempotent for a POST operation that has
// been specifically vetted as safe to repeat.
func (t *transport) do(ctx context.Context, method, path string, body any, result any) error {
	return t.doClassified(ctx, method, path, body, result, retryEligibleForMethod(method))
}

// postIdempotent performs a POST for an operation this SDK has vetted as
// idempotent -- repeating it reaches the same end state, so a lost response
// after the server already applied it is not a duplicate action -- and is
// therefore eligible for the same automatic retry as a GET/DELETE despite the
// method.
//
// Only call this for an operation that is genuinely safe to apply more than
// once regardless of how many times a retry lands (e.g. "set queue state to
// paused"). Never use it for one that creates, reserves, or appends something
// (enqueue, fetch, checkpoint save, ...): those stay on post, which defaults
// retries off because this SDK has no per-request idempotency key to make
// that safe.
func (t *transport) postIdempotent(ctx context.Context, path string, body any, result any) error {
	return t.doClassified(ctx, http.MethodPost, path, body, result, true)
}

// doClassified is the shared request loop behind do and postIdempotent.
// retryEligible decides both whether a transient status code triggers a retry
// and whether a connection failure that is not provably pre-write does.
//
// The loop body is deliberately only the retry decision: composing the OJS
// request envelope and bounding the response body are separate concerns that
// each have their own rules and live in their own functions
// (connectionRetryDecision, statusRetryDecision), and neither depends on
// which attempt this is.
func (t *transport) doClassified(ctx context.Context, method, path string, body any, result any, retryEligible bool) error {
	var bodyData []byte
	if body != nil {
		var err error
		bodyData, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("ojs: marshal request: %w", err)
		}
	}

	url := t.baseURL + path

	for attempt := 0; ; attempt++ {
		req, err := t.newRequest(ctx, method, url, bodyData)
		if err != nil {
			return err
		}

		resp, err := t.httpClient.Do(req)
		if err != nil {
			retry, retryErr := t.connectionRetryDecision(ctx, attempt, path, retryEligible, err)
			if retry {
				continue
			}
			return retryErr
		}

		respBody, err := readLimitedBody(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		retry, err := t.statusRetryDecision(ctx, attempt, path, retryEligible, resp)
		if err != nil {
			return err
		}
		if retry {
			continue
		}

		if resp.StatusCode >= 400 {
			return parseErrorResponse(respBody, resp.StatusCode, resp.Header)
		}

		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("ojs: unmarshal response: %w", err)
			}
		}

		return nil
	}
}

// connectionRetryDecision decides what to do about a transport-level failure
// (no HTTP response at all). retry is true when the caller should wait out
// the returned backoff (already done by the time this returns) and try the
// request again; otherwise err is what the caller should return -- either the
// original connection error, wrapped, or a context error from an interrupted
// backoff wait.
func (t *transport) connectionRetryDecision(
	ctx context.Context, attempt int, path string, retryEligible bool, connErr error,
) (retry bool, err error) {
	if !t.retryConfig.Enabled || attempt >= t.retryConfig.MaxRetries || !shouldRetryConnectionError(connErr, retryEligible) {
		return false, fmt.Errorf("ojs: request failed: %w", connErr)
	}
	backoff := t.retryConfig.retryBackoff(attempt, 0)
	logRetry(t.logger, attempt, t.retryConfig.MaxRetries, backoff, path)
	if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
		return false, sleepErr
	}
	return true, nil
}

// statusRetryDecision decides whether a response's status code should be
// retried, respecting Retry-After when present. retry is true when the
// caller should try the request again (the backoff has already been waited
// out); err is non-nil only when the wait itself was interrupted by ctx.
func (t *transport) statusRetryDecision(
	ctx context.Context, attempt int, path string, retryEligible bool, resp *http.Response,
) (retry bool, err error) {
	retryAfter, retryAfterValid := parseRetryAfterValue(resp.Header)
	if !t.retryConfig.shouldRetry(resp.StatusCode, attempt, retryEligible, retryAfterValid) {
		return false, nil
	}
	backoff := t.retryConfig.retryBackoff(attempt, retryAfter)
	logRetry(t.logger, attempt, t.retryConfig.MaxRetries, backoff, path)
	if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
		return false, sleepErr
	}
	return true, nil
}

// newRequest builds one attempt's request with the OJS protocol headers.
//
// A fresh request is built per attempt rather than reused: the body reader must
// be rewound and X-Request-ID must be unique so a retried call is individually
// traceable in server logs.
func (t *transport) newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("ojs: create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", ojsContentType)
	}
	req.Header.Set("Accept", ojsContentType)
	req.Header.Set("OJS-Version", ojsVersion)
	req.Header.Set("X-Request-ID", generateRequestID())

	ua := t.userAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)

	if t.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.authToken)
	}
	// Caller-supplied headers are applied last so they can override the
	// defaults above.
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// readLimitedBody reads a response body, refusing anything over the limit.
//
// It reads one byte past the limit so an over-limit body can be detected
// exactly: a body of precisely maxResponseBodyLen bytes is complete and must not
// be rejected. Closing the body stays with the caller that owns the response.
func readLimitedBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxResponseBodyLen+1))
	if err != nil {
		return nil, fmt.Errorf("ojs: read response: %w", err)
	}
	if int64(len(data)) > maxResponseBodyLen {
		return nil, fmt.Errorf("ojs: response body exceeds %d bytes limit — response truncated", maxResponseBodyLen)
	}
	return data, nil
}

// get performs an HTTP GET request.
func (t *transport) get(ctx context.Context, path string, result any) error {
	return t.do(ctx, http.MethodGet, path, nil, result)
}

// post performs an HTTP POST request.
func (t *transport) post(ctx context.Context, path string, body any, result any) error {
	return t.do(ctx, http.MethodPost, path, body, result)
}

// delete performs an HTTP DELETE request.
func (t *transport) delete(ctx context.Context, path string, result any) error {
	return t.do(ctx, http.MethodDelete, path, nil, result)
}

// parseErrorResponse parses an OJS error response body into an *Error.
func parseErrorResponse(body []byte, statusCode int, header http.Header) error {
	var errResp struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Retryable bool           `json:"retryable"`
			Details   map[string]any `json:"details,omitempty"`
			RequestID string         `json:"request_id,omitempty"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err != nil {
		return &Error{
			Code:       "unknown",
			Message:    fmt.Sprintf("HTTP %d: %s", statusCode, string(body)),
			HTTPStatus: statusCode,
		}
	}

	retryAfter := parseRetryAfter(header)
	rateLimit := parseRateLimitHeaders(header, retryAfter)

	return &Error{
		Code:       errResp.Error.Code,
		Message:    errResp.Error.Message,
		Retryable:  errResp.Error.Retryable,
		Details:    errResp.Error.Details,
		RequestID:  errResp.Error.RequestID,
		HTTPStatus: statusCode,
		RetryAfter: retryAfter,
		RateLimit:  rateLimit,
	}
}

// parseRetryAfter extracts the Retry-After header value as a time.Duration.
//
// RFC 9110 §10.2.3 defines two forms: delay-seconds and an HTTP-date. Both are
// accepted; an HTTP-date is converted to the remaining delay relative to now
// and clamped at zero for dates in the past. Returns zero if the header is
// absent or matches neither form.
func parseRetryAfter(header http.Header) time.Duration {
	retryAfter, _ := parseRetryAfterValue(header)
	return retryAfter
}

// parseRetryAfterAt is parseRetryAfter with an injectable clock for testing.
func parseRetryAfterAt(header http.Header, now time.Time) time.Duration {
	retryAfter, _ := parseRetryAfterAtValue(header, now)
	return retryAfter
}

// parseRetryAfterValue returns the parsed delay and whether the header is
// syntactically valid. Valid zero-delay values remain distinguishable from an
// absent or malformed header for retry classification.
func parseRetryAfterValue(header http.Header) (time.Duration, bool) {
	return parseRetryAfterAtValue(header, time.Now())
}

func parseRetryAfterAtValue(header http.Header, now time.Time) (time.Duration, bool) {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return 0, false
		}
		return time.Duration(seconds * float64(time.Second)), true
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// parseRateLimitHeaders extracts rate limit metadata from response headers.
// Returns nil if none of the rate limit headers are present.
func parseRateLimitHeaders(header http.Header, retryAfter time.Duration) *RateLimitInfo {
	limitStr := header.Get("X-RateLimit-Limit")
	remainingStr := header.Get("X-RateLimit-Remaining")
	resetStr := header.Get("X-RateLimit-Reset")

	if limitStr == "" && remainingStr == "" && resetStr == "" && retryAfter == 0 {
		return nil
	}

	info := &RateLimitInfo{RetryAfter: retryAfter}
	if v, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
		info.Limit = v
	}
	if v, err := strconv.ParseInt(remainingStr, 10, 64); err == nil {
		info.Remaining = v
	}
	if v, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
		info.Reset = v
	}
	return info
}

// generateRequestID produces a random UUIDv4 string for request correlation.
func generateRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
