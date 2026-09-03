package serverless

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type pushAuthHarness struct {
	register func(string, HandlerFunc)
	serve    func(http.ResponseWriter, *http.Request)
}

type pushAuthHarnessFactory func(pushAuthHarnessOptions) pushAuthHarness

type pushAuthHarnessOptions struct {
	secrets     []string
	window      time.Duration
	maxBodySize int64
}

func newInsecureLambdaHandler(opts ...Option) *LambdaHandler {
	opts = append(opts, WithInsecureAllowUnsignedPushForLocalDevelopment())
	return NewLambdaHandler(opts...)
}

func newInsecureCloudflareHandler(opts ...CloudflareOption) *CloudflareHandler {
	opts = append(opts, WithCloudflareInsecureAllowUnsignedPushForLocalDevelopment())
	return NewCloudflareHandler(opts...)
}

func newInsecureVercelHandler(opts ...VercelOption) *VercelHandler {
	opts = append(opts, WithVercelInsecureAllowUnsignedPushForLocalDevelopment())
	return NewVercelHandler(opts...)
}

func pushAuthAdapters() map[string]pushAuthHarnessFactory {
	return map[string]pushAuthHarnessFactory{
		"lambda": func(options pushAuthHarnessOptions) pushAuthHarness {
			opts := []Option{WithPushSigningSecrets(options.secrets...)}
			if options.window != 0 {
				opts = append(opts, WithPushFreshnessWindow(options.window))
			}
			if options.maxBodySize != 0 {
				opts = append(opts, WithMaxBodySize(options.maxBodySize))
			}
			h := NewLambdaHandler(opts...)
			return pushAuthHarness{register: h.Register, serve: h.HandleHTTP()}
		},
		"cloudflare": func(options pushAuthHarnessOptions) pushAuthHarness {
			opts := []CloudflareOption{WithCloudflarePushSigningSecrets(options.secrets...)}
			if options.window != 0 {
				opts = append(opts, WithCloudflarePushFreshnessWindow(options.window))
			}
			if options.maxBodySize != 0 {
				opts = append(opts, WithCloudflareMaxBodySize(options.maxBodySize))
			}
			h := NewCloudflareHandler(opts...)
			return pushAuthHarness{register: h.Register, serve: h.ServeHTTP}
		},
		"vercel": func(options pushAuthHarnessOptions) pushAuthHarness {
			opts := []VercelOption{WithVercelPushSigningSecrets(options.secrets...)}
			if options.window != 0 {
				opts = append(opts, WithVercelPushFreshnessWindow(options.window))
			}
			if options.maxBodySize != 0 {
				opts = append(opts, WithVercelMaxBodySize(options.maxBodySize))
			}
			h := NewVercelHandler(opts...)
			return pushAuthHarness{register: h.Register, serve: h.ServeHTTP}
		},
	}
}

func signPush(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func serveSignedPush(
	t *testing.T,
	h pushAuthHarness,
	body []byte,
	timestamp string,
	signatures ...string,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	called := false
	h.register("email.send", func(context.Context, JobEvent) error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	if timestamp != "" {
		req.Header.Set(PushTimestampHeader, timestamp)
	}
	if len(signatures) > 0 {
		req.Header.Set(PushSignatureHeader, strings.Join(signatures, ", "))
	}
	w := httptest.NewRecorder()
	h.serve(w, req)
	return w, called
}

func testPushAuthAdapters(t *testing.T, test func(*testing.T, pushAuthHarnessFactory)) {
	t.Helper()
	for adapterName, newHarness := range pushAuthAdapters() {
		t.Run(adapterName, func(t *testing.T) {
			test(t, newHarness)
		})
	}
}

const (
	pushAuthCurrentSecret  = "current-secret"
	pushAuthPreviousSecret = "previous-secret"
)

var pushAuthTestBody = []byte(`{"job":{"id":"job-1","type":"email.send","queue":"default","args":[],"attempt":1},"worker_id":"w1","delivery_id":"d1"}`)

func TestPushAuthenticationValidAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{secrets: []string{pushAuthCurrentSecret}}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusOK || !called {
			t.Fatalf("status = %d, handler called = %v; want 200, true", w.Code, called)
		}
	})
}

func TestPushAuthenticationRejectsTamperingAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		tampered := []byte(strings.Replace(string(pushAuthTestBody), "job-1", "job-2", 1))
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{secrets: []string{pushAuthCurrentSecret}}),
			tampered,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("status = %d, handler called = %v; want 401, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationRejectsReplayAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Add(-DefaultPushFreshnessWindow-time.Second).Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{secrets: []string{pushAuthCurrentSecret}}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("status = %d, handler called = %v; want 401, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationRejectsFutureTimestampAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Add(DefaultPushFreshnessWindow+time.Second).Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{secrets: []string{pushAuthCurrentSecret}}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("status = %d, handler called = %v; want 401, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationSupportsSecretRotationAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{
				secrets: []string{pushAuthCurrentSecret, pushAuthPreviousSecret},
			}),
			pushAuthTestBody,
			timestamp,
			signPush("unrelated-secret", timestamp, pushAuthTestBody),
			signPush(pushAuthPreviousSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusOK || !called {
			t.Fatalf("status = %d, handler called = %v; want 200, true", w.Code, called)
		}
	})
}

func TestPushAuthenticationFailsClosedWithoutSecretAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusServiceUnavailable || called {
			t.Fatalf("status = %d, handler called = %v; want 503, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationEnforcesBodyLimitAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{
				secrets:     []string{pushAuthCurrentSecret},
				maxBodySize: 16,
			}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusRequestEntityTooLarge || called {
			t.Fatalf("status = %d, handler called = %v; want 413, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationPrecedesDecodingAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		invalidJSON := []byte(`{`)
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{secrets: []string{pushAuthCurrentSecret}}),
			invalidJSON,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("status = %d, handler called = %v; want 401, false", w.Code, called)
		}
	})
}

func TestPushAuthenticationCustomFreshnessAcrossAdapters(t *testing.T) {
	testPushAuthAdapters(t, func(t *testing.T, newHarness pushAuthHarnessFactory) {
		timestamp := strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10)
		w, called := serveSignedPush(
			t,
			newHarness(pushAuthHarnessOptions{
				secrets: []string{pushAuthCurrentSecret},
				window:  10 * time.Minute,
			}),
			pushAuthTestBody,
			timestamp,
			signPush(pushAuthCurrentSecret, timestamp, pushAuthTestBody),
		)
		if w.Code != http.StatusOK || !called {
			t.Fatalf("status = %d, handler called = %v; want 200, true", w.Code, called)
		}
	})
}

func TestPushAuthenticationHeaderLimit(t *testing.T) {
	body := []byte(`{"job":{"id":"job-1","type":"email.send","args":[]}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	h := pushAuthAdapters()["lambda"](pushAuthHarnessOptions{secrets: []string{"secret"}})

	w, called := serveSignedPush(
		t,
		h,
		body,
		timestamp,
		strings.Repeat("x", maxPushSignatureHeaderBytes+1),
	)
	if w.Code != http.StatusRequestHeaderFieldsTooLarge || called {
		t.Fatalf("status = %d, handler called = %v; want 431, false", w.Code, called)
	}
}

func TestHandlerOptionsCopiesPushSigningSecrets(t *testing.T) {
	secrets := []string{"secret-one"}
	h := NewLambdaHandler(WithHandlerOptions(HandlerOptions{
		PushSigningSecrets:  secrets,
		PushFreshnessWindow: time.Minute,
	}))
	secrets[0] = "mutated"

	if got := string(h.pushSigningSecrets[0]); got != "secret-one" {
		t.Fatalf("stored secret = %q, want copied value", got)
	}
	if h.pushFreshnessWindow != time.Minute {
		t.Fatalf("freshness window = %v, want 1m", h.pushFreshnessWindow)
	}
}
