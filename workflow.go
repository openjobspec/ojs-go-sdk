package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// WorkflowState represents the lifecycle state of a workflow.
type WorkflowState string

const (
	WorkflowStatePending   WorkflowState = "pending"
	WorkflowStateRunning   WorkflowState = "running"
	WorkflowStateCompleted WorkflowState = "completed"
	WorkflowStateFailed    WorkflowState = "failed"
	WorkflowStateCancelled WorkflowState = "cancelled"
)

// Step represents a single step in a workflow.
type Step struct {
	// Type is the dot-namespaced job type.
	Type string

	// Args are the job arguments.
	Args Args

	// Options are optional per-step enqueue options.
	Options []EnqueueOption
}

// WorkflowDefinition describes a workflow to be created.
type WorkflowDefinition struct {
	// Type is the workflow primitive: "chain", "group", or "batch".
	Type string

	// Name is a human-readable workflow name.
	Name string

	// Steps are the workflow steps (used by Chain).
	Steps []Step

	// Jobs are the workflow jobs (used by Group and Batch).
	Jobs []Step

	// Callbacks are the batch callbacks (used by Batch).
	Callbacks *BatchCallbacks

	// Options are default enqueue options for all steps. Metadata options are
	// materialized into every job/callback because workflow-root meta is not
	// part of the OJS workflow schema.
	Options []EnqueueOption
}

// BatchCallbacks defines the callbacks for a batch workflow.
type BatchCallbacks struct {
	// OnComplete is enqueued when ALL jobs finish, regardless of outcome.
	OnComplete *Step

	// OnSuccess is enqueued only if ALL jobs succeeded.
	OnSuccess *Step

	// OnFailure is enqueued if ANY job failed.
	OnFailure *Step
}

// Workflow primitive identifiers used by WorkflowDefinition.Type.
const (
	workflowTypeChain = "chain"
	workflowTypeGroup = "group"
	workflowTypeBatch = "batch"
)

// Batch callback keys on the wire `callbacks` object
// (workflow.schema.json `callbacks.on_complete`/`on_success`/`on_failure`).
// Callbacks are addressed by this key, not by a synthetic step id: unlike the
// earlier flattened wire, a callback is never one of a "steps"/"jobs" array
// entry that needs an id to be found.
const (
	callbackKeyOnComplete = "on_complete"
	callbackKeyOnSuccess  = "on_success"
	callbackKeyOnFailure  = "on_failure"
)

// Chain creates a sequential workflow definition where steps execute one after another.
func Chain(steps ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type:  workflowTypeChain,
		Steps: steps,
	}
}

// Group creates a parallel workflow definition where all jobs execute concurrently.
func Group(jobs ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type: workflowTypeGroup,
		Jobs: jobs,
	}
}

// Batch creates a parallel workflow with callbacks based on collective outcome.
func Batch(callbacks BatchCallbacks, jobs ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type:      workflowTypeBatch,
		Jobs:      jobs,
		Callbacks: &callbacks,
	}
}

// MaxWorkflowSteps is the maximum number of steps allowed in a single workflow.
const MaxWorkflowSteps = 500

// Validate checks a WorkflowDefinition for common errors before sending to the server.
func (d WorkflowDefinition) Validate() error {
	switch d.Type {
	case workflowTypeChain:
		return validateSteps("chain", "step", d.Steps, 1)
	case workflowTypeGroup:
		return validateSteps("group", "job", d.Jobs, 1)
	case workflowTypeBatch:
		if err := validateSteps("batch", "job", d.Jobs, 1); err != nil {
			return err
		}
		return validateBatchCallbacks(d.Callbacks)
	default:
		return fmt.Errorf("ojs: unknown workflow type %q (expected chain, group, or batch)", d.Type)
	}
}

// validateSteps enforces the cardinality and per-step rules shared by every
// workflow primitive.
func validateSteps(primitive, noun string, steps []Step, minCount int) error {
	if len(steps) < minCount {
		if minCount == 1 {
			return fmt.Errorf("ojs: %s workflow requires at least 1 %s", primitive, noun)
		}
		return fmt.Errorf("ojs: %s workflow requires at least %d parallel %ss", primitive, minCount, noun)
	}
	if len(steps) > MaxWorkflowSteps {
		return fmt.Errorf("ojs: %s workflow exceeds maximum of %d %ss (got %d)", primitive, MaxWorkflowSteps, noun, len(steps))
	}
	for i, s := range steps {
		if s.Type == "" {
			return fmt.Errorf("ojs: %s %s %d has empty type", primitive, noun, i)
		}
	}
	return nil
}

// validateBatchCallbacks requires at least one callback and rejects callbacks
// that carry no job type, which would otherwise be sent as an unroutable step.
func validateBatchCallbacks(cb *BatchCallbacks) error {
	if cb == nil || (cb.OnComplete == nil && cb.OnSuccess == nil && cb.OnFailure == nil) {
		return fmt.Errorf("ojs: batch workflow requires at least one callback (on_complete, on_success, or on_failure)")
	}
	for _, c := range []struct {
		name string
		step *Step
	}{
		{"on_complete", cb.OnComplete},
		{"on_success", cb.OnSuccess},
		{"on_failure", cb.OnFailure},
	} {
		if c.step != nil && c.step.Type == "" {
			return fmt.Errorf("ojs: batch callback %s has empty type", c.name)
		}
	}
	return nil
}

// Workflow represents the server response for a workflow.
//
// Fields added backward-compatibly; all new fields use pointer or omitempty
// semantics so existing keyed-literal constructors compile unchanged.
type Workflow struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      string        `json:"type,omitempty"`
	State     WorkflowState `json:"state"`
	CreatedAt *time.Time    `json:"created_at,omitempty"`

	// Steps contains the status of each workflow step.
	Steps []WorkflowStep `json:"steps,omitempty"`

	// Progress counters for chains and groups/batches.
	StepsTotal     *int `json:"steps_total,omitempty"`
	StepsCompleted *int `json:"steps_completed,omitempty"`
	JobsTotal      *int `json:"jobs_total,omitempty"`
	JobsCompleted  *int `json:"jobs_completed,omitempty"`

	// Callbacks echoes the batch callback configuration.
	Callbacks *WorkflowResponseCallbacks `json:"callbacks,omitempty"`

	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// For cancel responses.
	CancelledAt          *time.Time `json:"cancelled_at,omitempty"`
	StepsCancelled       int        `json:"steps_cancelled,omitempty"`
	StepsAlreadyComplete int        `json:"steps_already_completed,omitempty"`
}

// WorkflowResponseCallbacks echoes the batch callback configuration in the
// workflow response, matching the backend JSON shape.
type WorkflowResponseCallbacks struct {
	OnComplete *WorkflowResponseCallback `json:"on_complete,omitempty"`
	OnSuccess  *WorkflowResponseCallback `json:"on_success,omitempty"`
	OnFailure  *WorkflowResponseCallback `json:"on_failure,omitempty"`
}

// WorkflowResponseCallback describes a single callback in the response.
type WorkflowResponseCallback struct {
	Type string `json:"type"`
	// Args are preserved as raw JSON from the server.
	Args json.RawMessage `json:"args,omitempty"`
	// Options are preserved as raw JSON so callback enqueue settings from newer
	// backends remain available without coupling this response model to private
	// request wire types.
	Options json.RawMessage `json:"options,omitempty"`
}

// WorkflowStep represents the status of an individual step in a workflow.
type WorkflowStep struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	State       string         `json:"state"`
	JobID       *string        `json:"job_id"`
	DependsOn   []string       `json:"depends_on"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
}

// CreateWorkflow creates and starts a workflow.
func (c *Client) CreateWorkflow(ctx context.Context, def WorkflowDefinition, opts ...EnqueueOption) (*Workflow, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}

	cfg := resolveWorkflowDefaults(def, opts)
	if err := validateQueue(cfg.queue); err != nil {
		return nil, err
	}

	req := buildWorkflowRequest(&def, cfg)

	var resp struct {
		Workflow Workflow `json:"workflow"`
	}
	if err := c.transport.post(ctx, basePath+"/workflows", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Workflow, nil
}

// resolveWorkflowDefaults merges the documented WorkflowDefinition.Options
// field with CreateWorkflow's own variadic enqueue options. The result is
// materialized into every job and callback because the shared workflow request
// has no root enqueue-options field.
//
// def.Options is the reusable definition-level default; opts are supplied at
// the CreateWorkflow call site and are applied afterward, so a caller building
// a shared WorkflowDefinition can still override a specific field (e.g. the
// queue) for one invocation without mutating the definition. This preserves
// CreateWorkflow's existing variadic-option contract: a definition with no
// Options set behaves exactly as before, driven entirely by the call-site
// opts.
func resolveWorkflowDefaults(def WorkflowDefinition, opts []EnqueueOption) enqueueConfig {
	merged := make([]EnqueueOption, 0, len(def.Options)+len(opts))
	merged = append(merged, def.Options...)
	merged = append(merged, opts...)
	return resolveEnqueueConfig(merged)
}

// GetWorkflow retrieves the current state of a workflow.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var resp struct {
		Workflow Workflow `json:"workflow"`
	}
	path := fmt.Sprintf("%s/workflows/%s", basePath, url.PathEscape(id))
	if err := c.transport.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Workflow, nil
}

// CancelWorkflow cancels a workflow and all its non-terminal steps.
func (c *Client) CancelWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var resp struct {
		Workflow Workflow `json:"workflow"`
	}
	path := fmt.Sprintf("%s/workflows/%s", basePath, url.PathEscape(id))
	if err := c.transport.delete(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Workflow, nil
}
