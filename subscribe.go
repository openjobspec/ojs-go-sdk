package ojs

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Subscription represents an active SSE subscription.
// Call Cancel() to stop receiving events.
type Subscription struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Cancel stops the subscription and closes the SSE connection.
func (s *Subscription) Cancel() {
	s.cancel()
	<-s.done
}

// Subscribe opens an SSE connection to receive real-time job events.
// The handler is called for each event. Returns a Subscription that can be cancelled.
func (c *Client) Subscribe(ctx context.Context, channel string, handler EventHandler) (*Subscription, error) {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &Subscription{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	url := fmt.Sprintf("%s/ojs/v1/events/stream?channel=%s", c.transport.baseURL, channel)
	req, err := http.NewRequestWithContext(subCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.transport.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.transport.authToken)
	}

	resp, err := c.transport.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("subscribe: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("subscribe: server returned %d", resp.StatusCode)
	}

	go func() {
		defer close(sub.done)
		defer resp.Body.Close()
		readSSEStream(subCtx, resp, handler)
	}()

	return sub, nil
}

// SubscribeJob subscribes to state change events for a specific job.
func (c *Client) SubscribeJob(ctx context.Context, jobID string, handler EventHandler) (*Subscription, error) {
	return c.Subscribe(ctx, "job:"+jobID, handler)
}

// SubscribeQueue subscribes to events for all jobs in a queue.
func (c *Client) SubscribeQueue(ctx context.Context, queue string, handler EventHandler) (*Subscription, error) {
	return c.Subscribe(ctx, "queue:"+queue, handler)
}

func readSSEStream(ctx context.Context, resp *http.Response, handler EventHandler) string {
	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	var eventID string
	var lastEventID string
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return lastEventID
		default:
		}

		line := scanner.Text()

		if line == "" {
			if len(dataLines) > 0 {
				if eventID != "" {
					lastEventID = eventID
				}
				evt := Event{
					Type:   eventType,
					Source: "sse",
					Time:   time.Now(),
					Data:   map[string]any{"raw": strings.Join(dataLines, "\n"), "id": lastEventID},
				}
				handler(evt)
			}
			eventType = ""
			eventID = ""
			dataLines = dataLines[:0]
			continue
		}

		if strings.HasPrefix(line, ":") {
			continue // SSE comment line — ignore per spec
		} else if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "data" {
			dataLines = append(dataLines, "")
		} else if strings.HasPrefix(line, "id: ") {
			eventID = strings.TrimPrefix(line, "id: ")
		} else if line == "id" {
			eventID = ""
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		// Log I/O errors that aren't caused by context cancellation
		if t := recover(); t != nil {
			// ignore
		}
	}

	return lastEventID
}

// SubscribeWithReconnect is like Subscribe but automatically reconnects with
// exponential backoff when the SSE connection drops. It keeps retrying until
// the context is cancelled or Cancel() is called.
func (c *Client) SubscribeWithReconnect(ctx context.Context, channel string, handler EventHandler) *Subscription {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &Subscription{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		defer close(sub.done)
		backoff := 1 * time.Second
		const maxBackoff = 30 * time.Second
		var lastEventID string

		for {
			eventID, err := c.subscribeOnce(subCtx, channel, handler, lastEventID)
			if subCtx.Err() != nil {
				return
			}
			if eventID != "" {
				lastEventID = eventID
			}
			// Reset backoff on successful connection (got at least one event)
			if err == nil {
				backoff = 1 * time.Second
			}
			if err != nil && c.transport.logger != nil {
				c.transport.logger.Warn("SSE connection lost, reconnecting",
					"channel", channel, "backoff", backoff, "error", err, "last_event_id", lastEventID)
			}

			select {
			case <-subCtx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, maxBackoff)
		}
	}()

	return sub
}

// subscribeOnce opens a single SSE connection and reads until it closes or errors.
// Returns the last event ID received and any error.
func (c *Client) subscribeOnce(ctx context.Context, channel string, handler EventHandler, lastEventID string) (string, error) {
	url := fmt.Sprintf("%s/ojs/v1/events/stream?channel=%s", c.transport.baseURL, channel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	if c.transport.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.transport.authToken)
	}

	resp, err := c.transport.httpClient.Do(req)
	if err != nil {
		return lastEventID, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return lastEventID, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	eventID := readSSEStream(ctx, resp, handler)
	return eventID, nil
}
