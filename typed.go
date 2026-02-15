package ojs

import (
	"encoding/json"
	"fmt"
)

// TypedHandlerFunc is a handler that receives pre-parsed, typed arguments.
// Use with [RegisterTyped] to avoid manual type assertions on job args.
type TypedHandlerFunc[T any] func(ctx JobContext, args T) error

// RegisterTyped registers a typed handler for the given job type.
// Job args are automatically unmarshaled into T before the handler is called.
// If unmarshaling fails, the job is NACKed as a non-retryable error.
//
// Example:
//
//	type EmailArgs struct {
//	    To      string `json:"to"`
//	    Subject string `json:"subject"`
//	}
//
//	ojs.RegisterTyped(worker, "email.send", func(ctx ojs.JobContext, args EmailArgs) error {
//	    log.Printf("Sending to %s: %s", args.To, args.Subject)
//	    return nil
//	})
func RegisterTyped[T any](w *Worker, jobType string, handler TypedHandlerFunc[T]) {
	w.Register(jobType, func(ctx JobContext) error {
		var args T
		// Round-trip through JSON: Args (map[string]any) → JSON → T.
		data, err := json.Marshal(ctx.Job.Args)
		if err != nil {
			return NonRetryable(fmt.Errorf("ojs: marshal args for %s: %w", jobType, err))
		}
		if err := json.Unmarshal(data, &args); err != nil {
			return NonRetryable(fmt.Errorf("ojs: unmarshal args into %T for %s: %w", args, jobType, err))
		}
		return handler(ctx, args)
	})
}
