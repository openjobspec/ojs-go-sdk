// Package agent provides a client for the OJS Agent API, enabling
// fork/merge branching, pause/resume human-in-the-loop control, and
// deterministic replay of agent job executions.
//
// This package is part of OJS Labs — forward-looking R&D that is not
// part of the core release train. APIs may change between minor versions.
// See https://openjobspec.org/docs/moonshots/ for details.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Sentinel errors returned by AgentClient methods.
var (
	// ErrAgentNotFound is returned when the requested agent job does not exist.
	ErrAgentNotFound = errors.New("ojs: agent not found")

	// ErrBranchConflict is returned when a merge cannot be completed due to
	// conflicting changes between branches.
	ErrBranchConflict = errors.New("ojs: branch conflict")

	// ErrAgentNotPaused is returned when a resume is attempted on an agent
	// that is not in the paused state.
	ErrAgentNotPaused = errors.New("ojs: agent is not paused")
)

// MergeStrategy controls how conflicting turns are reconciled during a merge.
type MergeStrategy string

const (
	// MergeOurs keeps the changes from branch A on conflict.
	MergeOurs MergeStrategy = "ours"
	// MergeTheirs keeps the changes from branch B on conflict.
	MergeTheirs MergeStrategy = "theirs"
	// MergeUnion combines non-overlapping changes from both branches.
	MergeUnion MergeStrategy = "union"
)

// ForkOptions configures where a new branch diverges from the main execution.
type ForkOptions struct {
	// AtTurn is the zero-based turn index at which the fork begins.
	AtTurn int `json:"at_turn"`
	// BranchName is the human-readable label for the new branch.
	BranchName string `json:"branch_name"`
}

// ForkResult contains the identifiers produced by a successful fork.
type ForkResult struct {
	// BranchID is the unique identifier of the newly created branch.
	BranchID string `json:"branch_id"`
	// ContentID is the content-addressable hash of the branch snapshot.
	ContentID string `json:"content_id"`
}

// MergeOptions specifies the two branches to merge and the strategy to use.
type MergeOptions struct {
	// BranchA is the identifier of the first branch.
	BranchA string `json:"branch_a"`
	// BranchB is the identifier of the second branch.
	BranchB string `json:"branch_b"`
	// Strategy selects the conflict resolution approach.
	Strategy MergeStrategy `json:"strategy"`
}

// MergeResult describes the outcome of a merge operation.
type MergeResult struct {
	// MergedID is the identifier of the resulting merged branch.
	MergedID string `json:"merged_id"`
	// Conflicts lists the turn or field paths that could not be auto-resolved.
	Conflicts []string `json:"conflicts"`
}

// ResumeDecision carries the human reviewer's verdict for a paused agent.
type ResumeDecision struct {
	// Approved indicates whether the agent should continue execution.
	Approved bool `json:"approved"`
	// Comment is an optional note from the reviewer.
	Comment string `json:"comment"`
	// Metadata holds arbitrary key-value pairs attached to the decision.
	Metadata map[string]any `json:"metadata"`
}

// ReplayOptions configures a deterministic replay of a previous execution.
type ReplayOptions struct {
	// FromTurn is the zero-based turn index from which replay begins.
	FromTurn int `json:"from_turn"`
	// MockProviders maps provider names to canned response identifiers
	// used during the replay instead of live calls.
	MockProviders map[string]string `json:"mock_providers"`
}

// ReplayResult summarises a completed replay run.
type ReplayResult struct {
	// Steps is the total number of turns that were replayed.
	Steps int `json:"steps"`
	// Divergences lists every turn where the replay produced output that
	// differs from the original execution.
	Divergences []Divergence `json:"divergences"`
}

// Divergence records a single point where a replay's output differed from the
// original execution.
type Divergence struct {
	// Turn is the zero-based index of the diverging turn.
	Turn int `json:"turn"`
	// Expected is the output from the original execution.
	Expected string `json:"expected"`
	// Actual is the output produced during the replay.
	Actual string `json:"actual"`
}

// Option is a functional option for configuring an AgentClient.
type Option func(*AgentClient)

// WithHTTPClient sets a custom HTTP client used for all requests.
// If not provided, http.DefaultClient is used.
func WithHTTPClient(hc *http.Client) Option {
	return func(a *AgentClient) {
		a.httpClient = hc
	}
}

// AgentClient is a thin HTTP client for the OJS Agent API.
type AgentClient struct {
	baseURL    string
	httpClient interface{ Do(req *http.Request) (*http.Response, error) }
}

// NewAgentClient creates a new AgentClient pointed at the given base URL.
// The URL must be non-empty and use the http or https scheme.
func NewAgentClient(baseURL string, opts ...Option) (*AgentClient, error) {
	if baseURL == "" {
		return nil, errors.New("ojs: base URL must not be empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ojs: invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("ojs: unsupported scheme %q, must be http or https", u.Scheme)
	}

	c := &AgentClient{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Fork creates a new execution branch for the given job, diverging at the
// turn specified in opts.
func (c *AgentClient) Fork(ctx context.Context, jobID string, opts ForkOptions) (*ForkResult, error) {
	var res ForkResult
	path := fmt.Sprintf("/v1/agent/jobs/%s/fork", jobID)
	if err := c.doJSON(ctx, http.MethodPost, path, opts, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Merge combines two branches of the given job using the strategy specified in
// opts. The returned MergeResult may contain a non-empty Conflicts slice when
// automatic resolution was not possible for every turn.
func (c *AgentClient) Merge(ctx context.Context, jobID string, opts MergeOptions) (*MergeResult, error) {
	var res MergeResult
	path := fmt.Sprintf("/v1/agent/jobs/%s/merge", jobID)
	if err := c.doJSON(ctx, http.MethodPost, path, opts, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Pause requests that the agent stop execution after the current turn
// completes. The reason string is recorded for audit purposes.
func (c *AgentClient) Pause(ctx context.Context, jobID string, reason string) error {
	body := struct {
		Reason string `json:"reason"`
	}{Reason: reason}
	path := fmt.Sprintf("/v1/agent/jobs/%s/pause", jobID)
	return c.doJSON(ctx, http.MethodPost, path, body, nil)
}

// Resume instructs a paused agent to continue (or abort) execution based on
// the provided ResumeDecision.
func (c *AgentClient) Resume(ctx context.Context, jobID string, decision ResumeDecision) error {
	path := fmt.Sprintf("/v1/agent/jobs/%s/resume", jobID)
	return c.doJSON(ctx, http.MethodPost, path, decision, nil)
}

// Replay re-executes the given job deterministically starting from the turn
// specified in opts, optionally substituting mock providers for live ones.
func (c *AgentClient) Replay(ctx context.Context, jobID string, opts ReplayOptions) (*ReplayResult, error) {
	var res ReplayResult
	path := fmt.Sprintf("/v1/agent/jobs/%s/replay", jobID)
	if err := c.doJSON(ctx, http.MethodPost, path, opts, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// doJSON is an internal helper that marshals body to JSON, sends the request,
// and decodes the response into result. It maps well-known HTTP status codes
// to sentinel errors.
func (c *AgentClient) doJSON(ctx context.Context, method, path string, body, result any) error {
	var reqBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&reqBody).Encode(body); err != nil {
			return fmt.Errorf("ojs: failed to marshal request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &reqBody)
	if err != nil {
		return fmt.Errorf("ojs: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ojs: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrAgentNotFound
	case http.StatusConflict:
		return ErrBranchConflict
	case http.StatusUnprocessableEntity:
		return ErrAgentNotPaused
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ojs: unexpected status %d", resp.StatusCode)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("ojs: failed to decode response: %w", err)
		}
	}
	return nil
}
