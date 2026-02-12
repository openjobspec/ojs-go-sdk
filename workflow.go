package ojs

import (
	"context"
	"fmt"
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

	// Options are default enqueue options for all steps.
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

// Chain creates a sequential workflow definition where steps execute one after another.
func Chain(steps ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type:  "chain",
		Steps: steps,
	}
}

// Group creates a parallel workflow definition where all jobs execute concurrently.
func Group(jobs ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type: "group",
		Jobs: jobs,
	}
}

// Batch creates a parallel workflow with callbacks based on collective outcome.
func Batch(callbacks BatchCallbacks, jobs ...Step) WorkflowDefinition {
	return WorkflowDefinition{
		Type:      "batch",
		Jobs:      jobs,
		Callbacks: &callbacks,
	}
}

// Workflow represents the server response for a workflow.
type Workflow struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	State     WorkflowState `json:"state"`
	CreatedAt *time.Time    `json:"created_at,omitempty"`

	// Steps contains the status of each workflow step.
	Steps []WorkflowStep `json:"steps,omitempty"`

	// For cancel responses.
	CancelledAt          *time.Time `json:"cancelled_at,omitempty"`
	StepsCancelled       int        `json:"steps_cancelled,omitempty"`
	StepsAlreadyComplete int        `json:"steps_already_completed,omitempty"`
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

// --- Wire format types ---

type workflowRequest struct {
	Name    string              `json:"name,omitempty"`
	Steps   []workflowStepWire `json:"steps"`
	Options *wireOptions        `json:"options,omitempty"`
}

type workflowStepWire struct {
	ID        string       `json:"id"`
	Type      string       `json:"type"`
	Args      []any        `json:"args"`
	DependsOn []string     `json:"depends_on"`
	Options   *wireOptions `json:"options,omitempty"`
}

// CreateWorkflow creates and starts a workflow.
func (c *Client) CreateWorkflow(ctx context.Context, def WorkflowDefinition, opts ...EnqueueOption) (*Workflow, error) {
	cfg := resolveEnqueueConfig(opts)

	req := workflowRequest{
		Name: def.Name,
	}

	if cfg.queue != "default" || cfg.timeoutMS > 0 {
		req.Options = buildWireOptions(cfg)
	}

	switch def.Type {
	case "chain":
		for i, s := range def.Steps {
			stepCfg := resolveEnqueueConfig(s.Options)
			step := workflowStepWire{
				ID:        fmt.Sprintf("step-%d", i),
				Type:      s.Type,
				Args:      argsToWire(s.Args),
				DependsOn: []string{},
			}
			if i > 0 {
				step.DependsOn = []string{fmt.Sprintf("step-%d", i-1)}
			}
			if stepCfg.queue != "default" || stepCfg.timeoutMS > 0 {
				step.Options = buildWireOptions(stepCfg)
			}
			req.Steps = append(req.Steps, step)
		}

	case "group":
		for i, s := range def.Jobs {
			step := workflowStepWire{
				ID:        fmt.Sprintf("job-%d", i),
				Type:      s.Type,
				Args:      argsToWire(s.Args),
				DependsOn: []string{},
			}
			req.Steps = append(req.Steps, step)
		}

	case "batch":
		for i, s := range def.Jobs {
			step := workflowStepWire{
				ID:        fmt.Sprintf("job-%d", i),
				Type:      s.Type,
				Args:      argsToWire(s.Args),
				DependsOn: []string{},
			}
			req.Steps = append(req.Steps, step)
		}
		// Add callback steps that depend on all jobs.
		jobIDs := make([]string, len(def.Jobs))
		for i := range def.Jobs {
			jobIDs[i] = fmt.Sprintf("job-%d", i)
		}
		if def.Callbacks != nil {
			if def.Callbacks.OnComplete != nil {
				req.Steps = append(req.Steps, workflowStepWire{
					ID:        "on-complete",
					Type:      def.Callbacks.OnComplete.Type,
					Args:      argsToWire(def.Callbacks.OnComplete.Args),
					DependsOn: jobIDs,
				})
			}
			if def.Callbacks.OnSuccess != nil {
				req.Steps = append(req.Steps, workflowStepWire{
					ID:        "on-success",
					Type:      def.Callbacks.OnSuccess.Type,
					Args:      argsToWire(def.Callbacks.OnSuccess.Args),
					DependsOn: jobIDs,
				})
			}
			if def.Callbacks.OnFailure != nil {
				req.Steps = append(req.Steps, workflowStepWire{
					ID:        "on-failure",
					Type:      def.Callbacks.OnFailure.Type,
					Args:      argsToWire(def.Callbacks.OnFailure.Args),
					DependsOn: jobIDs,
				})
			}
		}
	}

	var resp struct {
		Workflow Workflow `json:"workflow"`
	}
	if err := c.transport.post(ctx, basePath+"/workflows", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Workflow, nil
}

// GetWorkflow retrieves the current state of a workflow.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var resp struct {
		Workflow Workflow `json:"workflow"`
	}
	path := fmt.Sprintf("%s/workflows/%s", basePath, id)
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
	path := fmt.Sprintf("%s/workflows/%s", basePath, id)
	if err := c.transport.delete(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Workflow, nil
}
