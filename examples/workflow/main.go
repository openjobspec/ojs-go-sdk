// Package main demonstrates OJS workflow usage.
package main

import (
	"context"
	"fmt"
	"log"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func main() {
	client, err := ojs.NewClient("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// --- Chain: Sequential workflow ---
	// Steps execute one after another: fetch -> transform -> notify.
	chain, err := client.CreateWorkflow(ctx, ojs.Chain(
		ojs.Step{Type: "data.fetch", Args: ojs.Args{"url": "https://api.example.com/data"}},
		ojs.Step{Type: "data.transform", Args: ojs.Args{"format": "csv"}},
		ojs.Step{Type: "notification.send", Args: ojs.Args{"channel": "slack"}},
	))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Chain workflow: %s (state: %s, steps: %d)\n", chain.ID, chain.State, len(chain.Steps))

	// --- Group: Parallel workflow ---
	// All jobs execute concurrently.
	group, err := client.CreateWorkflow(ctx, ojs.Group(
		ojs.Step{Type: "export.csv", Args: ojs.Args{"report_id": "rpt_456"}},
		ojs.Step{Type: "export.pdf", Args: ojs.Args{"report_id": "rpt_456"}},
		ojs.Step{Type: "export.xlsx", Args: ojs.Args{"report_id": "rpt_456"}},
	))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Group workflow: %s (state: %s, jobs: %d)\n", group.ID, group.State, len(group.Steps))

	// --- Batch: Parallel with callbacks ---
	// Jobs execute concurrently, callbacks fire based on collective outcome.
	batch, err := client.CreateWorkflow(ctx, ojs.Batch(
		ojs.BatchCallbacks{
			OnComplete: &ojs.Step{Type: "batch.report", Args: ojs.Args{}},
			OnSuccess:  &ojs.Step{Type: "batch.celebrate", Args: ojs.Args{}},
			OnFailure:  &ojs.Step{Type: "batch.alert", Args: ojs.Args{"channel": "pagerduty"}},
		},
		ojs.Step{Type: "email.send", Args: ojs.Args{"to": "user1@example.com"}},
		ojs.Step{Type: "email.send", Args: ojs.Args{"to": "user2@example.com"}},
		ojs.Step{Type: "email.send", Args: ojs.Args{"to": "user3@example.com"}},
	))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Batch workflow: %s (state: %s, steps: %d)\n", batch.ID, batch.State, len(batch.Steps))

	// --- Check workflow status ---
	status, err := client.GetWorkflow(ctx, chain.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Chain status: %s\n", status.State)
	for _, step := range status.Steps {
		fmt.Printf("  Step %s (%s): %s\n", step.ID, step.Type, step.State)
	}
}
