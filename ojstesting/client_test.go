package ojstesting_test

import (
	"context"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
	"github.com/openjobspec/ojs-go-sdk/ojstesting"
)

func TestFakeClientEnqueue(t *testing.T) {
	_ = ojstesting.Fake(t)
	client := ojstesting.FakeClient(t)

	job, err := client.Enqueue(context.Background(), "email.send",
		ojs.Args{"to": "user@example.com"},
		ojs.WithQueue("email"),
	)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if job.ID == "" {
		t.Error("expected non-empty job ID")
	}
	if job.Type != "email.send" {
		t.Errorf("expected type=email.send, got %s", job.Type)
	}

	ojstesting.AssertEnqueued(t, "email.send")
	ojstesting.AssertEnqueued(t, "email.send",
		ojstesting.MatchQueue("email"),
	)
}

func TestFakeClientEnqueueBatch(t *testing.T) {
	_ = ojstesting.Fake(t)
	client := ojstesting.FakeClient(t)

	jobs, err := client.EnqueueBatch(context.Background(), []ojs.JobRequest{
		{Type: "email.send", Args: ojs.Args{"to": "a@example.com"}},
		{Type: "email.send", Args: ojs.Args{"to": "b@example.com"}},
		{Type: "sms.send", Args: ojs.Args{"to": "+1234567890"}},
	})
	if err != nil {
		t.Fatalf("EnqueueBatch() error = %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(jobs))
	}

	ojstesting.AssertEnqueued(t, "email.send", ojstesting.MatchCount(2))
	ojstesting.AssertEnqueued(t, "sms.send", ojstesting.MatchCount(1))
	ojstesting.RefuteEnqueued(t, "push.send")
}

func TestFakeClientMultipleEnqueues(t *testing.T) {
	_ = ojstesting.Fake(t)
	client := ojstesting.FakeClient(t)

	_, err := client.Enqueue(context.Background(), "email.send",
		ojs.Args{"to": "first@example.com"},
	)
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}

	_, err = client.Enqueue(context.Background(), "email.send",
		ojs.Args{"to": "second@example.com"},
	)
	if err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}

	ojstesting.AssertEnqueued(t, "email.send", ojstesting.MatchCount(2))

	all := ojstesting.AllEnqueued("email.send")
	if len(all) != 2 {
		t.Fatalf("expected 2 enqueued, got %d", len(all))
	}
}

func TestFakeClientWithOptions(t *testing.T) {
	_ = ojstesting.Fake(t)
	client := ojstesting.FakeClient(t,
		ojs.WithAuthToken("test-token"),
		ojs.WithHeader("X-Custom", "value"),
	)

	_, err := client.Enqueue(context.Background(), "test.job", ojs.Args{"key": "val"})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	ojstesting.AssertEnqueued(t, "test.job")
}

func TestFakeClientClearAll(t *testing.T) {
	_ = ojstesting.Fake(t)
	client := ojstesting.FakeClient(t)

	_, _ = client.Enqueue(context.Background(), "email.send", ojs.Args{"to": "test@example.com"})
	ojstesting.AssertEnqueued(t, "email.send")

	ojstesting.ClearAll(t)
	ojstesting.RefuteEnqueued(t, "email.send")
}
