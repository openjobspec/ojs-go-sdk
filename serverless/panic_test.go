package serverless

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func quietHandler() *LambdaHandler {
	return newInsecureLambdaHandler(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
}

// TestSQSPanicFailsOnlyOffendingMessage is a regression test: a panicking
// handler used to unwind through HandleSQS and abort the whole invocation, so
// AWS retried every message in the batch instead of just the failed one.
func TestSQSPanicFailsOnlyOffendingMessage(t *testing.T) {
	h := quietHandler()
	h.Register("boom.job", func(context.Context, JobEvent) error { panic("handler exploded") })
	ok := 0
	h.Register("ok.job", func(context.Context, JobEvent) error { ok++; return nil })

	body := func(id, typ string) string {
		b, _ := json.Marshal(JobEvent{ID: id, Type: typ, Args: json.RawMessage(`[]`)})
		return string(b)
	}
	event := SQSEvent{Records: []SQSMessage{
		{MessageID: "m1", Body: body("j1", "ok.job")},
		{MessageID: "m2", Body: body("j2", "boom.job")},
		{MessageID: "m3", Body: body("j3", "ok.job")},
	}}

	resp, err := h.HandleSQS(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleSQS returned error: %v", err)
	}
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "m2" {
		t.Fatalf("failures = %+v, want only m2", resp.BatchItemFailures)
	}
	if ok != 2 {
		t.Errorf("successful handlers ran %d times, want 2 (the batch must keep going)", ok)
	}
}

func TestHandleDirectPanicBecomesJobFailure(t *testing.T) {
	h := quietHandler()
	h.Register("boom.job", func(context.Context, JobEvent) error { panic("nope") })

	resp, err := h.HandleDirect(context.Background(), JobEvent{ID: "j1", Type: "boom.job"})
	if err != nil {
		t.Fatalf("HandleDirect returned error: %v", err)
	}
	if resp.Status != "failed" {
		t.Errorf("status = %s, want failed", resp.Status)
	}
	if !strings.Contains(resp.Error, "panic in job handler") {
		t.Errorf("error = %q, want it to identify the panic", resp.Error)
	}
}

func TestHandleHTTPPanicBecomesJobFailure(t *testing.T) {
	h := quietHandler()
	h.Register("boom.job", func(context.Context, JobEvent) error { panic("nope") })

	body := `{"job":{"id":"j1","type":"boom.job","args":[]},"worker_id":"w1","delivery_id":"d1"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleHTTP().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp PushDeliveryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "failed" || resp.Error == nil {
		t.Fatalf("response = %+v, want a failed status with an error", resp)
	}
	if !resp.Error.Retryable {
		t.Error("a panicking handler should yield a retryable failure")
	}
}

// TestPushBindingSharedAcrossPlatforms characterizes that the Lambda, Vercel,
// and Cloudflare entry points implement the same push-delivery contract.
func TestPushBindingSharedAcrossPlatforms(t *testing.T) {
	envelope := `{"job":{"id":"j1","type":"a.job","args":[]},"worker_id":"w1","delivery_id":"d1"}`
	bare := `{"id":"j1","type":"a.job","args":[]}`

	serve := map[string]struct {
		body     string
		fn       func(http.ResponseWriter, *http.Request)
		setLimit func(int64)
	}{}

	lambda := quietHandler()
	lambda.Register("a.job", func(context.Context, JobEvent) error { return nil })
	serve["lambda"] = struct {
		body     string
		fn       func(http.ResponseWriter, *http.Request)
		setLimit func(int64)
	}{envelope, lambda.HandleHTTP(), func(n int64) { lambda.maxBodySize = n }}

	vercel := newInsecureVercelHandler(WithVercelLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	vercel.Register("a.job", func(context.Context, JobEvent) error { return nil })
	serve["vercel"] = struct {
		body     string
		fn       func(http.ResponseWriter, *http.Request)
		setLimit func(int64)
	}{envelope, vercel.ServeHTTP, func(n int64) { vercel.inner.maxBodySize = n }}

	cf := newInsecureCloudflareHandler(WithCloudflareLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	cf.Register("a.job", func(context.Context, JobEvent) error { return nil })
	serve["cloudflare-fetch"] = struct {
		body     string
		fn       func(http.ResponseWriter, *http.Request)
		setLimit func(int64)
	}{bare, cf.HandleFetchEvent, func(n int64) { cf.inner.maxBodySize = n }}

	for name, tc := range serve {
		t.Run(name+"/success", func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.fn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var resp PushDeliveryResponse
			_ = json.NewDecoder(w.Body).Decode(&resp)
			if resp.Status != "completed" {
				t.Errorf("status = %s, want completed", resp.Status)
			}
		})
		t.Run(name+"/wrong-method", func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.fn(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", w.Code)
			}
		})
		t.Run(name+"/missing-fields", func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.fn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
		t.Run(name+"/oversize", func(t *testing.T) {
			tc.setLimit(10)
			defer tc.setLimit(DefaultMaxBodySize)
			w := httptest.NewRecorder()
			tc.fn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			if w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status = %d, want 413", w.Code)
			}
		})
	}
}

// TestVercelRequestIDPropagates locks the one platform-specific behaviour kept
// in the Vercel adapter after the shared binding extraction.
func TestVercelRequestIDPropagates(t *testing.T) {
	h := newInsecureVercelHandler(WithVercelLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	var seen string
	h.Register("a.job", func(ctx context.Context, _ JobEvent) error {
		seen = VercelRequestID(ctx)
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"job":{"id":"j1","type":"a.job","args":[]}}`))
	req.Header.Set("X-Vercel-Id", "iad1::abc123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "iad1::abc123" {
		t.Errorf("VercelRequestID = %q, want iad1::abc123", seen)
	}
}
