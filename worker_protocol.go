package ojs

import "context"

// This file owns the OJS worker-protocol HTTP binding: the shape of every
// worker request/response body and the endpoint each one is sent to. It changes
// when the OJS worker protocol changes, independently of the worker's polling,
// concurrency, and shutdown behaviour.
//
// See spec/spec/ojs-worker-protocol.md.

// --- Wire types ---

// fetchRequest is the body of POST /ojs/v1/workers/fetch.
type fetchRequest struct {
	Queues       []string            `json:"queues"`
	Count        int                 `json:"count"`
	WorkerID     string              `json:"worker_id"`
	Capabilities *WorkerCapabilities `json:"capabilities,omitempty"`
}

// fetchResponse is the body returned by the fetch endpoint.
type fetchResponse struct {
	Jobs []Job `json:"jobs"`
}

// ackRequest is the body of POST /ojs/v1/workers/ack.
type ackRequest struct {
	JobID  string         `json:"job_id"`
	Result map[string]any `json:"result,omitempty"`
}

// nackRequest is the body of POST /ojs/v1/workers/nack.
type nackRequest struct {
	JobID string       `json:"job_id"`
	Error nackErrorObj `json:"error"`
}

// nackErrorObj is the structured failure reported with a NACK.
type nackErrorObj struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// heartbeatRequest is the body of POST /ojs/v1/workers/heartbeat.
//
// worker_id, state, active_jobs and active_job_ids are REQUIRED by the worker
// protocol; labels and capabilities are optional and are omitted entirely when
// the corresponding worker options were not supplied, so the payload is
// unchanged for workers that do not configure them.
type heartbeatRequest struct {
	WorkerID     string              `json:"worker_id"`
	State        string              `json:"state"`
	ActiveJobs   int                 `json:"active_jobs"`
	ActiveJobIDs []string            `json:"active_job_ids"`
	Labels       []string            `json:"labels,omitempty"`
	Capabilities *WorkerCapabilities `json:"capabilities,omitempty"`
}

// heartbeatResponse carries the server-directed lifecycle state.
type heartbeatResponse struct {
	State string `json:"state"`
}

// --- Endpoint calls ---

// fetchJobs requests up to count jobs from the server.
func (w *Worker) fetchJobs(ctx context.Context, count int) ([]Job, error) {
	req := fetchRequest{
		Queues:       w.config.queues,
		Count:        count,
		WorkerID:     w.workerID,
		Capabilities: w.config.capabilities,
	}
	var resp fetchResponse
	if err := w.transport.post(ctx, basePath+"/workers/fetch", req, &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// ackJob acknowledges successful completion of a job.
func (w *Worker) ackJob(ctx context.Context, jobID string, result map[string]any) error {
	return w.transport.post(ctx, basePath+"/workers/ack", ackRequest{
		JobID:  jobID,
		Result: result,
	}, nil)
}

// nackJob reports job failure to the OJS server.
func (w *Worker) nackJob(ctx context.Context, jobID, code, message string, retryable bool) error {
	return w.transport.post(ctx, basePath+"/workers/nack", nackRequest{
		JobID: jobID,
		Error: nackErrorObj{Code: code, Message: message, Retryable: retryable},
	}, nil)
}

// sendHeartbeat sends a single heartbeat and applies any server-directed state.
func (w *Worker) sendHeartbeat(ctx context.Context) error {
	activeJobIDs := w.active.idsSnapshot()
	if activeJobIDs == nil {
		activeJobIDs = []string{}
	}

	req := heartbeatRequest{
		WorkerID:     w.workerID,
		State:        string(w.State()),
		ActiveJobs:   len(activeJobIDs),
		ActiveJobIDs: activeJobIDs,
		Labels:       w.config.labels,
		Capabilities: w.config.capabilities,
	}

	var resp heartbeatResponse
	if err := w.transport.post(ctx, basePath+"/workers/heartbeat", req, &resp); err != nil {
		// Heartbeat failures are non-fatal. Continue operating.
		return err
	}

	if resp.State != "" {
		w.lifecycle.applyServerDirective(WorkerState(resp.State))
	}
	return nil
}
