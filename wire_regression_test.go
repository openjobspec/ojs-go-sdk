package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- job.go: args wire round-trip ---

// TestJobPositionalArgsRoundTrip is a regression test: decoding positional args
// builds a synthetic index map, and re-encoding through it rewrote [1,2,3] as
// [{"0":1,"1":2,"2":3}], corrupting the job on the wire.
func TestJobPositionalArgsRoundTrip(t *testing.T) {
	const in = `{"id":"j1","type":"a.job","args":[1,"two",true]}`

	var job Job
	if err := json.Unmarshal([]byte(in), &job); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(job.RawArgs) != 3 {
		t.Fatalf("RawArgs = %v, want 3 elements", job.RawArgs)
	}

	out, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got struct {
		Args []any `json:"args"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if len(got.Args) != 3 {
		t.Fatalf("round-tripped args = %v, want the original 3-element array", got.Args)
	}
	if got.Args[1] != "two" || got.Args[2] != true {
		t.Errorf("round-tripped args = %v, want [1 two true]", got.Args)
	}
}

// TestJobObjectArgsRoundTrip characterizes the normal object form, where Args
// stays authoritative so caller mutations are preserved.
func TestJobObjectArgsRoundTrip(t *testing.T) {
	const in = `{"id":"j1","type":"a.job","args":[{"to":"a@example.com"}]}`

	var job Job
	if err := json.Unmarshal([]byte(in), &job); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if job.Args["to"] != "a@example.com" {
		t.Fatalf("Args = %v", job.Args)
	}

	job.Args["to"] = "b@example.com"
	out, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), "b@example.com") {
		t.Errorf("marshalled job = %s, want the mutated Args value", out)
	}
}

func TestJobEmptyArgsMarshalsAsEmptyArray(t *testing.T) {
	out, err := json.Marshal(Job{ID: "j1", Type: "a.job"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"args":[]`) {
		t.Errorf("marshalled job = %s, want args:[]", out)
	}
}

// TestJobMalformedArgsReturnsError is a regression test: a non-array args value
// used to be silently swallowed, leaving Args nil with no error.
func TestJobMalformedArgsReturnsError(t *testing.T) {
	var job Job
	err := json.Unmarshal([]byte(`{"id":"j1","type":"a.job","args":{"to":"x"}}`), &job)
	if err == nil {
		t.Fatal("malformed args must be reported, not silently dropped")
	}
	if !strings.Contains(err.Error(), "malformed args") {
		t.Errorf("error = %v, want it to mention malformed args", err)
	}
}

// --- transport.go: Retry-After parsing ---

func TestParseRetryAfterDelaySeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "2.5")
	if got := parseRetryAfter(h); got != 2500*time.Millisecond {
		t.Errorf("parseRetryAfter = %v, want 2.5s", got)
	}
}

// TestParseRetryAfterHTTPDate covers the second form allowed by RFC 9110
// §10.2.3, which previously parsed as zero (retrying immediately).
func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("Retry-After", now.Add(90*time.Second).Format(http.TimeFormat))

	got := parseRetryAfterAt(h, now)
	if got < 89*time.Second || got > 91*time.Second {
		t.Errorf("parseRetryAfterAt = %v, want ~90s", got)
	}
}

func TestParseRetryAfterEdgeCases(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct{ name, value string }{
		{"absent", ""},
		{"garbage", "soon"},
		{"negative seconds", "-5"},
		{"past date", now.Add(-time.Hour).Format(http.TimeFormat)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			if got := parseRetryAfterAt(h, now); got != 0 {
				t.Errorf("parseRetryAfterAt(%q) = %v, want 0", tc.value, got)
			}
		})
	}
}

func TestParseRetryAfterValidity(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value string
		valid bool
	}{
		{"zero delay", "0", true},
		{"future date", now.Add(time.Minute).Format(http.TimeFormat), true},
		{"past date", now.Add(-time.Minute).Format(http.TimeFormat), true},
		{"absent", "", false},
		{"garbage", "soon", false},
		{"negative", "-1", false},
		{"nan", "NaN", false},
		{"infinity", "Inf", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			_, valid := parseRetryAfterAtValue(h, now)
			if valid != tc.valid {
				t.Errorf("parseRetryAfterAtValue(%q) valid = %v, want %v", tc.value, valid, tc.valid)
			}
		})
	}
}

// --- subscribe.go: SSE framing ---

func TestSSEFieldSplitting(t *testing.T) {
	cases := []struct{ line, field, value string }{
		{"data: hello", "data", "hello"},
		{"data:hello", "data", "hello"},    // the space after ':' is optional
		{"data:  hello", "data", " hello"}, // only ONE leading space is stripped
		{"data", "data", ""},
		{"event: job.completed", "event", "job.completed"},
		{"id: 42", "id", "42"},
	}
	for _, tc := range cases {
		f, v := splitSSEField(tc.line)
		if f != tc.field || v != tc.value {
			t.Errorf("splitSSEField(%q) = (%q, %q), want (%q, %q)", tc.line, f, v, tc.field, tc.value)
		}
	}
}

// TestSSEDataWithoutSpaceIsDelivered is a regression test: the reader only
// matched the "data: " form, so spec-legal "data:x" lines were dropped.
func TestSSEDataWithoutSpaceIsDelivered(t *testing.T) {
	var got []Event
	d := &sseDispatcher{handler: func(e Event) { got = append(got, e) }}
	for _, line := range []string{"event:job.completed", "data:{\"id\":\"j1\"}", ""} {
		d.line(line)
	}
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].Type != "job.completed" {
		t.Errorf("event type = %q, want job.completed", got[0].Type)
	}
	if raw, _ := got[0].Data["raw"].(string); raw != `{"id":"j1"}` {
		t.Errorf("event raw = %q", raw)
	}
}

func TestSSEDispatcherTracksLastEventID(t *testing.T) {
	d := &sseDispatcher{handler: func(Event) {}}
	for _, line := range []string{"id: 7", "data: a", "", "data: b", ""} {
		d.line(line)
	}
	if d.lastEventID != "7" {
		t.Errorf("lastEventID = %q, want 7", d.lastEventID)
	}
}

func TestSSECommentAndBlankLinesProduceNoEvents(t *testing.T) {
	n := 0
	d := &sseDispatcher{handler: func(Event) { n++ }}
	for _, line := range []string{":ping", "", ":keep-alive", ""} {
		d.line(line)
	}
	if n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
}

// TestEventStreamURLEscapesChannel is a regression test: channel names contain
// ':' and may embed user input, which corrupted the query string.
func TestEventStreamURLEscapesChannel(t *testing.T) {
	c, err := NewClient("http://localhost:8080")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got := c.eventStreamURL("queue:my queue&admin=1")
	want := "http://localhost:8080/ojs/v1/events/stream?channel=queue%3Amy+queue%26admin%3D1"
	if got != want {
		t.Errorf("eventStreamURL = %q, want %q", got, want)
	}
}

// --- errors.go / enqueue config ---

func TestEnqueueConfigHasOverrides(t *testing.T) {
	if resolveEnqueueConfig(nil).hasOverrides() {
		t.Error("default config must report no overrides")
	}
	cases := map[string]EnqueueOption{
		"queue":      WithQueue("other"),
		"priority":   WithPriority(1),
		"timeout":    WithTimeout(time.Second),
		"delay":      WithDelay(time.Second),
		"expires":    WithExpiresAt(time.Now()),
		"retry":      WithRetry(RetryPolicy{}),
		"unique":     WithUnique(UniquePolicy{}),
		"tags":       WithTags("t"),
		"visibility": WithVisibilityTimeout(time.Second),
	}
	for name, opt := range cases {
		if !resolveEnqueueConfig([]EnqueueOption{opt}).hasOverrides() {
			t.Errorf("%s option must be detected as an override", name)
		}
	}
}

func TestWireOptionsPriorityPresenceGoldenJSON(t *testing.T) {
	tests := []struct {
		name string
		opts []EnqueueOption
		want string
	}{
		{"unset", nil, `{}`},
		{"nonzero", []EnqueueOption{WithPriority(7)}, `{"priority":7}`},
		{"explicit zero", []EnqueueOption{WithPriority(0)}, `{"priority":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(buildWireOptions(resolveEnqueueConfig(tt.opts)))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("wire options JSON = %s, want %s", got, tt.want)
			}
		})
	}
}

// --- jobcontext.go ---

func TestJobContextWithContext(t *testing.T) {
	type ctxKey struct{}
	base := NewJobContextForTest(Job{ID: "j1", Type: "a.job"})
	derived := base.WithContext(context.WithValue(context.Background(), ctxKey{}, "v"))

	if derived.Context().Value(ctxKey{}) != "v" {
		t.Error("WithContext must carry the supplied context")
	}
	if base.Context().Value(ctxKey{}) != nil {
		t.Error("WithContext must not mutate the receiver")
	}
	if derived.Job.ID != "j1" {
		t.Error("WithContext must preserve the job")
	}
}

func TestJobContextWithContextSharesResultRef(t *testing.T) {
	ref := &jobResultRef{}
	jc := JobContext{Job: Job{ID: "j1"}, ctx: context.Background(), resultRef: ref}
	jc.WithContext(context.Background()).SetResult(map[string]any{"ok": true})
	if ref.data["ok"] != true {
		t.Error("SetResult through a derived JobContext must reach the shared result ref")
	}
}

func TestJobContextZeroValueContextIsUsable(t *testing.T) {
	var jc JobContext
	if jc.Context() == nil {
		t.Error("Context() must never return nil")
	}
}

func TestJobContextWithNilContextPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("WithContext(nil) must panic")
		}
	}()
	// Held in a variable rather than written as a literal nil so the call is
	// what a real caller would hit, not a statically diagnosable mistake.
	var nilCtx context.Context
	NewJobContextForTest(Job{}).WithContext(nilCtx)
}
