package ojs

import "time"

// Event type constants as defined in the OJS Events specification.
const (
	// Core job events (REQUIRED).
	EventJobEnqueued  = "job.enqueued"
	EventJobStarted   = "job.started"
	EventJobCompleted = "job.completed"
	EventJobFailed    = "job.failed"
	EventJobDiscarded = "job.discarded"

	// Extended job events.
	EventJobRetrying  = "job.retrying"
	EventJobCancelled = "job.cancelled"
	EventJobScheduled = "job.scheduled"
	EventJobExpired   = "job.expired"
	EventJobProgress  = "job.progress"

	// Worker events.
	EventWorkerStarted   = "worker.started"
	EventWorkerStopped   = "worker.stopped"
	EventWorkerQuiet     = "worker.quiet"
	EventWorkerHeartbeat = "worker.heartbeat"

	// Workflow events.
	EventWorkflowStarted       = "workflow.started"
	EventWorkflowStepCompleted = "workflow.step_completed"
	EventWorkflowCompleted     = "workflow.completed"
	EventWorkflowFailed        = "workflow.failed"

	// Cron events.
	EventCronTriggered = "cron.triggered"
	EventCronSkipped   = "cron.skipped"

	// Queue events.
	EventQueuePaused  = "queue.paused"
	EventQueueResumed = "queue.resumed"
)

// Event represents an OJS event following the CloudEvents-inspired envelope.
type Event struct {
	// Type is the event type (e.g., "job.completed").
	Type string `json:"type"`

	// Source identifies the context in which the event happened.
	Source string `json:"source"`

	// Subject is the primary subject of the event (typically a job ID).
	Subject string `json:"subject"`

	// Time is when the event occurred.
	Time time.Time `json:"time"`

	// Data contains event-specific payload.
	Data map[string]any `json:"data,omitempty"`
}

// EventHandler is a function that handles an OJS event.
type EventHandler func(Event)
