package ojs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureWorkflowJSON runs CreateWorkflow against a stub server and returns the
// raw JSON body it received, so tests can assert the exact wire representation
// rather than the Go structs that produced it.
func captureWorkflowJSON(t *testing.T, def WorkflowDefinition, opts ...EnqueueOption) map[string]any {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		if body, err = io.ReadAll(r.Body); err != nil {
			t.Errorf("read workflow request: %v", err)
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

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode workflow request %s: %v", body, err)
	}
	return decoded
}

// jsonOptionsMetadata extracts the metadata map from a job entry's
// options.metadata in the raw JSON representation.
func jsonOptionsMetadata(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()
	opts, ok := entry["options"].(map[string]any)
	if !ok {
		return nil
	}
	meta, ok := opts["metadata"].(map[string]any)
	if !ok {
		return nil
	}
	return meta
}

// entryAt returns one job envelope from the named top-level array field.
func entryAt(t *testing.T, req map[string]any, field string, index int) map[string]any {
	t.Helper()
	entries, ok := req[field].([]any)
	if !ok {
		t.Fatalf("%s = %v, want an array", field, req[field])
	}
	if index < 0 || index >= len(entries) {
		t.Fatalf("%s[%d] is out of range for %d entries", field, index, len(entries))
	}
	entry, ok := entries[index].(map[string]any)
	if !ok {
		t.Fatalf("%s[%d] = %T(%v), want an object", field, index, entries[index], entries[index])
	}
	return entry
}

// stepAt looks up a chain step by array position.
func stepAt(t *testing.T, req map[string]any, index int) map[string]any {
	t.Helper()
	return entryAt(t, req, "steps", index)
}

// jobAt looks up a group/batch job by array position.
func jobAt(t *testing.T, req map[string]any, index int) map[string]any {
	t.Helper()
	return entryAt(t, req, "jobs", index)
}

// callbackByName looks up a batch callback job envelope by its callbacks key
// ("on_complete", "on_success", or "on_failure").
func callbackByName(t *testing.T, req map[string]any, name string) map[string]any {
	t.Helper()
	callbacks, ok := req["callbacks"].(map[string]any)
	if !ok {
		t.Fatalf("callbacks = %v, want an object", req["callbacks"])
	}
	cb, ok := callbacks[name].(map[string]any)
	if !ok {
		t.Fatalf("callbacks.%s = %v, want a job envelope", name, callbacks[name])
	}
	return cb
}

// TestWorkflowLevelMetaIsMaterializedForEveryJobKind is a regression test:
// workflow-root `meta` is not part of the workflow request schema, so defaults
// must be copied onto every chain step, group job, batch job, and callback.
func TestWorkflowLevelMetaIsMaterializedForEveryJobKind(t *testing.T) {
	cases := []struct {
		name    string
		def     WorkflowDefinition
		opts    []EnqueueOption
		entries func(*testing.T, map[string]any) []map[string]any
	}{
		{
			name: "chain steps from call-site options",
			def: Chain(
				Step{Type: "a.job", Args: Args{}},
				Step{Type: "b.job", Args: Args{}},
			),
			opts: []EnqueueOption{WithMeta(map[string]any{"tenant_id": "acme", "locale": "de-DE"})},
			entries: func(t *testing.T, req map[string]any) []map[string]any {
				return []map[string]any{
					stepAt(t, req, 0),
					stepAt(t, req, 1),
				}
			},
		},
		{
			name: "group jobs from definition options",
			def: func() WorkflowDefinition {
				def := Group(
					Step{Type: "a.job", Args: Args{}},
					Step{Type: "b.job", Args: Args{}},
				)
				def.Options = []EnqueueOption{WithMeta(map[string]any{
					"tenant_id": "acme",
					"locale":    "de-DE",
				})}
				return def
			}(),
			entries: func(t *testing.T, req map[string]any) []map[string]any {
				return []map[string]any{
					jobAt(t, req, 0),
					jobAt(t, req, 1),
				}
			},
		},
		{
			name: "batch jobs and callbacks",
			def: Batch(
				BatchCallbacks{
					OnComplete: &Step{Type: "complete.job", Args: Args{}},
					OnSuccess:  &Step{Type: "success.job", Args: Args{}},
					OnFailure:  &Step{Type: "failure.job", Args: Args{}},
				},
				Step{Type: "a.job", Args: Args{}},
			),
			opts: []EnqueueOption{WithMeta(map[string]any{"tenant_id": "acme", "locale": "de-DE"})},
			entries: func(t *testing.T, req map[string]any) []map[string]any {
				return []map[string]any{
					jobAt(t, req, 0),
					callbackByName(t, req, callbackKeyOnComplete),
					callbackByName(t, req, callbackKeyOnSuccess),
					callbackByName(t, req, callbackKeyOnFailure),
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := captureWorkflowJSON(t, tc.def, tc.opts...)
			if _, present := req["meta"]; present {
				t.Fatalf("unsupported workflow-root meta was emitted: %v", req["meta"])
			}
			for _, entry := range tc.entries(t, req) {
				meta := jsonOptionsMetadata(t, entry)
				if meta == nil {
					t.Fatalf("job options.metadata = nil, want values")
				}
				if meta["tenant_id"] != "acme" || meta["locale"] != "de-DE" {
					t.Errorf("job options.metadata = %v, want both workflow metadata values", meta)
				}
			}
		})
	}
}

// TestWorkflowStepMetaIsSentForEveryPrimitive covers chain steps, group jobs,
// batch jobs, and batch callbacks in one pass: all four route through the same
// step mapping and all four dropped metadata before. Each primitive looks its
// entry up in the wire container the discriminated wire actually uses for it
// ("steps" for chain, "jobs" for group/batch, "callbacks.<key>" for batch
// callbacks), rather than a single flattened "steps" array.
func TestWorkflowStepMetaIsSentForEveryPrimitive(t *testing.T) {
	metaOpt := func(v string) []EnqueueOption {
		return []EnqueueOption{WithMeta(map[string]any{"tenant_id": v})}
	}

	cases := []struct {
		name   string
		def    WorkflowDefinition
		lookup func(t *testing.T, req map[string]any) map[string]any
		want   string
	}{
		{
			name: "chain step",
			def:  Chain(Step{Type: "a.job", Args: Args{}, Options: metaOpt("chain")}),
			lookup: func(t *testing.T, req map[string]any) map[string]any {
				return stepAt(t, req, 0)
			},
			want: "chain",
		},
		{
			name: "group job",
			def: Group(
				Step{Type: "a.job", Args: Args{}, Options: metaOpt("group")},
				Step{Type: "b.job", Args: Args{}},
			),
			lookup: func(t *testing.T, req map[string]any) map[string]any {
				return jobAt(t, req, 0)
			},
			want: "group",
		},
		{
			name: "batch job",
			def: Batch(
				BatchCallbacks{OnComplete: &Step{Type: "done.job", Args: Args{}}},
				Step{Type: "a.job", Args: Args{}, Options: metaOpt("batch")},
			),
			lookup: func(t *testing.T, req map[string]any) map[string]any {
				return jobAt(t, req, 0)
			},
			want: "batch",
		},
		{
			name: "batch callback",
			def: Batch(
				BatchCallbacks{OnSuccess: &Step{Type: "done.job", Args: Args{}, Options: metaOpt("callback")}},
				Step{Type: "a.job", Args: Args{}},
			),
			lookup: func(t *testing.T, req map[string]any) map[string]any {
				return callbackByName(t, req, callbackKeyOnSuccess)
			},
			want: "callback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := captureWorkflowJSON(t, tc.def)
			step := tc.lookup(t, req)
			meta := jsonOptionsMetadata(t, step)
			if meta == nil {
				t.Fatalf("step options.metadata = nil, want an object")
			}
			if meta["tenant_id"] != tc.want {
				t.Errorf("step options.metadata.tenant_id = %v, want %q", meta["tenant_id"], tc.want)
			}
		})
	}
}

// TestWorkflowStepMetaAndOptionsCoexist locks the exact wire shape when both are
// configured: two sibling objects, neither nested in the other.
func TestWorkflowStepMetaAndOptionsCoexist(t *testing.T) {
	req := captureWorkflowJSON(t, Chain(Step{
		Type: "a.job",
		Args: Args{"k": "v"},
		Options: []EnqueueOption{
			WithQueue("gpu"),
			WithPriority(7),
			WithMeta(map[string]any{"correlation_id": "abc-123"}),
		},
	}))

	step := stepAt(t, req, 0)
	options, ok := step["options"].(map[string]any)
	if !ok {
		t.Fatalf("step options = %v, want an object", step["options"])
	}
	if options["queue"] != "gpu" {
		t.Errorf("options.queue = %v, want gpu", options["queue"])
	}
	if options["priority"] != float64(7) {
		t.Errorf("options.priority = %v, want 7", options["priority"])
	}
	meta, ok := options["metadata"].(map[string]any)
	if !ok || meta["correlation_id"] != "abc-123" {
		t.Fatalf("options.metadata = %v, want correlation_id", options["metadata"])
	}
	if _, hasMeta := step["meta"]; hasMeta {
		t.Error("meta must not be a sibling of options on workflow steps")
	}
}

// TestWorkflowStepMetaPreservesNestedMLResources proves the extension payloads
// that travel in meta survive the workflow mapping unflattened.
func TestWorkflowStepMetaPreservesNestedMLResources(t *testing.T) {
	res := NewMLResources().
		WithAccelerator("gpu").
		WithGPUType("nvidia-h100").
		WithGPUCount(8).
		WithGPUMemoryGB(80)

	req := captureWorkflowJSON(t, Group(
		Step{Type: "train.job", Args: Args{}, Options: []EnqueueOption{
			WithMLResources(res),
			WithNodeSelector(map[string]string{"zone": "us-east-1a"}),
		}},
		Step{Type: "eval.job", Args: Args{}},
	))

	step := jobAt(t, req, 0)
	meta := jsonOptionsMetadata(t, step)
	if meta == nil {
		t.Fatalf("step options.metadata = nil, want an object")
	}

	ml, ok := meta["ext_ml_resources"].(map[string]any)
	if !ok {
		t.Fatalf("options.metadata.ext_ml_resources = %v, want a nested object", meta["ext_ml_resources"])
	}
	if ml["ext_ml_accelerator"] != "gpu" || ml["ext_ml_gpu_type"] != "nvidia-h100" {
		t.Errorf("ext_ml_resources = %v, want the accelerator and GPU type preserved", ml)
	}
	if ml["ext_ml_gpu_count"] != float64(8) || ml["ext_ml_gpu_memory_gb"] != float64(80) {
		t.Errorf("ext_ml_resources = %v, want gpu_count=8 and gpu_memory_gb=80", ml)
	}

	sel, ok := meta["ext_node_selector"].(map[string]any)
	if !ok || sel["zone"] != "us-east-1a" {
		t.Errorf("options.metadata.ext_node_selector = %v, want the node selector preserved", meta["ext_node_selector"])
	}
}

// TestWorkflowLevelMetaPreservesNestedMLResources covers the same for
// materialized workflow-level defaults.
func TestWorkflowLevelMetaPreservesNestedMLResources(t *testing.T) {
	req := captureWorkflowJSON(t,
		Chain(
			Step{Type: "a.job", Args: Args{}},
			Step{Type: "b.job", Args: Args{}},
		),
		WithMLResources(NewMLResources().WithAccelerator("tpu").WithTPUType("v5e")),
	)

	if _, present := req["meta"]; present {
		t.Fatalf("unsupported workflow-root meta was emitted: %v", req["meta"])
	}
	for i := range 2 {
		meta := jsonOptionsMetadata(t, stepAt(t, req, i))
		if meta == nil {
			t.Fatalf("steps[%d] options.metadata = nil, want an object", i)
		}
		ml, ok := meta["ext_ml_resources"].(map[string]any)
		if !ok {
			t.Fatalf("steps[%d] options.metadata.ext_ml_resources = %v, want a nested object", i, meta["ext_ml_resources"])
		}
		if ml["ext_ml_accelerator"] != "tpu" || ml["ext_ml_tpu_type"] != "v5e" {
			t.Errorf("steps[%d] ext_ml_resources = %v, want the TPU requirements preserved", i, ml)
		}
	}
}

func TestWorkflowMetadataMergeDoesNotMutateCallerMaps(t *testing.T) {
	defaultNested := map[string]any{"accelerator": "gpu", "count": 4}
	defaultMeta := map[string]any{
		"tenant":           "acme",
		"shared":           "default",
		"ext_custom_stack": defaultNested,
	}
	jobNested := map[string]any{"accelerator": "tpu", "topology": "2x4"}
	jobMeta := map[string]any{
		"shared":           "job",
		"ext_custom_stack": jobNested,
	}

	def := Chain(Step{
		Type:    "a.job",
		Args:    Args{},
		Options: []EnqueueOption{WithMeta(jobMeta)},
	})
	def.Options = []EnqueueOption{WithMeta(defaultMeta)}

	req := captureWorkflowJSON(t, def)
	meta := jsonOptionsMetadata(t, stepAt(t, req, 0))
	if meta == nil {
		t.Fatalf("step options.metadata = nil")
	}
	if meta["tenant"] != "acme" || meta["shared"] != "job" {
		t.Fatalf("merged metadata = %v, want inherited tenant and per-job shared override", meta)
	}
	nested, ok := meta["ext_custom_stack"].(map[string]any)
	if !ok || nested["accelerator"] != "tpu" || nested["topology"] != "2x4" {
		t.Fatalf("merged ext_custom_stack = %v, want the per-job nested value intact", meta["ext_custom_stack"])
	}

	if defaultMeta["shared"] != "default" || defaultMeta["tenant"] != "acme" {
		t.Fatalf("default caller map mutated: %v", defaultMeta)
	}
	if defaultNested["accelerator"] != "gpu" || defaultNested["count"] != 4 {
		t.Fatalf("nested default caller map mutated: %v", defaultNested)
	}
	if jobMeta["shared"] != "job" || jobNested["accelerator"] != "tpu" {
		t.Fatalf("per-job caller map mutated: meta=%v nested=%v", jobMeta, jobNested)
	}
}

// TestWorkflowMetaOmittedWhenUnset keeps the payload byte-identical for
// workflows that set no metadata.
func TestWorkflowMetaOmittedWhenUnset(t *testing.T) {
	req := captureWorkflowJSON(t, Chain(
		Step{Type: "a.job", Args: Args{}},
		Step{Type: "b.job", Args: Args{}, Options: []EnqueueOption{WithQueue("gpu")}},
	))

	if _, present := req["meta"]; present {
		t.Errorf("workflow meta = %v, want omitted when unset", req["meta"])
	}
	step0 := stepAt(t, req, 0)
	if step0["options"] != nil {
		opts := step0["options"].(map[string]any)
		if _, hasMeta := opts["metadata"]; hasMeta {
			t.Errorf("step-0 carries metadata but set none")
		}
	}
	step1 := stepAt(t, req, 1)
	opts, ok := step1["options"].(map[string]any)
	if !ok || opts["queue"] != "gpu" {
		t.Errorf("step-1 options = %v, want queue=gpu", step1["options"])
	}
	if _, hasMeta := opts["metadata"]; hasMeta {
		t.Errorf("step-1 carries metadata but set none")
	}
}

func TestWorkflowMetadataSerializationErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent when workflow metadata cannot be encoded")
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.CreateWorkflow(context.Background(),
		Chain(Step{Type: "a.job", Args: Args{}}),
		WithMeta(map[string]any{"unsupported": make(chan int)}),
	)
	if err == nil {
		t.Fatal("CreateWorkflow succeeded with unencodable metadata")
	}
}

// TestEnqueueMetaWireIsUnchanged guards the single-job path: meta already
// reached the wire there, and the override-detection change must not duplicate
// it into the options object.
func TestEnqueueMetaWireIsUnchanged(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode enqueue request: %v", err)
		}
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"job": map[string]any{"id": "j-1"}})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Enqueue(context.Background(), "a.job", Args{},
		WithMeta(map[string]any{"tenant_id": "acme"})); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	meta, ok := body["meta"].(map[string]any)
	if !ok || meta["tenant_id"] != "acme" {
		t.Fatalf("meta = %v, want tenant_id=acme at the envelope level", body["meta"])
	}
	options, ok := body["options"].(map[string]any)
	if !ok {
		t.Fatalf("options = %v, want the unchanged options object", body["options"])
	}
	if _, nested := options["meta"]; nested {
		t.Error("meta must not appear inside options")
	}
}

// TestHasOverridesCountsMeta locks the override-detection rule directly.
func TestHasOverridesCountsMeta(t *testing.T) {
	metaOnly := resolveEnqueueConfig([]EnqueueOption{WithMeta(map[string]any{"a": 1})})
	if !metaOnly.hasOverrides() {
		t.Error("metadata is an override: dropping it discards tenant routing and every ext_* extension")
	}
	if metaOnly.hasOptionOverrides() {
		t.Error("metadata alone does not trigger hasOptionOverrides")
	}

	none := resolveEnqueueConfig(nil)
	if none.hasOverrides() || none.hasOptionOverrides() {
		t.Error("a default config overrides nothing")
	}

	empty := resolveEnqueueConfig([]EnqueueOption{WithMeta(map[string]any{})})
	if empty.hasOverrides() {
		t.Error("an empty metadata map is not an override")
	}
}
