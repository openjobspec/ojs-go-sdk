package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudflareHandler_ServeHTTP_Success(t *testing.T) {
	h := NewCloudflareHandler()
	h.Register("email.send", func(_ context.Context, job JobEvent) error {
		return nil
	})

	body := `{"job":{"id":"job-1","type":"email.send","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
}

func TestCloudflareHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	h := NewCloudflareHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestCloudflareHandler_HandleFetchEvent_Success(t *testing.T) {
	h := NewCloudflareHandler()
	h.Register("image.resize", func(_ context.Context, job JobEvent) error {
		if job.ID != "job-42" {
			return fmt.Errorf("unexpected job ID: %s", job.ID)
		}
		return nil
	})

	body := `{"id":"job-42","type":"image.resize","queue":"media","args":[{"width":800}],"attempt":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
}

func TestCloudflareHandler_HandleFetchEvent_NoHandler(t *testing.T) {
	h := NewCloudflareHandler()

	body := `{"id":"job-1","type":"unknown.type","queue":"default","args":[],"attempt":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "failed" {
		t.Errorf("expected status 'failed', got '%s'", resp.Status)
	}
	if resp.Error == nil || !resp.Error.Retryable {
		t.Error("expected retryable error")
	}
}

func TestCloudflareHandler_HandleFetchEvent_InvalidJSON(t *testing.T) {
	h := NewCloudflareHandler()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{broken`))
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCloudflareHandler_HandleFetchEvent_MethodNotAllowed(t *testing.T) {
	h := NewCloudflareHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestCloudflareHandler_HandleJob_Success(t *testing.T) {
	h := NewCloudflareHandler()

	var processed bool
	h.Register("test.job", func(_ context.Context, _ JobEvent) error {
		processed = true
		return nil
	})

	err := h.HandleJob(context.Background(), JobEvent{
		ID:      "job-1",
		Type:    "test.job",
		Queue:   "default",
		Attempt: 1,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("handler was not called")
	}
}

func TestCloudflareHandler_HandleJob_NoHandler(t *testing.T) {
	h := NewCloudflareHandler()

	err := h.HandleJob(context.Background(), JobEvent{
		ID:   "job-1",
		Type: "missing.type",
	})

	if err == nil {
		t.Fatal("expected error for unregistered job type")
	}
}

func TestCloudflareHandler_ImplementsHTTPHandler(t *testing.T) {
	// Verify CloudflareHandler satisfies http.Handler interface.
	var _ http.Handler = NewCloudflareHandler()
}

func TestCloudflareHandler_WithOptions(t *testing.T) {
	h := NewCloudflareHandler(
		WithCloudflareOJSURL("https://ojs.example.com"),
	)

	if h.inner.ojsURL != "https://ojs.example.com" {
		t.Errorf("expected ojsURL 'https://ojs.example.com', got '%s'", h.inner.ojsURL)
	}
}

func TestCloudflareHandler_WithTimeoutOption(t *testing.T) {
	h := NewCloudflareHandler(
		WithCloudflareTimeout(5 * time.Second),
		WithCloudflareMaxBodySize(2048),
	)

	if h.inner.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", h.inner.timeout)
	}
	if h.inner.maxBodySize != 2048 {
		t.Errorf("expected maxBodySize 2048, got %d", h.inner.maxBodySize)
	}
}

func TestCloudflareHandler_HandleFetchEvent_MissingJobFields(t *testing.T) {
	h := NewCloudflareHandler()

	body := `{"id":"job-1","queue":"default","args":[],"attempt":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "invalid_request" {
		t.Error("expected invalid_request error for missing fields")
	}
}

func TestCloudflareHandler_HandleFetchEvent_Timeout(t *testing.T) {
	h := NewCloudflareHandler(
		WithCloudflareTimeout(50 * time.Millisecond),
	)
	h.Register("slow.job", func(ctx context.Context, _ JobEvent) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	body := `{"id":"job-1","type":"slow.job","queue":"default","args":[],"attempt":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleFetchEvent(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "failed" {
		t.Errorf("expected status 'failed', got '%s'", resp.Status)
	}
	if resp.Error == nil || !resp.Error.Retryable {
		t.Error("expected retryable error for timeout")
	}
}

func TestCloudflareHandler_ServeHTTP_MissingJobFields(t *testing.T) {
	h := NewCloudflareHandler()

	body := `{"job":{"id":"job-1","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

