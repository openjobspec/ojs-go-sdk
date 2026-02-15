package ojs

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Queue represents an OJS queue.
type Queue struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// QueueStats represents detailed statistics for a queue.
type QueueStats struct {
	Queue              string  `json:"queue"`
	Status             string  `json:"status"`
	Available          int     `json:"available"`
	Active             int     `json:"active"`
	Scheduled          int     `json:"scheduled"`
	Retryable          int     `json:"retryable"`
	Discarded          int     `json:"discarded"`
	CompletedLastHour  int     `json:"completed_last_hour"`
	FailedLastHour     int     `json:"failed_last_hour"`
	AvgDurationMS      float64 `json:"avg_duration_ms"`
	AvgWaitMS          float64 `json:"avg_wait_ms"`
	ThroughputPerSec   float64 `json:"throughput_per_second"`
	ComputedAt         string  `json:"computed_at"`
}

// Pagination contains pagination metadata for list endpoints.
type Pagination struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// ListQueues returns the list of all known queues with pagination metadata.
func (c *Client) ListQueues(ctx context.Context) ([]Queue, *Pagination, error) {
	var resp struct {
		Queues     []Queue    `json:"queues"`
		Pagination Pagination `json:"pagination"`
	}
	if err := c.transport.get(ctx, basePath+"/queues", &resp); err != nil {
		return nil, nil, err
	}
	return resp.Queues, &resp.Pagination, nil
}

// GetQueueStats returns detailed statistics for a queue.
func (c *Client) GetQueueStats(ctx context.Context, name string) (*QueueStats, error) {
	var resp struct {
		Queue  string `json:"queue"`
		Status string `json:"status"`
		Stats  struct {
			Available         int     `json:"available"`
			Active            int     `json:"active"`
			Scheduled         int     `json:"scheduled"`
			Retryable         int     `json:"retryable"`
			Discarded         int     `json:"discarded"`
			CompletedLastHour int     `json:"completed_last_hour"`
			FailedLastHour    int     `json:"failed_last_hour"`
			AvgDurationMS     float64 `json:"avg_duration_ms"`
			AvgWaitMS         float64 `json:"avg_wait_ms"`
			ThroughputPerSec  float64 `json:"throughput_per_second"`
		} `json:"stats"`
		ComputedAt string `json:"computed_at"`
	}
	path := fmt.Sprintf("%s/queues/%s/stats", basePath, url.PathEscape(name))
	if err := c.transport.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &QueueStats{
		Queue:             resp.Queue,
		Status:            resp.Status,
		Available:         resp.Stats.Available,
		Active:            resp.Stats.Active,
		Scheduled:         resp.Stats.Scheduled,
		Retryable:         resp.Stats.Retryable,
		Discarded:         resp.Stats.Discarded,
		CompletedLastHour: resp.Stats.CompletedLastHour,
		FailedLastHour:    resp.Stats.FailedLastHour,
		AvgDurationMS:     resp.Stats.AvgDurationMS,
		AvgWaitMS:         resp.Stats.AvgWaitMS,
		ThroughputPerSec:  resp.Stats.ThroughputPerSec,
		ComputedAt:        resp.ComputedAt,
	}, nil
}

// PauseQueue pauses a queue, preventing workers from fetching new jobs.
func (c *Client) PauseQueue(ctx context.Context, name string) error {
	path := fmt.Sprintf("%s/queues/%s/pause", basePath, url.PathEscape(name))
	return c.transport.post(ctx, path, nil, nil)
}

// ResumeQueue resumes a paused queue.
func (c *Client) ResumeQueue(ctx context.Context, name string) error {
	path := fmt.Sprintf("%s/queues/%s/resume", basePath, url.PathEscape(name))
	return c.transport.post(ctx, path, nil, nil)
}
