package ojs

import (
	"encoding/json"
	"testing"
)

// --- Args wire conversion benchmarks ---

func BenchmarkArgsToWire_Small(b *testing.B) {
	args := Args{"to": "user@example.com"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		argsToWire(args)
	}
}

func BenchmarkArgsToWire_Large(b *testing.B) {
	args := Args{
		"to":      "user@example.com",
		"subject": "Welcome to the platform",
		"body":    "Hello, this is a longer message body for benchmarking purposes.",
		"headers": map[string]any{"X-Priority": "high", "X-Campaign": "onboarding"},
		"tags":    []any{"email", "onboarding", "welcome"},
		"retries": 5,
		"delay":   300,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		argsToWire(args)
	}
}

func BenchmarkArgsToWire_Empty(b *testing.B) {
	args := Args{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		argsToWire(args)
	}
}

func BenchmarkArgsFromWire_Map(b *testing.B) {
	wire := []any{map[string]any{"to": "user@example.com", "subject": "hello"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		argsFromWire(wire)
	}
}

func BenchmarkArgsFromWire_Positional(b *testing.B) {
	wire := []any{"user@example.com", "hello", 42}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		argsFromWire(wire)
	}
}

// --- Job JSON serialization benchmarks ---

func BenchmarkJobMarshalJSON_Minimal(b *testing.B) {
	job := Job{
		ID:    "test-123",
		Type:  "email.send",
		State: JobStateAvailable,
		Queue: "default",
		Args:  Args{"to": "user@example.com"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(job)
	}
}

func BenchmarkJobMarshalJSON_Full(b *testing.B) {
	job := Job{
		ID:          "019414d4-8b2e-7c3a-b5d1-f0e2a3b4c5d6",
		Type:        "email.send",
		State:       JobStateActive,
		Queue:       "email",
		Priority:    10,
		Attempt:     2,
		MaxAttempts: 5,
		TimeoutMS:   30000,
		Tags:        []string{"onboarding", "email", "priority"},
		Meta:        map[string]any{"campaign": "welcome", "source": "api"},
		Args: Args{
			"to":      "user@example.com",
			"subject": "Welcome",
			"body":    "Hello and welcome to the platform!",
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(job)
	}
}

func BenchmarkJobUnmarshalJSON(b *testing.B) {
	data := []byte(`{
		"id": "019414d4-8b2e-7c3a-b5d1-f0e2a3b4c5d6",
		"type": "email.send",
		"state": "active",
		"queue": "email",
		"priority": 10,
		"attempt": 2,
		"max_attempts": 5,
		"timeout_ms": 30000,
		"tags": ["onboarding", "email"],
		"meta": {"campaign": "welcome"},
		"args": [{"to": "user@example.com", "subject": "Welcome"}]
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var job Job
		_ = json.Unmarshal(data, &job)
	}
}

func BenchmarkJobMarshalUnmarshalRoundTrip(b *testing.B) {
	job := Job{
		ID:          "test-roundtrip",
		Type:        "data.process",
		State:       JobStateAvailable,
		Queue:       "default",
		Priority:    5,
		Attempt:     1,
		MaxAttempts: 3,
		Args:        Args{"input": "data.csv", "format": "json"},
		Tags:        []string{"etl"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(job)
		var out Job
		_ = json.Unmarshal(data, &out)
	}
}

// --- Middleware chain benchmarks ---

func BenchmarkMiddlewareChain_Direct(b *testing.B) {
	handler := func(ctx JobContext) error { return nil }
	ctx := JobContext{Job: Job{ID: "bench", Type: "test"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

func BenchmarkMiddlewareChain_1(b *testing.B) {
	benchMiddlewareChain(b, 1)
}

func BenchmarkMiddlewareChain_3(b *testing.B) {
	benchMiddlewareChain(b, 3)
}

func BenchmarkMiddlewareChain_5(b *testing.B) {
	benchMiddlewareChain(b, 5)
}

func BenchmarkMiddlewareChain_10(b *testing.B) {
	benchMiddlewareChain(b, 10)
}

func benchMiddlewareChain(b *testing.B, depth int) {
	b.Helper()
	chain := newMiddlewareChain()
	for i := 0; i < depth; i++ {
		chain.Add("", func(ctx JobContext, next HandlerFunc) error {
			return next(ctx)
		})
	}
	handler := chain.then(func(ctx JobContext) error { return nil })
	ctx := JobContext{Job: Job{ID: "bench", Type: "test"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// --- Validation benchmarks ---

func BenchmarkValidateEnqueueParams(b *testing.B) {
	args := []any{map[string]any{"to": "user@example.com"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateEnqueueParams("email.send", args)
	}
}

func BenchmarkValidateQueue(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateQueue("my-queue.v2")
	}
}

// --- Retry/Unique policy wire conversion benchmarks ---

func BenchmarkRetryPolicyToWire(b *testing.B) {
	jitter := true
	policy := RetryPolicy{
		MaxAttempts:        5,
		InitialInterval:    2000000000,
		BackoffCoefficient: 2.0,
		MaxInterval:        300000000000,
		Jitter:             &jitter,
		NonRetryableErrors: []string{"invalid_"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.toWire()
	}
}

func BenchmarkUniquePolicyToWire(b *testing.B) {
	policy := UniquePolicy{
		Key:        []string{"email", "type"},
		Period:     300000000000,
		OnConflict: "reject",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.toWire()
	}
}
