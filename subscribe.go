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

func readSSEStream(ctx context.Context, resp *http.Response, handler EventHandler) {
	scanner := bufio.NewScanner(resp.Body)
	var eventType, data string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			if data != "" {
				evt := Event{
					Type:   eventType,
					Source: "sse",
					Time:   time.Now(),
					Data:   map[string]any{"raw": data},
				}
				handler(evt)
			}
			eventType = ""
			data = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}
