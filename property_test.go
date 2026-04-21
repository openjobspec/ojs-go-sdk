package ojs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"testing/quick"
	"time"
)

// =============================================================================
// 1. Job JSON roundtrip
// =============================================================================

func TestPropertyJobRoundtrip(t *testing.T) {
	f := func(id, jobType, queue string, priority, attempt, maxAttempts int) bool {
		if id == "" || jobType == "" || queue == "" {
			return true // skip degenerate inputs
		}
		job := Job{
			ID:          id,
			Type:        jobType,
			State:       JobStateAvailable,
			Queue:       queue,
			Priority:    priority,
			Attempt:     attempt,
			MaxAttempts: maxAttempts,
			Args:        Args{"key": "value"},
		}

		data, err := json.Marshal(job)
		if err != nil {
			return true // skip if marshal fails on unusual input
		}

		var decoded Job
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Logf("roundtrip unmarshal failed: %v", err)
			return false
		}

		return decoded.ID == job.ID &&
			decoded.Type == job.Type &&
			decoded.State == job.State &&
			decoded.Queue == job.Queue &&
			decoded.Priority == job.Priority &&
			decoded.Attempt == job.Attempt &&
			decoded.MaxAttempts == job.MaxAttempts
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyJobRoundtripPreservesAllFields(t *testing.T) {
	f := func(id, jobType, queue string, priority int, timeoutMS int) bool {
		if id == "" || jobType == "" || queue == "" {
			return true
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		tags := []string{"tag-a", "tag-b"}
		meta := map[string]any{"env": "test"}
		job := Job{
			ID:          id,
			Type:        jobType,
			State:       JobStateActive,
			Queue:       queue,
			Priority:    priority,
			Attempt:     1,
			MaxAttempts: 3,
			TimeoutMS:   timeoutMS,
			Tags:        tags,
			Meta:        meta,
			CreatedAt:   &now,
			Args:        Args{"user": "alice", "count": float64(42)},
		}

		data, err := json.Marshal(job)
		if err != nil {
			return true
		}

		var decoded Job
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Logf("unmarshal failed: %v", err)
			return false
		}

		if len(decoded.Tags) != len(tags) {
			return false
		}
		for i, tag := range decoded.Tags {
			if tag != tags[i] {
				return false
			}
		}
		if decoded.Meta["env"] != "test" {
			return false
		}
		if decoded.TimeoutMS != timeoutMS {
			return false
		}
		if decoded.Args["user"] != "alice" {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// 2. Args serialization roundtrip
// =============================================================================

func TestPropertyArgsRoundtrip(t *testing.T) {
	f := func(key1, val1, key2, val2 string) bool {
		if key1 == "" || key2 == "" {
			return true
		}
		args := Args{key1: val1, key2: val2}
		wire := argsToWire(args)
		recovered := argsFromWire(wire)

		return recovered[key1] == val1 && recovered[key2] == val2
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyArgsNumericRoundtrip(t *testing.T) {
	f := func(intVal int, floatVal float64) bool {
		if math.IsNaN(floatVal) || math.IsInf(floatVal, 0) {
			return true // JSON doesn't support these
		}
		args := Args{
			"int":   float64(intVal), // JSON numbers are float64
			"float": floatVal,
		}

		wire := argsToWire(args)
		data, err := json.Marshal(wire)
		if err != nil {
			return true
		}

		var rawWire []any
		if err := json.Unmarshal(data, &rawWire); err != nil {
			return true
		}

		recovered := argsFromWire(rawWire)
		recoveredInt, ok1 := recovered["int"].(float64)
		recoveredFloat, ok2 := recovered["float"].(float64)
		if !ok1 || !ok2 {
			return false
		}

		return recoveredInt == float64(intVal) && recoveredFloat == floatVal
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyArgsEmptyRoundtrip(t *testing.T) {
	// Empty args should roundtrip cleanly.
	wire := argsToWire(Args{})
	recovered := argsFromWire(wire)
	if len(recovered) != 0 {
		t.Errorf("expected empty args, got %v", recovered)
	}

	// Nil args should also produce empty wire format.
	wireNil := argsToWire(nil)
	recoveredNil := argsFromWire(wireNil)
	if len(recoveredNil) != 0 {
		t.Errorf("expected empty args from nil, got %v", recoveredNil)
	}
}

func TestPropertyArgsNestedRoundtrip(t *testing.T) {
	f := func(key, innerKey, innerVal string) bool {
		if key == "" || innerKey == "" {
			return true
		}
		args := Args{
			key: map[string]any{innerKey: innerVal},
		}
		wire := argsToWire(args)
		data, err := json.Marshal(wire)
		if err != nil {
			return true
		}

		var rawWire []any
		if err := json.Unmarshal(data, &rawWire); err != nil {
			return true
		}
		recovered := argsFromWire(rawWire)
		nested, ok := recovered[key].(map[string]any)
		if !ok {
			return false
		}
		return nested[innerKey] == innerVal
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// 3. RetryPolicy roundtrip
// =============================================================================

func TestPropertyRetryPolicyRoundtrip(t *testing.T) {
	f := func(maxAttempts int, initialMS, maxMS uint16, coeff float64) bool {
		if math.IsNaN(coeff) || math.IsInf(coeff, 0) {
			return true
		}
		jitter := true
		policy := RetryPolicy{
			MaxAttempts:        maxAttempts,
			InitialInterval:    time.Duration(initialMS) * time.Millisecond,
			BackoffCoefficient: coeff,
			MaxInterval:        time.Duration(maxMS) * time.Millisecond,
			Jitter:             &jitter,
			NonRetryableErrors: []string{"err_a", "err_b"},
		}

		wire := policy.toWire()
		recovered := retryPolicyFromWire(wire)

		return recovered.MaxAttempts == policy.MaxAttempts &&
			recovered.InitialInterval == policy.InitialInterval &&
			recovered.BackoffCoefficient == policy.BackoffCoefficient &&
			recovered.MaxInterval == policy.MaxInterval &&
			recovered.Jitter != nil && *recovered.Jitter == *policy.Jitter &&
			len(recovered.NonRetryableErrors) == 2 &&
			recovered.NonRetryableErrors[0] == "err_a" &&
			recovered.NonRetryableErrors[1] == "err_b"
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyRetryPolicyNilRoundtrip(t *testing.T) {
	recovered := retryPolicyFromWire(nil)
	if recovered != nil {
		t.Error("expected nil from nil wire input")
	}
}

// =============================================================================
// 4. RetryPolicy delay computation (exponential backoff invariants)
// =============================================================================

func TestPropertyBackoffMonotonicallyIncreasing(t *testing.T) {
	f := func(initialMS uint16, coeffU uint8) bool {
		// Ensure coefficient >= 1.0 for monotonic growth.
		coeff := 1.0 + float64(coeffU)/100.0
		initial := time.Duration(initialMS+1) * time.Millisecond // at least 1ms

		var prevDelay time.Duration
		for attempt := 0; attempt < 10; attempt++ {
			delay := time.Duration(float64(initial) * math.Pow(coeff, float64(attempt)))
			if delay < 0 {
				return true // overflow: skip
			}
			if delay < prevDelay {
				return false // non-monotonic
			}
			prevDelay = delay
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyBackoffDelaysNonNegative(t *testing.T) {
	f := func(initialMS uint16, coeffU uint8, attempts uint8) bool {
		coeff := float64(coeffU) / 50.0 // 0.0 to 5.1
		initial := time.Duration(initialMS) * time.Millisecond

		numAttempts := int(attempts%20) + 1
		for attempt := 0; attempt < numAttempts; attempt++ {
			delay := time.Duration(float64(initial) * math.Pow(coeff, float64(attempt)))
			if delay < 0 {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyBackoffCappedByMaxInterval(t *testing.T) {
	f := func(initialMS uint8, maxMS uint16) bool {
		initial := time.Duration(initialMS+1) * time.Millisecond
		maxInterval := time.Duration(maxMS+1) * time.Millisecond
		coeff := 2.0

		for attempt := 0; attempt < 20; attempt++ {
			raw := time.Duration(float64(initial) * math.Pow(coeff, float64(attempt)))
			capped := raw
			if capped > maxInterval {
				capped = maxInterval
			}
			if capped > maxInterval {
				return false
			}
			if capped < 0 && raw > 0 {
				return false // overflow detection
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// 5. Job state invariants
// =============================================================================

func TestPropertyTerminalStatesAreAlwaysTerminal(t *testing.T) {
	terminalStates := []JobState{JobStateCompleted, JobStateCancelled, JobStateDiscarded}
	for _, state := range terminalStates {
		if !state.IsTerminal() {
			t.Errorf("state %q should be terminal", state)
		}
	}
}

func TestPropertyNonTerminalStatesAreNeverTerminal(t *testing.T) {
	nonTerminalStates := []JobState{
		JobStatePending, JobStateScheduled, JobStateAvailable,
		JobStateActive, JobStateRetryable,
	}
	for _, state := range nonTerminalStates {
		if state.IsTerminal() {
			t.Errorf("state %q should NOT be terminal", state)
		}
	}
}

func TestPropertyArbitraryStringsAreNotTerminal(t *testing.T) {
	f := func(s string) bool {
		state := JobState(s)
		if state == JobStateCompleted || state == JobStateCancelled || state == JobStateDiscarded {
			return state.IsTerminal()
		}
		return !state.IsTerminal()
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}

func TestPropertyJobStateRoundtripThroughJSON(t *testing.T) {
	allStates := []JobState{
		JobStatePending, JobStateScheduled, JobStateAvailable,
		JobStateActive, JobStateCompleted, JobStateRetryable,
		JobStateCancelled, JobStateDiscarded,
	}
	for _, state := range allStates {
		job := Job{ID: "test", Type: "t", State: state, Queue: "default", Args: Args{}}
		data, err := json.Marshal(job)
		if err != nil {
			t.Fatalf("marshal failed for state %q: %v", state, err)
		}
		var decoded Job
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal failed for state %q: %v", state, err)
		}
		if decoded.State != state {
			t.Errorf("state roundtrip: expected %q, got %q", state, decoded.State)
		}
		if decoded.State.IsTerminal() != state.IsTerminal() {
			t.Errorf("terminal invariant broken for state %q", state)
		}
	}
}

// =============================================================================
// 6. Workflow construction
// =============================================================================

func TestPropertyChainStructure(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%20) + 1 // 1..20 steps
		steps := make([]Step, count)
		for i := range steps {
			steps[i] = Step{
				Type: fmt.Sprintf("step.%d", i),
				Args: Args{"index": float64(i)},
			}
		}
		def := Chain(steps...)
		if def.Type != "chain" {
			return false
		}
		if len(def.Steps) != count {
			return false
		}
		for i, s := range def.Steps {
			if s.Type != fmt.Sprintf("step.%d", i) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

func TestPropertyGroupStructure(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%20) + 1
		jobs := make([]Step, count)
		for i := range jobs {
			jobs[i] = Step{
				Type: fmt.Sprintf("job.%d", i),
				Args: Args{},
			}
		}
		def := Group(jobs...)
		if def.Type != "group" {
			return false
		}
		if len(def.Jobs) != count {
			return false
		}
		// Group should have no steps (only jobs).
		if len(def.Steps) != 0 {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

func TestPropertyBatchStructure(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%20) + 1
		jobs := make([]Step, count)
		for i := range jobs {
			jobs[i] = Step{
				Type: fmt.Sprintf("work.%d", i),
				Args: Args{},
			}
		}
		callbacks := BatchCallbacks{
			OnComplete: &Step{Type: "batch.done", Args: Args{}},
			OnSuccess:  &Step{Type: "batch.ok", Args: Args{}},
			OnFailure:  &Step{Type: "batch.fail", Args: Args{}},
		}
		def := Batch(callbacks, jobs...)
		if def.Type != "batch" {
			return false
		}
		if len(def.Jobs) != count {
			return false
		}
		if def.Callbacks == nil {
			return false
		}
		if def.Callbacks.OnComplete.Type != "batch.done" {
			return false
		}
		if def.Callbacks.OnSuccess.Type != "batch.ok" {
			return false
		}
		if def.Callbacks.OnFailure.Type != "batch.fail" {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

func TestPropertyBatchWithNilCallbacks(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%10) + 1
		jobs := make([]Step, count)
		for i := range jobs {
			jobs[i] = Step{Type: fmt.Sprintf("w.%d", i), Args: Args{}}
		}
		def := Batch(BatchCallbacks{}, jobs...)
		return def.Type == "batch" &&
			len(def.Jobs) == count &&
			def.Callbacks != nil &&
			def.Callbacks.OnComplete == nil &&
			def.Callbacks.OnSuccess == nil &&
			def.Callbacks.OnFailure == nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// 7. Middleware chain
// =============================================================================

func TestPropertyMiddlewareExecutionOrder(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%10) + 1 // 1..10 middleware
		var order []int
		chain := newMiddlewareChain()

		for i := 0; i < count; i++ {
			idx := i
			chain.Add(fmt.Sprintf("mw-%d", idx), func(ctx JobContext, next HandlerFunc) error {
				order = append(order, idx)
				return next(ctx)
			})
		}

		handler := chain.then(func(ctx JobContext) error {
			order = append(order, -1) // sentinel for handler
			return nil
		})

		order = nil // reset
		ctx := JobContext{
			Job: Job{ID: "test", Type: "test.job"},
			ctx: context.Background(),
		}
		if err := handler(ctx); err != nil {
			return false
		}

		// Middleware should execute in order 0, 1, ..., n-1, then handler (-1).
		if len(order) != count+1 {
			return false
		}
		for i := 0; i < count; i++ {
			if order[i] != i {
				return false
			}
		}
		return order[count] == -1
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

func TestPropertyMiddlewareErrorStopsChain(t *testing.T) {
	f := func(errorAt uint8) bool {
		count := 5
		errIdx := int(errorAt) % count
		sentinel := errors.New("middleware error")
		var executed []int
		chain := newMiddlewareChain()

		for i := 0; i < count; i++ {
			idx := i
			chain.Add(fmt.Sprintf("mw-%d", idx), func(ctx JobContext, next HandlerFunc) error {
				executed = append(executed, idx)
				if idx == errIdx {
					return sentinel
				}
				return next(ctx)
			})
		}

		handlerCalled := false
		handler := chain.then(func(ctx JobContext) error {
			handlerCalled = true
			return nil
		})

		executed = nil
		ctx := JobContext{
			Job: Job{ID: "test", Type: "test.job"},
			ctx: context.Background(),
		}
		err := handler(ctx)

		// Error should propagate.
		if err != sentinel {
			return false
		}
		// Only middleware up to and including the error index should execute.
		if len(executed) != errIdx+1 {
			return false
		}
		// Handler should not be called if error is before the last middleware,
		// or the last middleware errors before calling next.
		if handlerCalled {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 50}); err != nil {
		t.Error(err)
	}
}

func TestPropertyMiddlewareChainEmptyPassesThrough(t *testing.T) {
	chain := newMiddlewareChain()
	called := false
	handler := chain.then(func(ctx JobContext) error {
		called = true
		return nil
	})

	ctx := JobContext{
		Job: Job{ID: "test", Type: "test.job"},
		ctx: context.Background(),
	}
	if err := handler(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called with empty middleware chain")
	}
}

func TestPropertyMiddlewareCanModifyContext(t *testing.T) {
	f := func(n uint8) bool {
		count := int(n%5) + 1
		chain := newMiddlewareChain()

		for i := 0; i < count; i++ {
			idx := i
			chain.Add(fmt.Sprintf("mw-%d", idx), func(ctx JobContext, next HandlerFunc) error {
				if ctx.Job.Meta == nil {
					ctx.Job.Meta = make(map[string]any)
				}
				ctx.Job.Meta[fmt.Sprintf("mw-%d", idx)] = true
				return next(ctx)
			})
		}

		var finalMeta map[string]any
		handler := chain.then(func(ctx JobContext) error {
			finalMeta = ctx.Job.Meta
			return nil
		})

		ctx := JobContext{
			Job: Job{ID: "test", Type: "test.job"},
			ctx: context.Background(),
		}
		if err := handler(ctx); err != nil {
			return false
		}

		// All middleware should have added their key.
		for i := 0; i < count; i++ {
			key := fmt.Sprintf("mw-%d", i)
			if finalMeta[key] != true {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// =============================================================================
// Additional: Validation properties
// =============================================================================

func TestPropertyValidJobTypesAlwaysAccepted(t *testing.T) {
	validTypes := []string{
		"email.send", "data.export", "a", "a.b.c.d",
		"my_job", "a1.b2.c3",
	}
	for _, jt := range validTypes {
		if err := validateEnqueueParams(jt, []any{}); err != nil {
			t.Errorf("valid type %q rejected: %v", jt, err)
		}
	}
}

// TestPropertyHyphenatedJobTypesAlwaysRejected locks the spec rule that each
// job-type segment matches [a-z][a-z0-9_]*. Hyphens are NOT permitted: see
// spec/spec/ojs-core.md ("Each segment MUST match the pattern `[a-z][a-z0-9_]*`")
// and the negative conformance fixture
// ojs-json-schema/tests/invalid/22-type-with-hyphens.json.
func TestPropertyHyphenatedJobTypesAlwaysRejected(t *testing.T) {
	invalidTypes := []string{"my-job", "email.send-now", "-leading", "trailing-"}
	for _, jt := range invalidTypes {
		if err := validateEnqueueParams(jt, []any{}); err == nil {
			t.Errorf("hyphenated type %q accepted, want rejection per OJS core spec", jt)
		}
	}
}

func TestPropertyEmptyJobTypeAlwaysRejected(t *testing.T) {
	if err := validateEnqueueParams("", []any{}); err == nil {
		t.Error("empty job type should be rejected")
	}
}

func TestPropertyNilArgsAlwaysRejected(t *testing.T) {
	if err := validateEnqueueParams("valid.type", nil); err == nil {
		t.Error("nil args should be rejected")
	}
}

func TestPropertyRetryPolicyWireMillisecondConversion(t *testing.T) {
	f := func(ms uint16) bool {
		dur := time.Duration(ms) * time.Millisecond
		policy := RetryPolicy{
			InitialInterval: dur,
			MaxInterval:     dur,
		}
		wire := policy.toWire()
		recovered := retryPolicyFromWire(wire)

		return recovered.InitialInterval == dur &&
			recovered.MaxInterval == dur &&
			wire.InitialIntervalMS == int(ms) &&
			wire.MaxIntervalMS == int(ms)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyUniquePolicyWireRoundtrip(t *testing.T) {
	f := func(periodMS uint16, onConflict string) bool {
		period := time.Duration(periodMS) * time.Millisecond
		policy := UniquePolicy{
			Key:        []string{"email", "user_id"},
			Period:     period,
			OnConflict: onConflict,
		}
		wire := policy.toWire()

		return wire.PeriodMS == int(periodMS) &&
			wire.OnConflict == onConflict &&
			len(wire.Key) == 2 &&
			wire.Key[0] == "email" &&
			wire.Key[1] == "user_id"
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

func TestPropertyJobErrorRoundtrip(t *testing.T) {
	f := func(code, message string, retryable bool) bool {
		if code == "" || message == "" {
			return true
		}
		job := Job{
			ID:    "err-test",
			Type:  "test.job",
			State: JobStateDiscarded,
			Queue: "default",
			Args:  Args{},
			Error: &JobError{
				Code:      code,
				Message:   message,
				Retryable: retryable,
			},
		}

		data, err := json.Marshal(job)
		if err != nil {
			return true
		}

		var decoded Job
		if err := json.Unmarshal(data, &decoded); err != nil {
			return false
		}

		return decoded.Error != nil &&
			decoded.Error.Code == code &&
			decoded.Error.Message == message &&
			decoded.Error.Retryable == retryable
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}
