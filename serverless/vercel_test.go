package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVercelHandler_ServeHTTP_Success(t *testing.T) {
	h := newInsecureVercelHandler()
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

func TestVercelHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	h := NewVercelHandler()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/", nil)
		w := httptest.NewRecorder()

		h.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected status 405, got %d", method, w.Code)
		}
	}
}

func TestVercelHandler_ServeHTTP_InvalidJSON(t *testing.T) {
	h := newInsecureVercelHandler()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{broken`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestVercelHandler_ServeHTTP_MissingJobFields(t *testing.T) {
	h := newInsecureVercelHandler()

	// Missing type
	body := `{"job":{"id":"job-1","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "invalid_request" {
		t.Error("expected invalid_request error")
	}
}

func TestVercelHandler_ServeHTTP_HandlerError(t *testing.T) {
	h := newInsecureVercelHandler()
	h.Register("fail.job", func(_ context.Context, _ JobEvent) error {
		return fmt.Errorf("something went wrong")
	})

	body := `{"job":{"id":"job-1","type":"fail.job","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

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

func TestVercelHandler_ServeHTTP_NoHandler(t *testing.T) {
	h := newInsecureVercelHandler()

	body := `{"job":{"id":"job-1","type":"unknown","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

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
}

func TestVercelHandler_ServeHTTP_VercelRequestID(t *testing.T) {
	h := newInsecureVercelHandler()

	var capturedID string
	h.Register("trace.job", func(ctx context.Context, _ JobEvent) error {
		capturedID = VercelRequestID(ctx)
		return nil
	})

	body := `{"job":{"id":"job-1","type":"trace.job","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Vercel-Id", "iad1::abcdef-1234")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if capturedID != "iad1::abcdef-1234" {
		t.Errorf("expected vercel request ID 'iad1::abcdef-1234', got '%s'", capturedID)
	}
}

func TestVercelHandler_ServeHTTP_NoVercelRequestID(t *testing.T) {
	h := newInsecureVercelHandler()

	var capturedID string
	h.Register("trace.job", func(ctx context.Context, _ JobEvent) error {
		capturedID = VercelRequestID(ctx)
		return nil
	})

	body := `{"job":{"id":"job-1","type":"trace.job","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if capturedID != "" {
		t.Errorf("expected empty vercel request ID, got '%s'", capturedID)
	}
}

func TestVercelHandler_HandleJob_Success(t *testing.T) {
	h := NewVercelHandler()

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

func TestVercelHandler_HandleJob_NoHandler(t *testing.T) {
	h := NewVercelHandler()

	err := h.HandleJob(context.Background(), JobEvent{
		ID:   "job-1",
		Type: "missing.type",
	})

	if err == nil {
		t.Fatal("expected error for unregistered job type")
	}
}

func TestVercelHandler_ImplementsHTTPHandler(t *testing.T) {
	var _ http.Handler = NewVercelHandler()
}

func TestVercelHandler_WithOptions(t *testing.T) {
	h := NewVercelHandler(
		WithVercelOJSURL("https://ojs.example.com"),
	)

	if h.inner.ojsURL != "https://ojs.example.com" {
		t.Errorf("expected ojsURL 'https://ojs.example.com', got '%s'", h.inner.ojsURL)
	}
}

func TestVercelRequestID_EmptyContext(t *testing.T) {
	id := VercelRequestID(context.Background())
	if id != "" {
		t.Errorf("expected empty string, got '%s'", id)
	}
}
