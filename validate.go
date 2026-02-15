package ojs

import (
	"fmt"
	"regexp"
)

var (
	typePattern  = regexp.MustCompile(`^[a-z][a-z0-9_\-]*(\.[a-z][a-z0-9_\-]*)*$`)
	queuePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-\.]*$`)
)

const (
	maxTypeLength  = 255
	maxQueueLength = 128
)

// validateEnqueueParams validates job parameters before sending to the server.
func validateEnqueueParams(jobType string, args []any) error {
	if jobType == "" {
		return fmt.Errorf("ojs: job type is required")
	}
	if len(jobType) > maxTypeLength {
		return fmt.Errorf("ojs: job type must not exceed %d characters, got %d", maxTypeLength, len(jobType))
	}
	if !typePattern.MatchString(jobType) {
		return fmt.Errorf("ojs: invalid job type %q: must match pattern ^[a-z][a-z0-9_-]*(\\.[a-z][a-z0-9_-]*)*$", jobType)
	}
	if args == nil {
		return fmt.Errorf("ojs: args must not be nil, use an empty slice instead")
	}
	return nil
}

// validateQueue validates a queue name.
func validateQueue(queue string) error {
	if queue == "" {
		return nil // empty means use default
	}
	if len(queue) > maxQueueLength {
		return fmt.Errorf("ojs: queue name must not exceed %d characters, got %d", maxQueueLength, len(queue))
	}
	if !queuePattern.MatchString(queue) {
		return fmt.Errorf("ojs: invalid queue name %q: must match pattern ^[a-z0-9][a-z0-9\\-\\.]*$", queue)
	}
	return nil
}
