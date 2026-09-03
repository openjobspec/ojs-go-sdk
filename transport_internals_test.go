package ojs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// resolveHTTPClient replaced an if/else chain. The four cases are genuinely
// different and easy to collapse by accident — in particular "timeout explicitly
// set to 0" (no timeout at all) is not the same as "timeout not configured"
// (keep the shared pooled client).
func TestResolveHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}

	t.Run("explicit client wins over everything", func(t *testing.T) {
		cfg := clientConfig{httpClient: custom, httpTimeout: 3 * time.Second, httpTimeoutSet: true}
		if got := cfg.resolveHTTPClient(); got != custom {
			t.Errorf("resolveHTTPClient() = %p, want the supplied client %p", got, custom)
		}
	})

	t.Run("positive timeout builds a client sharing the pooled transport", func(t *testing.T) {
		cfg := clientConfig{httpTimeout: 3 * time.Second, httpTimeoutSet: true}
		got := cfg.resolveHTTPClient()
		if got == defaultHTTPClient {
			t.Fatal("resolveHTTPClient() returned the shared default; a timeout override needs its own client")
		}
		if got.Timeout != 3*time.Second {
			t.Errorf("Timeout = %v, want 3s", got.Timeout)
		}
		if got.Transport != defaultHTTPClient.Transport {
			t.Error("connection pooling lost: expected the shared default transport")
		}
	})

	t.Run("explicit zero timeout means no timeout", func(t *testing.T) {
		cfg := clientConfig{httpTimeout: 0, httpTimeoutSet: true}
		got := cfg.resolveHTTPClient()
		if got == defaultHTTPClient {
			t.Fatal("an explicit zero timeout must not reuse the 30s default client")
		}
		if got.Timeout != 0 {
			t.Errorf("Timeout = %v, want 0 (no timeout)", got.Timeout)
		}
		if got.Transport != defaultHTTPClient.Transport {
			t.Error("connection pooling lost: expected the shared default transport")
		}
	})

	t.Run("unset timeout keeps the shared default client", func(t *testing.T) {
		cfg := clientConfig{}
		if got := cfg.resolveHTTPClient(); got != defaultHTTPClient {
			t.Errorf("resolveHTTPClient() = %p, want the shared default %p", got, defaultHTTPClient)
		}
	})
}

// newRequest owns the OJS request envelope. Every attempt must carry the
// protocol headers and a fresh request ID.
func TestNewRequestSetsProtocolHeaders(t *testing.T) {
	tr := newTransport("http://ojs.example", clientConfig{
		authToken: "tok",
		userAgent: "custom-agent/1.0",
		headers:   map[string]string{"X-Tenant": "acme", "Accept": "override/type"},
	})

	req, err := tr.newRequest(context.Background(), http.MethodPost, "http://ojs.example/ojs/v1/jobs", []byte(`{}`))
	if err != nil {
		t.Fatalf("newRequest() = %v", err)
	}

	if got := req.Header.Get("Content-Type"); got != ojsContentType {
		t.Errorf("Content-Type = %q, want %q", got, ojsContentType)
	}
	if got := req.Header.Get("OJS-Version"); got != ojsVersion {
		t.Errorf("OJS-Version = %q, want %q", got, ojsVersion)
	}
	if got := req.Header.Get("User-Agent"); got != "custom-agent/1.0" {
		t.Errorf("User-Agent = %q, want the configured agent", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok")
	}
	if got := req.Header.Get("X-Tenant"); got != "acme" {
		t.Errorf("X-Tenant = %q, want %q", got, "acme")
	}
	// Caller headers are applied last and therefore win.
	if got := req.Header.Get("Accept"); got != "override/type" {
		t.Errorf("Accept = %q, want the caller override", got)
	}
	if req.Header.Get("X-Request-ID") == "" {
		t.Error("X-Request-ID must be set on every attempt")
	}

	// A body-less request must not claim a Content-Type.
	bodyless, err := tr.newRequest(context.Background(), http.MethodGet, "http://ojs.example/ojs/v1/jobs", nil)
	if err != nil {
		t.Fatalf("newRequest() = %v", err)
	}
	if got := bodyless.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q on a body-less request, want empty", got)
	}

	// Each attempt gets its own request ID so a retry is individually traceable.
	second, err := tr.newRequest(context.Background(), http.MethodPost, "http://ojs.example/ojs/v1/jobs", []byte(`{}`))
	if err != nil {
		t.Fatalf("newRequest() = %v", err)
	}
	if req.Header.Get("X-Request-ID") == second.Header.Get("X-Request-ID") {
		t.Error("X-Request-ID must differ between attempts")
	}

	// The body must be re-readable per attempt.
	data, err := io.ReadAll(req.Body)
	if err != nil || string(data) != "{}" {
		t.Errorf("body = %q, %v; want %q", data, err, "{}")
	}
}

// readLimitedBody must accept a body of exactly the limit and reject only a
// strictly larger one.
func TestReadLimitedBodyBoundary(t *testing.T) {
	exact := strings.Repeat("a", int(maxResponseBodyLen))
	got, err := readLimitedBody(strings.NewReader(exact))
	if err != nil {
		t.Fatalf("readLimitedBody(limit bytes) = %v, want nil", err)
	}
	if int64(len(got)) != maxResponseBodyLen {
		t.Errorf("read %d bytes, want %d", len(got), maxResponseBodyLen)
	}

	if _, err := readLimitedBody(strings.NewReader(exact + "a")); err == nil {
		t.Error("readLimitedBody(limit+1 bytes) = nil error, want a truncation error")
	}
}

// jitterFactor now draws from crypto/rand instead of the process-global
// math/rand. The contract callers depend on is the range.
func TestJitterFactorRange(t *testing.T) {
	seen := make(map[float64]bool)
	for i := 0; i < 2000; i++ {
		f := jitterFactor()
		if f < 0.5 || f >= 1.0 {
			t.Fatalf("jitterFactor() = %v, want [0.5, 1.0)", f)
		}
		seen[f] = true
	}
	if len(seen) < 1000 {
		t.Errorf("jitterFactor() produced only %d distinct values in 2000 draws", len(seen))
	}
}

func TestRetryBackoffStaysWithinJitterBand(t *testing.T) {
	rc := DefaultRetryConfig()
	for attempt := 0; attempt < 4; attempt++ {
		base := float64(rc.MinBackoff) * float64(int64(1)<<attempt)
		if base > float64(rc.MaxBackoff) {
			base = float64(rc.MaxBackoff)
		}
		for i := 0; i < 200; i++ {
			got := rc.retryBackoff(attempt, 0)
			if float64(got) < base*0.5 || float64(got) >= base {
				t.Fatalf("retryBackoff(%d) = %v, want [%v, %v)",
					attempt, got, time.Duration(base*0.5), time.Duration(base))
			}
		}
	}
}

// --- Subscribe response-body ownership ---

type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error {
	b.closed.Store(true)
	return nil
}

type stubRoundTripper struct {
	status int
	body   io.ReadCloser
}

func (rt *stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: rt.status,
		Status:     http.StatusText(rt.status),
		Header:     make(http.Header),
		Body:       rt.body,
	}, nil
}

// Subscribe hands the response body to a reader goroutine. Every path that
// returns without starting that goroutine still owns the body and must close
// it, or a rejected subscribe leaks the connection until the idle timeout.
func TestSubscribeClosesBodyWhenServerRejectsStream(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("forbidden")}
	client, err := NewClient("http://ojs.example",
		WithHTTPClient(&http.Client{Transport: &stubRoundTripper{status: http.StatusForbidden, body: body}}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.Subscribe(context.Background(), "queue:default", func(Event) {}); err == nil {
		t.Fatal("Subscribe must fail when the server rejects the stream")
	}
	if !body.closed.Load() {
		t.Error("Subscribe must close the response body when it does not start streaming")
	}
}

// The success path must NOT close the body from Subscribe itself: the reader
// goroutine owns it until the subscription is cancelled.
func TestSubscribeKeepsBodyOpenWhileStreaming(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("data: {}\n\n")}
	client, err := NewClient("http://ojs.example",
		WithHTTPClient(&http.Client{Transport: &stubRoundTripper{status: http.StatusOK, body: body}}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sub, err := client.Subscribe(context.Background(), "queue:default", func(Event) {})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	sub.Cancel() // Cancel waits for the reader goroutine to finish.

	if !body.closed.Load() {
		t.Error("the reader goroutine must close the response body when the stream ends")
	}
}
