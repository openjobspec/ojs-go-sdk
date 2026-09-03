package ojs

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
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

	url := c.eventStreamURL(channel)
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

	// The response body outlives this call: the reader goroutine below owns it
	// for the lifetime of the subscription. Until that hand-off happens this
	// function still owns it, so every path that returns without starting the
	// goroutine must close it — otherwise a rejected subscribe leaks the
	// connection for the whole idle timeout.
	streaming := false
	defer func() {
		if !streaming {
			resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		cancel()
		return nil, fmt.Errorf("subscribe: server returned %d", resp.StatusCode)
	}

	streaming = true
	go func() {
		defer close(sub.done)
		defer resp.Body.Close()
		if _, err := readSSEStream(subCtx, resp, handler); err != nil && subCtx.Err() == nil && c.transport.logger != nil {
			c.transport.logger.Warn("SSE stream ended with error", "channel", channel, "error", err)
		}
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

// eventStreamURL builds the SSE endpoint URL for a channel.
// The channel is query-escaped: OJS channels contain ":" and may contain user
// supplied queue or job identifiers, which previously corrupted the query.
func (c *Client) eventStreamURL(channel string) string {
	return fmt.Sprintf("%s%s/events/stream?channel=%s", c.transport.baseURL, basePath, url.QueryEscape(channel))
}

// maxSSELineBytes bounds a single SSE line. bufio.Scanner's 64 KiB default
// silently ended the stream on larger payloads.
const maxSSELineBytes = 1 << 20

// sseDispatcher accumulates SSE field lines into events and emits them.
//
// It owns the SSE framing rules (field parsing, the optional single space after
// the colon, event dispatch on a blank line, last-event-id tracking) separately
// from the connection and reconnect handling in this file.
type sseDispatcher struct {
	handler     EventHandler
	eventType   string
	eventID     string
	lastEventID string
	dataLines   []string
}

// line feeds a single decoded SSE line to the dispatcher.
func (d *sseDispatcher) line(line string) {
	if line == "" {
		d.dispatch()
		return
	}
	if strings.HasPrefix(line, ":") {
		return // comment line — ignored per the SSE specification
	}

	field, value := splitSSEField(line)
	switch field {
	case "event":
		d.eventType = value
	case "data":
		d.dataLines = append(d.dataLines, value)
	case "id":
		d.eventID = value
	}
}

// dispatch emits the buffered event, if any, and resets the field buffers.
func (d *sseDispatcher) dispatch() {
	if len(d.dataLines) == 0 {
		d.eventType = ""
		d.eventID = ""
		return
	}
	if d.eventID != "" {
		d.lastEventID = d.eventID
	}
	evt := Event{
		Type:   d.eventType,
		Source: "sse",
		Time:   time.Now(),
		Data:   map[string]any{"raw": strings.Join(d.dataLines, "\n"), "id": d.lastEventID},
	}
	if d.handler != nil {
		d.handler(evt)
	}
	d.eventType = ""
	d.eventID = ""
	d.dataLines = d.dataLines[:0]
}

// splitSSEField splits an SSE line into its field name and value, stripping the
// single optional space after the colon. A line with no colon is a field name
// with an empty value.
func splitSSEField(line string) (field, value string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}
	field = line[:idx]
	value = line[idx+1:]
	value = strings.TrimPrefix(value, " ")
	return field, value
}

// readSSEStream reads events until the stream ends, the context is cancelled,
// or an I/O error occurs. It returns the last event ID seen and the terminating
// error, if any.
//
// The error is returned rather than discarded: it previously fell into an
// unreachable recover() block, so a truncated or oversized stream looked like a
// clean end-of-stream and silently reset the reconnect backoff.
func readSSEStream(ctx context.Context, resp *http.Response, handler EventHandler) (string, error) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)

	d := &sseDispatcher{handler: handler}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return d.lastEventID, ctx.Err()
		default:
		}
		d.line(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return d.lastEventID, ctx.Err()
		}
		return d.lastEventID, fmt.Errorf("ojs: sse stream read: %w", err)
	}
	return d.lastEventID, nil
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
	url := c.eventStreamURL(channel)
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

	return readSSEStream(ctx, resp, handler)
}
