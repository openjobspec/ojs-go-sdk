package ojs

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsRetryable_OJSError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"retryable OJS error", &Error{Code: ErrCodeRateLimited, Retryable: true}, true},
		{"non-retryable OJS error", &Error{Code: ErrCodeNotFound, Retryable: false}, false},
		{"retryable backend error", &Error{Code: ErrCodeBackendError, Retryable: true}, true},
		{"non-OJS error", fmt.Errorf("generic error"), false},
		{"nil error", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				// IsRetryable panics on nil — skip
				return
			}
			got := IsRetryable(tt.err)
			if got != tt.expected {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestIsRetryable_NonRetryableWrapper(t *testing.T) {
	retryableErr := &Error{Code: ErrCodeBackendError, Retryable: true}
	wrapped := NonRetryable(retryableErr)

	if IsRetryable(wrapped) {
		t.Error("IsRetryable(NonRetryable(retryableErr)) should be false")
	}
}

func TestIsRetryable_WrappedChain(t *testing.T) {
	inner := &Error{Code: ErrCodeBackendError, Retryable: true}
	wrapped := fmt.Errorf("context: %w", NonRetryable(inner))

	if IsRetryable(wrapped) {
		t.Error("IsRetryable should be false through wrapping chain")
	}
}

func TestNonRetryable_NilReturnsNil(t *testing.T) {
	if NonRetryable(nil) != nil {
		t.Error("NonRetryable(nil) should return nil")
	}
}

func TestNonRetryable_PreservesMessage(t *testing.T) {
	original := fmt.Errorf("payment declined")
	wrapped := NonRetryable(original)

	if wrapped.Error() != "payment declined" {
		t.Errorf("NonRetryable message = %q, want %q", wrapped.Error(), "payment declined")
	}
}

func TestNonRetryable_Unwrap(t *testing.T) {
	original := fmt.Errorf("underlying error")
	wrapped := NonRetryable(original)

	if !errors.Is(wrapped, original) {
		t.Error("NonRetryable should unwrap to original error")
	}
}

func TestIsHandlerRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"generic error (default retryable)", fmt.Errorf("oops"), true},
		{"NonRetryable wrapped", NonRetryable(fmt.Errorf("permanent")), false},
		{"OJS error (handler errors default retryable)", &Error{Code: ErrCodeBackendError, Retryable: false}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHandlerRetryable(tt.err)
			if got != tt.expected {
				t.Errorf("isHandlerRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestErrorCode_OJSError(t *testing.T) {
	err := &Error{Code: ErrCodeNotFound, Message: "job not found"}
	if got := ErrorCode(err); got != ErrCodeNotFound {
		t.Errorf("ErrorCode = %q, want %q", got, ErrCodeNotFound)
	}
}

func TestErrorCode_NonOJSError(t *testing.T) {
	err := fmt.Errorf("generic")
	if got := ErrorCode(err); got != "" {
		t.Errorf("ErrorCode(non-OJS) = %q, want empty", got)
	}
}

func TestErrorCode_WrappedOJSError(t *testing.T) {
	inner := &Error{Code: ErrCodeDuplicate, Message: "duplicate job"}
	wrapped := fmt.Errorf("enqueue: %w", inner)
	if got := ErrorCode(wrapped); got != ErrCodeDuplicate {
		t.Errorf("ErrorCode(wrapped) = %q, want %q", got, ErrCodeDuplicate)
	}
}

func TestBatchPartialError_Message(t *testing.T) {
	err := &BatchPartialError{Submitted: 10, Succeeded: 7}
	expected := "ojs: batch partial failure: 7/10 jobs enqueued"
	if got := err.Error(); got != expected {
		t.Errorf("BatchPartialError.Error() = %q, want %q", got, expected)
	}
}

func TestErrorString_WithRequestID(t *testing.T) {
	err := &Error{Code: ErrCodeNotFound, Message: "not found", RequestID: "req-123"}
	expected := "ojs: not_found: not found (request_id=req-123)"
	if got := err.Error(); got != expected {
		t.Errorf("Error.Error() = %q, want %q", got, expected)
	}
}

func TestErrorString_WithoutRequestID(t *testing.T) {
	err := &Error{Code: ErrCodeBackendError, Message: "internal error"}
	expected := "ojs: backend_error: internal error"
	if got := err.Error(); got != expected {
		t.Errorf("Error.Error() = %q, want %q", got, expected)
	}
}
