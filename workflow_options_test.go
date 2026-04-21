package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// captureWorkflowRequest runs CreateWorkflow against a stub server and returns
// the wire request it received.
func captureWorkflowRequest(t *testing.T, def WorkflowDefinition, opts ...EnqueueOption) workflowRequest {
	t.Helper()
	var got workflowRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode workflow request: %v", err)
		}
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{"id": "wf-1", "state": "running"},
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.CreateWorkflow(context.Background(), def, opts...); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	return got
}

// TestGroupStepOptionsAreSent is a regression test: only chain steps mapped
// their per-step options, so options on group jobs were silently discarded.
func TestGroupStepOptionsAreSent(t *testing.T) {
	req := captureWorkflowRequest(t, Group(
		Step{Type: "a.job", Args: Args{}, Options: []EnqueueOption{WithQueue("gpu")}},
		Step{Type: "b.job", Args: Args{}, Options: []EnqueueOption{WithPriority(7)}},
	))

	if req.Type != workflowTypeGroup {
		t.Fatalf("type = %q, want %q", req.Type, workflowTypeGroup)
	}
	if len(req.Steps) != 0 {
		t.Errorf("steps = %v, want empty: a group carries its jobs in \"jobs\", not \"steps\"", req.Steps)
	}
	if len(req.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(req.Jobs))
	}
	if req.Jobs[0].Options == nil || req.Jobs[0].Options.Queue != "gpu" {
		t.Errorf("group job 0 options = %+v, want queue=gpu", req.Jobs[0].Options)
	}
	if req.Jobs[1].Options == nil || req.Jobs[1].Options.Priority != 7 {
		t.Errorf("group job 1 options = %+v, want priority=7", req.Jobs[1].Options)
	}
}

// TestBatchJobAndCallbackOptionsAreSent covers the batch primitive and its
// callback steps, which also dropped per-step options. Batch jobs are sent in
// the discriminated "jobs" array and callbacks are nested job envelopes under
// "callbacks.on_complete" -- there is no synthetic depends_on fanning the
// callback into the jobs that precede it; that relationship is expressed by
// the callback's own field name.
func TestBatchJobAndCallbackOptionsAreSent(t *testing.T) {
	req := captureWorkflowRequest(t, Batch(
		BatchCallbacks{
			OnComplete: &Step{Type: "cb.done", Args: Args{}, Options: []EnqueueOption{WithQueue("callbacks")}},
		},
		Step{Type: "a.job", Args: Args{}, Options: []EnqueueOption{WithTags("batch")}},
	))

	if req.Type != workflowTypeBatch {
		t.Fatalf("type = %q, want %q", req.Type, workflowTypeBatch)
	}
	if len(req.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(req.Jobs))
	}
	if req.Jobs[0].Options == nil || len(req.Jobs[0].Options.Tags) != 1 {
		t.Errorf("batch job options = %+v, want tags=[batch]", req.Jobs[0].Options)
	}

	if req.Callbacks == nil || req.Callbacks.OnComplete == nil {
		t.Fatalf("callbacks.on_complete = %+v, want a job envelope", req.Callbacks)
	}
	cb := *req.Callbacks.OnComplete
	if cb.Type != "cb.done" {
		t.Errorf("callback type = %s, want cb.done", cb.Type)
	}
	if cb.Options == nil || cb.Options.Queue != "callbacks" {
		t.Errorf("callback options = %+v, want queue=callbacks", cb.Options)
	}
	if req.Callbacks.OnSuccess != nil || req.Callbacks.OnFailure != nil {
		t.Errorf("callbacks = %+v, want only on_complete populated", req.Callbacks)
	}
}

// TestWorkflowLevelDefaultsAreMaterializedIntoSteps is a regression test:
// workflow-level defaults are now fully materialized into each step's options.
func TestWorkflowLevelDefaultsAreMaterializedIntoSteps(t *testing.T) {
	cases := []struct {
		name  string
		opt   EnqueueOption
		check func(*testing.T, *wireOptions)
	}{
		{"priority", WithPriority(9), func(t *testing.T, o *wireOptions) {
			if o.Priority != 9 {
				t.Errorf("priority = %d, want 9", o.Priority)
			}
		}},
		{"tags", WithTags("urgent"), func(t *testing.T, o *wireOptions) {
			if len(o.Tags) != 1 || o.Tags[0] != "urgent" {
				t.Errorf("tags = %v, want [urgent]", o.Tags)
			}
		}},
		{"retry", WithRetry(RetryPolicy{MaxAttempts: 5}), func(t *testing.T, o *wireOptions) {
			if o.Retry == nil || o.Retry.MaxAttempts != 5 {
				t.Errorf("retry = %+v, want max_attempts=5", o.Retry)
			}
		}},
		{"visibility", WithVisibilityTimeout(30 * time.Second), func(t *testing.T, o *wireOptions) {
			if o.VisibilityTimeoutMS != 30000 {
				t.Errorf("visibility_timeout_ms = %d, want 30000", o.VisibilityTimeoutMS)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := captureWorkflowRequest(t, Chain(Step{Type: "a.job", Args: Args{}}), tc.opt)
			if req.Steps[0].Options == nil {
				t.Fatalf("step options were dropped for %s", tc.name)
			}
			tc.check(t, req.Steps[0].Options)
		})
	}
}

// TestWorkflowOptionsOmittedWhenDefault keeps the payload unchanged for the
// common case where no option was supplied.
func TestWorkflowOptionsOmittedWhenDefault(t *testing.T) {
	req := captureWorkflowRequest(t, Chain(Step{Type: "a.job", Args: Args{}}))
	if req.Steps[0].Options != nil {
		t.Errorf("step options = %+v, want omitted", req.Steps[0].Options)
	}
}

func TestWorkflowPriorityDefaultsAndExplicitZeroOverrides(t *testing.T) {
	tests := []struct {
		name          string
		defPriority   []EnqueueOption
		callPriority  []EnqueueOption
		stepPriority  []EnqueueOption
		wantStep      any
		stepShouldSet bool
	}{
		{name: "unset"},
		{
			name:          "nonzero default materialized into step",
			defPriority:   []EnqueueOption{WithPriority(5)},
			wantStep:      float64(5),
			stepShouldSet: true,
		},
		{
			name:          "call site zero overrides definition default",
			defPriority:   []EnqueueOption{WithPriority(5)},
			callPriority:  []EnqueueOption{WithPriority(0)},
			wantStep:      float64(0),
			stepShouldSet: true,
		},
		{
			name:          "step zero overrides workflow default",
			defPriority:   []EnqueueOption{WithPriority(5)},
			stepPriority:  []EnqueueOption{WithPriority(0)},
			wantStep:      float64(0),
			stepShouldSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Chain(Step{Type: "a.job", Args: Args{}, Options: tt.stepPriority})
			def.Options = tt.defPriority
			req := buildWorkflowRequest(&def, resolveWorkflowDefaults(def, tt.callPriority))
			body := decodeJSONMap(t, req)

			if _, hasRoot := body["options"]; hasRoot {
				t.Error("workflow root must not carry options; defaults are materialized into steps")
			}
			steps := body["steps"].([]any)
			step := steps[0].(map[string]any)
			assertJSONPriorityPresence(t, step["options"], tt.stepShouldSet, tt.wantStep)
		})
	}
}

func assertJSONPriorityPresence(t *testing.T, rawOptions any, wantSet bool, wantPriority any) {
	t.Helper()
	if !wantSet {
		if rawOptions != nil {
			t.Fatalf("options = %v, want omitted", rawOptions)
		}
		return
	}
	options, ok := rawOptions.(map[string]any)
	if !ok {
		t.Fatalf("options = %T(%v), want object", rawOptions, rawOptions)
	}
	priority, present := options["priority"]
	if !present {
		t.Fatal("priority is omitted, want explicit value")
	}
	if priority != wantPriority {
		t.Errorf("priority = %v, want %v", priority, wantPriority)
	}
}

// assertWireOptions fails unless opts is present and has exactly the given
// queue and priority.
func assertWireOptions(t *testing.T, label string, opts *wireOptions, wantQueue string, wantPriority int) {
	t.Helper()
	if opts == nil {
		t.Fatalf("%s options = nil, want queue=%q priority=%d", label, wantQueue, wantPriority)
	}
	if opts.Queue != wantQueue {
		t.Errorf("%s options.queue = %q, want %q", label, opts.Queue, wantQueue)
	}
	if opts.Priority != wantPriority {
		t.Errorf("%s options.priority = %d, want %d", label, opts.Priority, wantPriority)
	}
}

// assertOptionsMetadata asserts the expected metadata key/value pairs.
func assertOptionsMetadata(t *testing.T, label string, opts *wireOptions, want map[string]any) {
	t.Helper()
	if opts == nil {
		t.Fatalf("%s options = nil, want metadata", label)
	}
	if opts.Metadata == nil {
		t.Fatalf("%s options.metadata = nil, want values", label)
	}
	for k, v := range want {
		if opts.Metadata[k] != v {
			t.Errorf("%s metadata[%q] = %v, want %v", label, k, opts.Metadata[k], v)
		}
	}
}

// TestWorkflowDefinitionOptionsApplyAsWorkflowDefaults covers the merged
// defaults model: all workflow-level options are materialized into every job
// and callback options object, with per-job overrides winning. Metadata
// is encoded under options.metadata per the shared HTTP binding.
func TestWorkflowDefinitionOptionsApplyAsWorkflowDefaults(t *testing.T) {
	def := Batch(
		BatchCallbacks{
			OnComplete: &Step{
				Type:    "cb.done",
				Args:    Args{},
				Options: []EnqueueOption{WithQueue("callbacks")},
			},
			OnFailure: &Step{Type: "cb.alert", Args: Args{}},
		},
		Step{
			Type: "a.job",
			Args: Args{},
			Options: []EnqueueOption{
				WithPriority(9),
				WithMeta(map[string]any{"shared": "job"}),
			},
		},
	)
	def.Options = []EnqueueOption{
		WithQueue("default-jobs"),
		WithPriority(3),
		WithMeta(map[string]any{"tenant": "acme", "shared": "default"}),
	}

	req := captureWorkflowRequest(t, def)

	// Job: step priority overrides default, step queue inherits default,
	// metadata merged with step key winning.
	job := req.Jobs[0]
	assertWireOptions(t, "job", job.Options, "default-jobs", 9)
	assertOptionsMetadata(t, "job", job.Options, map[string]any{"tenant": "acme", "shared": "job"})

	// The on_complete callback: overrides queue only, inherits priority and metadata.
	onComplete := req.Callbacks.OnComplete
	assertWireOptions(t, "on_complete", onComplete.Options, "callbacks", 3)
	assertOptionsMetadata(t, "on_complete", onComplete.Options, map[string]any{
		"tenant": "acme",
		"shared": "default",
	})

	// The on_failure callback: inherits all defaults.
	onFailure := req.Callbacks.OnFailure
	assertWireOptions(t, "on_failure", onFailure.Options, "default-jobs", 3)
	assertOptionsMetadata(t, "on_failure", onFailure.Options, map[string]any{
		"tenant": "acme",
		"shared": "default",
	})
}

// TestCreateWorkflowOptsOverrideDefinitionDefaults proves CreateWorkflow's own
// variadic enqueue options remain honoured (existing API compatibility) and
// are applied after WorkflowDefinition.Options, so a call-site option can
// override a field the definition also set. Defaults are materialized into steps.
func TestCreateWorkflowOptsOverrideDefinitionDefaults(t *testing.T) {
	def := Chain(Step{Type: "a.job", Args: Args{}})
	def.Options = []EnqueueOption{WithQueue("definition-queue"), WithPriority(1)}

	req := captureWorkflowRequest(t, def, WithQueue("call-site-queue"))
	step := req.Steps[0]
	if step.Options == nil || step.Options.Queue != "call-site-queue" {
		t.Fatalf("step options = %+v, want call-site queue to win", step.Options)
	}
	if step.Options.Priority != 1 {
		t.Fatalf("step options = %+v, want the definition's priority preserved", step.Options)
	}
}

func TestCreateWorkflowMetaOptsOverrideDefinitionMetaByKey(t *testing.T) {
	def := Chain(Step{Type: "a.job", Args: Args{}})
	def.Options = []EnqueueOption{WithMeta(map[string]any{
		"tenant": "acme",
		"shared": "definition",
	})}

	req := captureWorkflowRequest(t, def, WithMeta(map[string]any{
		"shared": "call-site",
		"region": "eu-west",
	}))
	// Metadata-only defaults produce step options with just the metadata field.
	assertOptionsMetadata(t, "step", req.Steps[0].Options, map[string]any{
		"tenant": "acme",
		"shared": "call-site",
		"region": "eu-west",
	})
}

func TestWorkflowValidationRejectsEmptyCallbackType(t *testing.T) {
	def := Batch(BatchCallbacks{OnSuccess: &Step{Type: ""}}, Step{Type: "a.job"})
	if err := def.Validate(); err == nil {
		t.Error("a batch callback with an empty type must be rejected")
	}
}

func TestGroupValidationAllowsSingleJob(t *testing.T) {
	def := Group(Step{Type: "a.job", Args: Args{}})
	if err := def.Validate(); err != nil {
		t.Fatalf("single-job group must be valid: %v", err)
	}

	req := captureWorkflowRequest(t, def)
	if len(req.Jobs) != 1 || req.Jobs[0].Type != "a.job" {
		t.Fatalf("single-job group wire jobs = %+v, want one a.job", req.Jobs)
	}
}

func TestGroupValidationRejectsEmptyGroup(t *testing.T) {
	err := Group().Validate()
	if err == nil {
		t.Fatal("empty group must be rejected")
	}
	if want := "ojs: group workflow requires at least 1 job"; err.Error() != want {
		t.Fatalf("Validate() = %q, want %q", err, want)
	}
}

func TestWorkflowValidationMessages(t *testing.T) {
	cases := []struct {
		name string
		def  WorkflowDefinition
		want string
	}{
		{"chain empty", Chain(), "ojs: chain workflow requires at least 1 step"},
		{"group empty", Group(), "ojs: group workflow requires at least 1 job"},
		{"batch empty", Batch(BatchCallbacks{OnComplete: &Step{Type: "cb"}}), "ojs: batch workflow requires at least 1 job"},
		{"unknown", WorkflowDefinition{Type: "dag"}, `ojs: unknown workflow type "dag" (expected chain, group, or batch)`},
		{"chain empty type", Chain(Step{Type: ""}), "ojs: chain step 0 has empty type"},
		{"group empty type", Group(Step{Type: "a"}, Step{Type: ""}), "ojs: group job 1 has empty type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.Validate()
			if err == nil || err.Error() != tc.want {
				t.Errorf("Validate() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCreateWorkflowRejectsInvalidQueue(t *testing.T) {
	client, err := NewClient("http://localhost:8080")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.CreateWorkflow(context.Background(),
		Chain(Step{Type: "a.job", Args: Args{}}), WithQueue("Invalid Queue!"))
	if err == nil {
		t.Error("an invalid queue name must be rejected before the request is sent")
	}
}
