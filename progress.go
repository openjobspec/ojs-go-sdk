package ojs

import (
	"context"
	"fmt"
)

// ProgressReport represents a progress update from a long-running job.
type ProgressReport struct {
	JobID      string         `json:"job_id"`
	Percentage int            `json:"percentage"`
	Message    string         `json:"message,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

// ReportProgress sends a progress update for the given job to the OJS server.
// Percentage must be between 0 and 100 inclusive.
func ReportProgress(ctx context.Context, t *transport, jobID string, pct int, message string, data map[string]any) error {
	if pct < 0 || pct > 100 {
		return fmt.Errorf("ojs: percentage must be between 0 and 100, got %d", pct)
	}
	if jobID == "" {
		return fmt.Errorf("ojs: job_id is required for progress reporting")
	}

	report := ProgressReport{
		JobID:      jobID,
		Percentage: pct,
		Message:    message,
		Data:       data,
	}
	return t.post(ctx, basePath+"/workers/progress", report, nil)
}

// ReportProgress sends a progress update for this job to the OJS server.
// This is a convenience method on JobContext that uses the job's ID and
// the worker's transport automatically.
func (jc JobContext) ReportProgress(pct int, message string) error {
	if jc.worker == nil {
		return fmt.Errorf("ojs: cannot report progress without a worker")
	}
	return ReportProgress(jc.ctx, jc.worker.transport, jc.Job.ID, pct, message, nil)
}

// ReportProgressWithData sends a progress update with additional data for this job.
func (jc JobContext) ReportProgressWithData(pct int, message string, data map[string]any) error {
	if jc.worker == nil {
		return fmt.Errorf("ojs: cannot report progress without a worker")
	}
	return ReportProgress(jc.ctx, jc.worker.transport, jc.Job.ID, pct, message, data)
}
