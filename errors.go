package ojs

import (
	"errors"
	"fmt"
)

// Standard OJS error codes as defined in the OJS HTTP binding specification.
const (
	ErrCodeHandlerError     = "handler_error"
	ErrCodeTimeout          = "timeout"
	ErrCodeCancelled        = "cancelled"
	ErrCodeInvalidPayload   = "invalid_payload"
	ErrCodeInvalidRequest   = "invalid_request"
	ErrCodeNotFound         = "not_found"
	ErrCodeBackendError     = "backend_error"
	ErrCodeRateLimited      = "rate_limited"
	ErrCodeDuplicate        = "duplicate"
	ErrCodeQueuePaused      = "queue_paused"
	ErrCodeSchemaValidation = "schema_validation"
	ErrCodeUnsupported      = "unsupported"
)

// Sentinel errors for use with errors.Is.
var (
	ErrNotFound    = errors.New("ojs: resource not found")
	ErrDuplicate   = errors.New("ojs: duplicate job")
	ErrQueuePaused = errors.New("ojs: queue is paused")
	ErrRateLimited = errors.New("ojs: rate limit exceeded")
	ErrConflict    = errors.New("ojs: conflict")
	ErrBackend     = errors.New("ojs: backend error")
	ErrTimeout     = errors.New("ojs: timeout")
)

// Error represents a structured OJS API error.
// It supports errors.Is and errors.As for idiomatic Go error handling.
type Error struct {
	// Code is the machine-readable error code from the OJS standard vocabulary.
	Code string `json:"code"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// Retryable indicates whether the client should retry the request.
	Retryable bool `json:"retryable"`

	// Details contains additional structured error context.
	Details map[string]any `json:"details,omitempty"`

	// RequestID is the unique request identifier for correlation.
	RequestID string `json:"request_id,omitempty"`

	// HTTPStatus is the HTTP status code from the response.
	HTTPStatus int `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("ojs: %s: %s (request_id=%s)", e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("ojs: %s: %s", e.Code, e.Message)
}

// Is enables errors.Is matching against sentinel errors.
func (e *Error) Is(target error) bool {
	switch e.Code {
	case ErrCodeNotFound:
		return target == ErrNotFound
	case ErrCodeDuplicate:
		return target == ErrDuplicate
	case ErrCodeQueuePaused:
		return target == ErrQueuePaused
	case ErrCodeRateLimited:
		return target == ErrRateLimited
	case ErrCodeBackendError:
		return target == ErrBackend
	case ErrCodeTimeout:
		return target == ErrTimeout
	}
	if e.HTTPStatus == 409 {
		return target == ErrConflict
	}
	return false
}

// Unwrap returns the sentinel error corresponding to the error code.
func (e *Error) Unwrap() error {
	switch e.Code {
	case ErrCodeNotFound:
		return ErrNotFound
	case ErrCodeDuplicate:
		return ErrDuplicate
	case ErrCodeQueuePaused:
		return ErrQueuePaused
	case ErrCodeRateLimited:
		return ErrRateLimited
	case ErrCodeBackendError:
		return ErrBackend
	case ErrCodeTimeout:
		return ErrTimeout
	}
	if e.HTTPStatus == 409 {
		return ErrConflict
	}
	return nil
}

// IsRetryable returns true if the error indicates the operation can be retried.
func IsRetryable(err error) bool {
	var ojsErr *Error
	if errors.As(err, &ojsErr) {
		return ojsErr.Retryable
	}
	return false
}

// ErrorCode extracts the OJS error code from an error, if available.
func ErrorCode(err error) string {
	var ojsErr *Error
	if errors.As(err, &ojsErr) {
		return ojsErr.Code
	}
	return ""
}
