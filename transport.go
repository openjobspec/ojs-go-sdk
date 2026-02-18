package ojs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ojsContentType     = "application/openjobspec+json"
	ojsVersion         = "1.0.0-rc.1"
	basePath           = "/ojs/v1"
	maxResponseBodyLen = 10 << 20 // 10 MB
)

// transport is a thin HTTP wrapper for OJS API communication.
type transport struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
	headers    map[string]string
}

func newTransport(baseURL string, cfg clientConfig) *transport {
	client := cfg.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	return &transport{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
		authToken:  cfg.authToken,
		headers:    cfg.headers,
	}
}

func newWorkerTransport(baseURL string, cfg workerConfig) *transport {
	client := cfg.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	return &transport{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
		authToken:  cfg.authToken,
	}
}

// do executes an HTTP request and decodes the JSON response.
func (t *transport) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("ojs: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := t.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("ojs: create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", ojsContentType)
	}
	req.Header.Set("Accept", ojsContentType)
	req.Header.Set("OJS-Version", ojsVersion)

	if t.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.authToken)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ojs: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyLen))
	if err != nil {
		return fmt.Errorf("ojs: read response: %w", err)
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
// Returns zero if the header is absent or cannot be parsed as seconds.
func parseRetryAfter(header http.Header) time.Duration {
	raw := header.Get("Retry-After")
	if raw == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
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
