package ojs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	ojsContentType = "application/openjobspec+json"
	ojsVersion     = "1.0.0-rc.1"
	basePath       = "/ojs/v1"
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

	req.Header.Set("Content-Type", ojsContentType)
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ojs: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseErrorResponse(respBody, resp.StatusCode)
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
func parseErrorResponse(body []byte, statusCode int) error {
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

	return &Error{
		Code:       errResp.Error.Code,
		Message:    errResp.Error.Message,
		Retryable:  errResp.Error.Retryable,
		Details:    errResp.Error.Details,
		RequestID:  errResp.Error.RequestID,
		HTTPStatus: statusCode,
	}
}
