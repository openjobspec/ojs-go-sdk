// Package main demonstrates basic OJS client usage.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func main() {
	// Create a client connected to an OJS server.
	client, err := ojs.NewClient("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Check server health.
	health, err := client.Health(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Server status: %s (backend: %s)\n", health.Status, health.Backend.Type)

	// Enqueue a single job.
	job, err := client.Enqueue(ctx, "email.send",
		ojs.Args{"to": "user@example.com", "subject": "Welcome"},
		ojs.WithQueue("email"),
		ojs.WithRetry(ojs.RetryPolicy{MaxAttempts: 5}),
		ojs.WithDelay(5*time.Minute),
		ojs.WithTags("onboarding", "email"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Enqueued job: %s (state: %s)\n", job.ID, job.State)

	// Get job details.
	job, err = client.GetJob(ctx, job.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Job state: %s\n", job.State)

	// Enqueue a batch of jobs.
	jobs, err := client.EnqueueBatch(ctx, []ojs.JobRequest{
		{Type: "email.send", Args: ojs.Args{"to": "a@example.com"}, Options: []ojs.EnqueueOption{ojs.WithQueue("email")}},
		{Type: "email.send", Args: ojs.Args{"to": "b@example.com"}, Options: []ojs.EnqueueOption{ojs.WithQueue("email")}},
		{Type: "email.send", Args: ojs.Args{"to": "c@example.com"}, Options: []ojs.EnqueueOption{ojs.WithQueue("email")}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Enqueued %d jobs\n", len(jobs))

	// Cancel a job.
	cancelled, err := client.CancelJob(ctx, job.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cancelled job: %s (previous state: %s)\n", cancelled.ID, cancelled.PreviousState)
}
