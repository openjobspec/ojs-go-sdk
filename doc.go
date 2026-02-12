// Package ojs provides a Go client and worker SDK for Open Job Spec (OJS),
// a vendor-neutral, language-agnostic specification for background job processing.
//
// The SDK has two primary components:
//
//   - [Client]: enqueue jobs, manage workflows, query queues, and interact with
//     dead letter and cron operations.
//   - [Worker]: fetch and process jobs with configurable concurrency, a composable
//     middleware chain, and graceful shutdown.
//
// # Quick Start
//
// Enqueue a job:
//
//	client, err := ojs.NewClient("http://localhost:8080")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	job, err := client.Enqueue(ctx, "email.send",
//	    ojs.Args{"to": "user@example.com"},
//	    ojs.WithQueue("email"),
//	)
//
// Process jobs:
//
//	worker := ojs.NewWorker("http://localhost:8080",
//	    ojs.WithQueues("default", "email"),
//	    ojs.WithConcurrency(10),
//	)
//	worker.Register("email.send", func(ctx ojs.JobContext) error {
//	    to := ctx.Job.Args["to"].(string)
//	    // process...
//	    return nil
//	})
//	worker.Start(ctx)
//
// This SDK has zero external dependencies -- it uses only the Go standard library.
package ojs
