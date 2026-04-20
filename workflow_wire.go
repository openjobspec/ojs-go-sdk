package ojs

// This file owns workflow topology: how a WorkflowDefinition's chain, group, or
// batch primitive is expanded into the discriminated wire request that the OJS
// workflow endpoint expects. It changes when the workflow spec changes,
// independently of the HTTP calls in workflow.go.
//
// The wire request mirrors the shared HTTP backend binding: `type` selects
// `steps` (chain), `jobs` (group/batch), and `callbacks` (batch), while each
// entry is a WorkflowJobRequest/WorkflowCallback envelope. Earlier versions
// flattened primitives and added synthetic IDs/dependency edges that the
// shared request types do not expose. Chain order is now represented solely by
// array position, and callbacks remain nested under their event keys.

// --- Wire format types ---

// workflowRequest is the body of `POST /ojs/v1/workflows`. Exactly one of
// Steps, Jobs is populated, chosen by Type; Callbacks is populated only for
// Type == "batch". This mirrors the JSON schema's `allOf`/`if`/`then` rules:
// chain requires `steps`, group requires `jobs`, batch requires `jobs` and
// `callbacks`.
type workflowRequest struct {
	// Type is the REQUIRED workflow-primitive discriminator: "chain", "group",
	// or "batch" (workflow.schema.json `type`).
	Type string `json:"type"`
	Name string `json:"name,omitempty"`

	// Steps is the ordered job list for a chain. Present only when Type ==
	// "chain".
	Steps []workflowStepWire `json:"steps,omitempty"`

	// Jobs is the unordered job list for a group or batch. Present only when
	// Type is "group" or "batch".
	Jobs []workflowStepWire `json:"jobs,omitempty"`

	// Callbacks carries the batch fan-in callbacks. Present only when Type ==
	// "batch".
	Callbacks *workflowCallbacksWire `json:"callbacks,omitempty"`
}

// workflowCallbacksWire is the batch `callbacks` object. Each populated field
// is a standalone job envelope; there is no depends_on back onto the batch
// jobs here -- "runs after all jobs finish/succeed/fail" is expressed by the
// callback's key, not by a dependency edge naming synthetic job IDs.
type workflowCallbacksWire struct {
	OnComplete *workflowStepWire `json:"on_complete,omitempty"`
	OnSuccess  *workflowStepWire `json:"on_success,omitempty"`
	OnFailure  *workflowStepWire `json:"on_failure,omitempty"`
}

// workflowStepWire is one job envelope inside a workflow: a chain step, a
// group/batch job, or a batch callback. It mirrors the shared backend's
// WorkflowJobRequest/WorkflowCallback shape.
type workflowStepWire struct {
	Type string `json:"type"`
	Args []any  `json:"args"`

	Options *wireOptions `json:"options,omitempty"`
}

// mergeWorkflowStepOptions resolves a single step's options, merging workflow-level
// defaults with per-step overrides. Metadata is encoded under options.metadata
// per the shared HTTP binding (WorkflowJobRequest has no sibling meta field).
func mergeWorkflowStepOptions(defaultCfg enqueueConfig, overrides []EnqueueOption) *wireOptions {
	stepCfg := resolveEnqueueConfig(overrides)
	merged := mergeEnqueueConfigs(defaultCfg, stepCfg)
	if !merged.hasOverrides() {
		return nil
	}
	return buildWireOptionsWithMetadata(merged)
}

// mergeEnqueueConfigs produces a merged config where step-level values override
// defaults. For metadata, per-step keys override default keys (shallow merge).
func mergeEnqueueConfigs(defaults, step enqueueConfig) enqueueConfig {
	merged := defaults
	if step.queueSet {
		merged.queue = step.queue
		merged.queueSet = true
	}
	if step.prioritySet {
		merged.priority = step.priority
		merged.prioritySet = true
	}
	if step.timeoutSet {
		merged.timeoutMS = step.timeoutMS
		merged.timeoutSet = true
	}
	if step.delayUntil != nil {
		merged.delayUntil = step.delayUntil
	}
	if step.expiresAt != nil {
		merged.expiresAt = step.expiresAt
	}
	if step.retry != nil {
		merged.retry = step.retry
	}
	if step.unique != nil {
		merged.unique = step.unique
	}
	if len(step.tags) > 0 {
		merged.tags = step.tags
	}
	if step.visibilityTimeout != 0 {
		merged.visibilityTimeout = step.visibilityTimeout
	}
	// Metadata: merge keys, step overrides defaults.
	merged.meta = mergeWorkflowMeta(defaults.meta, step.meta)
	return merged
}

// mergeWorkflowMeta creates a new top-level map for one job. It never mutates
// either source map; values are retained as-is so nested ext_* structs/maps
// preserve their JSON shape. Per-job values override workflow defaults by key.
func mergeWorkflowMeta(defaults, overrides map[string]any) map[string]any {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := make(map[string]any, len(defaults)+len(overrides))
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

// newStepWire builds one shared-backend-compatible job envelope.
func newStepWire(s Step, defaultCfg enqueueConfig) workflowStepWire {
	opts := mergeWorkflowStepOptions(defaultCfg, s.Options)
	return workflowStepWire{
		Type:    s.Type,
		Args:    argsToWire(s.Args),
		Options: opts,
	}
}

// buildWorkflowRequest expands a validated definition into its discriminated
// wire request. cfg holds the already-merged workflow-level enqueue defaults
// (WorkflowDefinition.Options plus CreateWorkflow's own enqueue options); see
// resolveWorkflowDefaults in workflow.go.
func buildWorkflowRequest(def *WorkflowDefinition, cfg enqueueConfig) workflowRequest {
	req := workflowRequest{Type: def.Type, Name: def.Name}

	switch def.Type {
	case workflowTypeChain:
		req.Steps = buildChainSteps(def.Steps, cfg)
	case workflowTypeGroup:
		req.Jobs = buildJobSteps(def.Jobs, cfg)
	case workflowTypeBatch:
		req.Jobs = buildJobSteps(def.Jobs, cfg)
		req.Callbacks = buildCallbacksWire(def.Callbacks, cfg)
	}
	return req
}

// buildChainSteps emits the ordered chain steps. The shared backend derives
// chain ordering from array position; WorkflowJobRequest has no id or
// depends_on fields.
func buildChainSteps(steps []Step, defaultCfg enqueueConfig) []workflowStepWire {
	wire := make([]workflowStepWire, 0, len(steps))
	for _, s := range steps {
		wire = append(wire, newStepWire(s, defaultCfg))
	}
	return wire
}

// buildJobSteps emits the unordered group/batch job list.
func buildJobSteps(jobs []Step, defaultCfg enqueueConfig) []workflowStepWire {
	wire := make([]workflowStepWire, 0, len(jobs))
	for _, s := range jobs {
		wire = append(wire, newStepWire(s, defaultCfg))
	}
	return wire
}

// buildCallbacksWire maps the batch callbacks onto the wire `callbacks`
// object. A nil result (no callbacks configured) never reaches here: Validate
// rejects a batch with no callbacks before the request is built.
func buildCallbacksWire(cb *BatchCallbacks, defaultCfg enqueueConfig) *workflowCallbacksWire {
	if cb == nil {
		return nil
	}
	wire := &workflowCallbacksWire{}
	if cb.OnComplete != nil {
		step := newStepWire(*cb.OnComplete, defaultCfg)
		wire.OnComplete = &step
	}
	if cb.OnSuccess != nil {
		step := newStepWire(*cb.OnSuccess, defaultCfg)
		wire.OnSuccess = &step
	}
	if cb.OnFailure != nil {
		step := newStepWire(*cb.OnFailure, defaultCfg)
		wire.OnFailure = &step
	}
	return wire
}
