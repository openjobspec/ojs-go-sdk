// Package main demonstrates OJS worker usage with handlers and middleware.
package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func main() {
	// Create a worker connected to an OJS server.
	worker := ojs.NewWorker("http://localhost:8080",
		ojs.WithQueues("email", "default"),
		ojs.WithConcurrency(10),
		ojs.WithGracePeriod(25*time.Second),
	)

	// Register job handlers.
	worker.Register("email.send", func(ctx ojs.JobContext) error {
		to, _ := ctx.Job.Args["to"].(string)
		subject, _ := ctx.Job.Args["subject"].(string)

		log.Printf("Sending email to %s: %s", to, subject)

		// Simulate sending an email.
		time.Sleep(100 * time.Millisecond)

		// Set the result.
		ctx.SetResult(map[string]any{
			"messageId": fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"delivered": true,
		})
		return nil
	})

	worker.Register("report.generate", func(ctx ojs.JobContext) error {
		reportType, _ := ctx.Job.Args["type"].(string)
		log.Printf("Generating %s report (attempt %d)", reportType, ctx.Attempt)

		// For long-running jobs, send heartbeats to extend visibility timeout.
		for i := 0; i < 5; i++ {
			time.Sleep(500 * time.Millisecond)
			if err := ctx.Heartbeat(); err != nil {
				log.Printf("Heartbeat error: %v", err)
			}
		}

		ctx.SetResult(map[string]any{"path": "/reports/daily.csv"})
		return nil
	})

	// Add logging middleware.
	worker.Use(func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		log.Printf("[START] %s (id=%s, attempt=%d)", ctx.Job.Type, ctx.Job.ID, ctx.Attempt)
		start := time.Now()
		err := next(ctx)
		duration := time.Since(start)
		if err != nil {
			log.Printf("[FAIL]  %s (id=%s, duration=%s, error=%s)", ctx.Job.Type, ctx.Job.ID, duration, err)
		} else {
			log.Printf("[DONE]  %s (id=%s, duration=%s)", ctx.Job.Type, ctx.Job.ID, duration)
		}
		return err
	})

	// Add error recovery middleware.
	worker.Use(func(ctx ojs.JobContext, next ojs.HandlerFunc) error {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] %s (id=%s): %v", ctx.Job.Type, ctx.Job.ID, r)
			}
		}()
		return next(ctx)
	})

	// Graceful shutdown via context cancellation.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	log.Println("Starting worker...")
	if err := worker.Start(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("Worker stopped.")
}
