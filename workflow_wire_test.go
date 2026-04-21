package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// This file golden-tests the discriminated workflow wire (workflow_wire.go)
// against the shape mandated by spec/spec/ojs-workflows.md §3 and
// ojs-json-schema/schemas/v1/workflow.schema.json, and drives a standalone
// fake server that implements those same discriminated-union rules to prove
// two things: every request this SDK sends conforms, and the check itself is
// not a no-op (it rejects hand-built nonconforming requests).

// --- A strict fake server that enforces the discriminated union ---

// workflowWireViolation describes one normative rule broken by a candidate
// workflow request body.
type workflowWireViolation struct {
	path    string
	message string
}

func (v workflowWireViolation) String() string {
	return fmt.Sprintf("%s: %s", v.path, v.message)
}

// validateWorkflowWireShape checks a decoded workflow request body against the
// discriminated-union rules in workflow.schema.json: `type` is REQUIRED, and
// exactly one topology field set is legal per type. It never inspects
// individual option values -- only the envelope shape -- because the
// job-options schema is validated elsewhere.
func validateWorkflowWireShape(body map[string]any) []workflowWireViolation {
	rawType, hasType := body["type"]
	typ, _ := rawType.(string)
	if !hasType || typ == "" {
		return []workflowWireViolation{{"$.type", "required field is missing"}}
	}

	var violations []workflowWireViolation
	switch typ {
	case "chain":
		violations = validateChainShape(body)
	case "group":
		violations = validateGroupShape(body)
	case "batch":
		violations = validateBatchShape(body)
	default:
		return []workflowWireViolation{{"$.type", fmt.Sprintf("unknown workflow type %q", typ)}}
	}
	if _, present := body["meta"]; present {
		violations = append(violations, workflowWireViolation{
			"$.meta",
			"client metadata must be materialized into jobs; workflow root meta is unsupported",
		})
	}
	if _, present := body["options"]; present {
		violations = append(violations, workflowWireViolation{
			"$.options",
			"workflow defaults must be materialized into jobs; workflow root options is unsupported",
		})
	}
	return violations
}

// disallowedFields reports one violation per name in fields that is present
// in body: the discriminated wire never carries more than one of
// steps/jobs/callbacks for a given type.
func disallowedFields(body map[string]any, typ string, fields ...string) []workflowWireViolation {
	var violations []workflowWireViolation
	for _, f := range fields {
		if _, present := body[f]; present {
			violations = append(violations, workflowWireViolation{"$." + f, fmt.Sprintf("must not be present for type=%s", typ)})
		}
	}
	return violations
}

func validateChainShape(body map[string]any) []workflowWireViolation {
	var violations []workflowWireViolation
	if steps, ok := body["steps"]; !ok {
		violations = append(violations, workflowWireViolation{"$.steps", "required for type=chain"})
	} else if !isNonEmptyArray(steps) {
		violations = append(violations, workflowWireViolation{"$.steps", "must be a non-empty array"})
	}
	violations = append(violations, disallowedFields(body, "chain", "jobs", "callbacks")...)
	violations = append(violations, validateStepArray(body["steps"], "$.steps")...)
	return violations
}

func validateGroupShape(body map[string]any) []workflowWireViolation {
	var violations []workflowWireViolation
	if jobs, ok := body["jobs"]; !ok {
		violations = append(violations, workflowWireViolation{"$.jobs", "required for type=group"})
	} else if !isNonEmptyArray(jobs) {
		violations = append(violations, workflowWireViolation{"$.jobs", "must be a non-empty array"})
	}
	violations = append(violations, disallowedFields(body, "group", "steps", "callbacks")...)
	violations = append(violations, validateStepArray(body["jobs"], "$.jobs")...)
	return violations
}

func validateBatchShape(body map[string]any) []workflowWireViolation {
	var violations []workflowWireViolation
	if jobs, ok := body["jobs"]; !ok {
		violations = append(violations, workflowWireViolation{"$.jobs", "required for type=batch"})
	} else if !isNonEmptyArray(jobs) {
		violations = append(violations, workflowWireViolation{"$.jobs", "must be a non-empty array"})
	}
	violations = append(violations, disallowedFields(body, "batch", "steps")...)
	if callbacks, hasCallbacks := body["callbacks"]; !hasCallbacks {
		violations = append(violations, workflowWireViolation{"$.callbacks", "required for type=batch"})
	} else {
		violations = append(violations, validateCallbacksObject(callbacks)...)
	}
	violations = append(violations, validateStepArray(body["jobs"], "$.jobs")...)
	return violations
}

func isNonEmptyArray(v any) bool {
	arr, ok := v.([]any)
	return ok && len(arr) > 0
}

func validateStepArray(v any, path string) []workflowWireViolation {
	arr, ok := v.([]any)
	if !ok {
		return nil // already reported by the caller
	}
	var violations []workflowWireViolation
	for i, e := range arr {
		entry, ok := e.(map[string]any)
		if !ok {
			violations = append(violations, workflowWireViolation{fmt.Sprintf("%s[%d]", path, i), "must be an object"})
			continue
		}
		violations = append(violations, validateJobEnvelope(entry, fmt.Sprintf("%s[%d]", path, i))...)
	}
	return violations
}

func validateCallbacksObject(v any) []workflowWireViolation {
	obj, ok := v.(map[string]any)
	if !ok {
		return []workflowWireViolation{{"$.callbacks", "must be an object"}}
	}
	var violations []workflowWireViolation
	found := false
	for _, key := range []string{"on_complete", "on_success", "on_failure"} {
		cb, present := obj[key]
		if !present {
			continue
		}
		found = true
		entry, ok := cb.(map[string]any)
		if !ok {
			violations = append(violations, workflowWireViolation{"$.callbacks." + key, "must be an object"})
			continue
		}
		violations = append(violations, validateJobEnvelope(entry, "$.callbacks."+key)...)
	}
	for key := range obj {
		if key != "on_complete" && key != "on_success" && key != "on_failure" {
			violations = append(violations, workflowWireViolation{"$.callbacks." + key, "unknown callback key"})
		}
	}
	if !found {
		violations = append(violations, workflowWireViolation{"$.callbacks", "at least one of on_complete/on_success/on_failure is required"})
	}
	return violations
}

// validateJobEnvelope enforces workflow_step's required properties: `type`
// and `args` (workflow.schema.json $defs/workflow_step).
func validateJobEnvelope(entry map[string]any, path string) []workflowWireViolation {
	var violations []workflowWireViolation
	if t, ok := entry["type"].(string); !ok || t == "" {
		violations = append(violations, workflowWireViolation{path + ".type", "required non-empty string"})
	}
	if _, ok := entry["args"]; !ok {
		violations = append(violations, workflowWireViolation{path + ".args", "required array"})
	} else if _, ok := entry["args"].([]any); !ok {
		violations = append(violations, workflowWireViolation{path + ".args", "must be an array"})
	}
	for key := range entry {
		if key != "name" && key != "type" && key != "args" && key != "options" {
			violations = append(violations, workflowWireViolation{
				path + "." + key,
				"field is not part of the shared backend workflow job shape",
			})
		}
	}
	return violations
}

// newStrictWorkflowServer returns a fake OJS server that rejects any workflow
// request violating the discriminated-union wire shape with 400, and
// otherwise accepts it with 201, invoking onAccept with the decoded body for
// further assertions.
func newStrictWorkflowServer(t *testing.T, onAccept func(body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "invalid_request", "message": err.Error()},
			})
			return
		}

		if violations := validateWorkflowWireShape(body); len(violations) > 0 {
			msgs := make([]string, len(violations))
			for i, v := range violations {
				msgs[i] = v.String()
			}
			w.Header().Set("Content-Type", ojsContentType)
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    "invalid_workflow",
					"message": "workflow validation failed",
					"details": map[string]any{"validation_errors": msgs},
				},
			})
			return
		}

		if onAccept != nil {
			onAccept(body)
		}
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workflow": map[string]any{"id": "wf-1", "type": body["type"], "state": "running"},
		})
	}))
}

// --- Positive: every shape this SDK emits must satisfy the strict validator ---

func TestSDKWorkflowRequestsConformToDiscriminatedWireShape(t *testing.T) {
	cases := []struct {
		name string
		def  WorkflowDefinition
	}{
		{"chain", Chain(
			Step{Type: "order.validate", Args: Args{"order_id": "ord_123"}},
			Step{Type: "payment.charge", Args: Args{}},
		)},
		{"group", Group(
			Step{Type: "export.csv", Args: Args{"report_id": "rpt_456"}},
		)},
		{"batch all callbacks", Batch(
			BatchCallbacks{
				OnComplete: &Step{Type: "batch.report", Args: Args{}},
				OnSuccess:  &Step{Type: "batch.celebrate", Args: Args{}},
				OnFailure:  &Step{Type: "batch.alert", Args: Args{}},
			},
			Step{Type: "email.send", Args: Args{"to": "user1@example.com"}},
			Step{Type: "email.send", Args: Args{"to": "user2@example.com"}},
		)},
		{"batch single callback", Batch(
			BatchCallbacks{OnComplete: &Step{Type: "batch.report", Args: Args{}}},
			Step{Type: "email.send", Args: Args{}},
		)},
		{"chain with options and meta", func() WorkflowDefinition {
			def := Chain(
				Step{Type: "a.job", Args: Args{}, Options: []EnqueueOption{WithQueue("gpu"), WithMeta(map[string]any{"k": "v"})}},
				Step{Type: "b.job", Args: Args{}},
			)
			def.Options = []EnqueueOption{WithPriority(5), WithMeta(map[string]any{"tenant": "acme"})}
			return def
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			srv := newStrictWorkflowServer(t, func(body map[string]any) { captured = body })
			defer srv.Close()

			client, err := NewClient(srv.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := client.CreateWorkflow(context.Background(), tc.def); err != nil {
				t.Fatalf("CreateWorkflow rejected by strict server: %v", err)
			}
			if captured == nil {
				t.Fatal("strict server never accepted the request")
			}
		})
	}
}

func TestStrictWorkflowServerReceivesMaterializedMetadata(t *testing.T) {
	def := Batch(
		BatchCallbacks{
			OnComplete: &Step{Type: "complete.job", Args: Args{}},
			OnSuccess: &Step{
				Type:    "success.job",
				Args:    Args{},
				Options: []EnqueueOption{WithMeta(map[string]any{"shared": "callback"})},
			},
			OnFailure: &Step{Type: "failure.job", Args: Args{}},
		},
		Step{
			Type:    "a.job",
			Args:    Args{},
			Options: []EnqueueOption{WithMeta(map[string]any{"shared": "job"})},
		},
	)
	def.Options = []EnqueueOption{WithMeta(map[string]any{
		"tenant": "acme",
		"shared": "default",
		"ext_custom": map[string]any{
			"nested": true,
		},
	})}

	srv := newStrictWorkflowServer(t, func(body map[string]any) {
		if _, present := body["meta"]; present {
			t.Errorf("strict server received unsupported workflow-root meta: %v", body["meta"])
		}
		// Metadata is now inside options.metadata
		assertJobOptionsMetadata(t, "batch job", jobAt(t, body, 0), map[string]any{
			"tenant": "acme",
			"shared": "job",
		})
		assertJobOptionsMetadata(t, "on_complete", callbackByName(t, body, callbackKeyOnComplete), map[string]any{
			"tenant": "acme",
			"shared": "default",
		})
		assertJobOptionsMetadata(t, "on_success", callbackByName(t, body, callbackKeyOnSuccess), map[string]any{
			"tenant": "acme",
			"shared": "callback",
		})
		assertJobOptionsMetadata(t, "on_failure", callbackByName(t, body, callbackKeyOnFailure), map[string]any{
			"tenant": "acme",
			"shared": "default",
		})
		// Check nested ext_custom preserved
		onCompleteMeta := extractOptionsMetadata(t, callbackByName(t, body, callbackKeyOnComplete))
		nested := onCompleteMeta["ext_custom"].(map[string]any)
		if nested["nested"] != true {
			t.Errorf("nested ext_custom metadata = %v, want preserved", nested)
		}
	})
	defer srv.Close()

	client, err := NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.CreateWorkflow(context.Background(), def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
}

// extractOptionsMetadata extracts the decoded metadata map from a job envelope's
// options.metadata field in the raw JSON representation.
func extractOptionsMetadata(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()
	opts, ok := entry["options"].(map[string]any)
	if !ok {
		t.Fatalf("entry options = %v, want an object", entry["options"])
	}
	meta, ok := opts["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("options.metadata = %v, want an object", opts["metadata"])
	}
	return meta
}

// assertJobOptionsMetadata checks that a job envelope's options.metadata
// carries the expected key/value pairs.
func assertJobOptionsMetadata(t *testing.T, label string, entry map[string]any, want map[string]any) {
	t.Helper()
	meta := extractOptionsMetadata(t, entry)
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("%s options.metadata[%q] = %v, want %v", label, k, meta[k], v)
		}
	}
}

// --- Negative: hand-built nonconforming requests must be rejected ---

func TestStrictServerRejectsNonconformingWorkflowRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing type",
			body: `{"steps":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "unsupported workflow root meta",
			body: `{"type":"chain","meta":{"tenant":"acme"},"steps":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "unsupported workflow root options",
			body: `{"type":"chain","options":{"queue":"gpu"},"steps":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "unsupported workflow job meta",
			body: `{"type":"chain","steps":[{"type":"a.job","args":[],"meta":{"tenant":"acme"}}]}`,
		},
		{
			name: "unsupported workflow job dependency",
			body: `{"type":"chain","steps":[{"id":"step-0","type":"a.job","args":[],"depends_on":[]}]}`,
		},
		{
			name: "chain using jobs instead of steps",
			body: `{"type":"chain","jobs":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "chain with empty steps",
			body: `{"type":"chain","steps":[]}`,
		},
		{
			name: "group using steps instead of jobs",
			body: `{"type":"group","steps":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "group with no jobs field",
			body: `{"type":"group","name":"g"}`,
		},
		{
			name: "batch missing callbacks",
			body: `{"type":"batch","jobs":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "batch with empty callbacks object",
			body: `{"type":"batch","jobs":[{"type":"a.job","args":[]}],"callbacks":{}}`,
		},
		{
			name: "batch carrying flattened steps alongside jobs (the old synthetic topology)",
			body: `{"type":"batch","jobs":[{"type":"a.job","args":[]}],"steps":[{"id":"on-complete","type":"cb","args":[],"depends_on":["job-0"]}],"callbacks":{"on_complete":{"type":"cb","args":[]}}}`,
		},
		{
			name: "unknown workflow type",
			body: `{"type":"dag","steps":[{"type":"a.job","args":[]}]}`,
		},
		{
			name: "step missing type",
			body: `{"type":"chain","steps":[{"args":[]}]}`,
		},
		{
			name: "step missing args",
			body: `{"type":"chain","steps":[{"type":"a.job"}]}`,
		},
		{
			name: "callback envelope missing type",
			body: `{"type":"batch","jobs":[{"type":"a.job","args":[]}],"callbacks":{"on_complete":{"args":[]}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStrictWorkflowServer(t, func(map[string]any) {
				t.Error("strict server accepted a nonconforming request")
			})
			defer srv.Close()

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
				srv.URL+"/ojs/v1/workflows", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("NewRequestWithContext: %v", err)
			}
			req.Header.Set("Content-Type", ojsContentType)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for nonconforming body %s", resp.StatusCode, tc.body)
			}
		})
	}
}

// --- Golden JSON: exact wire shape for one representative of each primitive ---

// decodeJSONMap normalizes a struct through JSON so it can be compared to a
// map[string]any literal (numbers become float64, absent fields become
// "not present" rather than Go zero values).
func decodeJSONMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return m
}

func TestGoldenChainWorkflowWireShape(t *testing.T) {
	req := buildWorkflowRequest(&WorkflowDefinition{
		Type: workflowTypeChain,
		Name: "order-processing",
		Steps: []Step{
			{Type: "order.validate", Args: Args{"order_id": "ord_123"}},
			{Type: "payment.charge", Args: Args{}},
		},
	}, resolveEnqueueConfig(nil))

	got := decodeJSONMap(t, req)
	want := map[string]any{
		"type": "chain",
		"name": "order-processing",
		"steps": []any{
			map[string]any{
				"type": "order.validate",
				"args": []any{map[string]any{"order_id": "ord_123"}},
			},
			map[string]any{
				"type": "payment.charge",
				"args": []any{},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chain wire shape mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestGoldenGroupWorkflowWireShape(t *testing.T) {
	req := buildWorkflowRequest(&WorkflowDefinition{
		Type: workflowTypeGroup,
		Name: "multi-format-export",
		Jobs: []Step{
			{Type: "export.csv", Args: Args{"report_id": "rpt_456"}},
			{Type: "export.pdf", Args: Args{"report_id": "rpt_456"}},
		},
	}, resolveEnqueueConfig(nil))

	got := decodeJSONMap(t, req)
	want := map[string]any{
		"type": "group",
		"name": "multi-format-export",
		"jobs": []any{
			map[string]any{
				"type": "export.csv",
				"args": []any{map[string]any{"report_id": "rpt_456"}},
			},
			map[string]any{
				"type": "export.pdf",
				"args": []any{map[string]any{"report_id": "rpt_456"}},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("group wire shape mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestGoldenBatchWorkflowWireShape(t *testing.T) {
	req := buildWorkflowRequest(&WorkflowDefinition{
		Type: workflowTypeBatch,
		Name: "bulk-email-send",
		Jobs: []Step{
			{Type: "email.send", Args: Args{}},
		},
		Callbacks: &BatchCallbacks{
			OnComplete: &Step{Type: "batch.report", Args: Args{}},
		},
	}, resolveEnqueueConfig(nil))

	got := decodeJSONMap(t, req)
	want := map[string]any{
		"type": "batch",
		"name": "bulk-email-send",
		"jobs": []any{
			map[string]any{
				"type": "email.send",
				"args": []any{},
			},
		},
		"callbacks": map[string]any{
			"on_complete": map[string]any{
				"type": "batch.report",
				"args": []any{},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("batch wire shape mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// TestGoldenWorkflowDefaultsAndStepOverride locks the exact byte shape when
// workflow defaults and a step override are configured: every step carries
// its fully materialized effective options.
func TestGoldenWorkflowDefaultsAndStepOverride(t *testing.T) {
	def := Chain(
		Step{Type: "a.job", Args: Args{}, Options: []EnqueueOption{
			WithPriority(9),
			WithMeta(map[string]any{"shared": "job"}),
		}},
		Step{Type: "b.job", Args: Args{}},
	)
	def.Options = []EnqueueOption{
		WithQueue("gpu"),
		WithMeta(map[string]any{"tenant": "acme", "shared": "default"}),
	}

	req := buildWorkflowRequest(&def, resolveWorkflowDefaults(def, nil))
	got := decodeJSONMap(t, req)

	want := map[string]any{
		"type": "chain",
		"steps": []any{
			map[string]any{
				"type": "a.job",
				"args": []any{},
				"options": map[string]any{
					"queue":    "gpu",
					"priority": float64(9),
					"metadata": map[string]any{
						"tenant": "acme",
						"shared": "job",
					},
				},
			},
			map[string]any{
				"type": "b.job",
				"args": []any{},
				"options": map[string]any{
					"queue": "gpu",
					"metadata": map[string]any{
						"tenant": "acme",
						"shared": "default",
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wire shape mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}
