package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// This file owns the OJS push-delivery HTTP binding: the request/response
// bodies, the validation rules, and the status codes used when an OJS server
// POSTs a job to a serverless function.
//
// The Lambda, Vercel, and Cloudflare entry points previously each carried their
// own copy of this sequence, so a binding change had to be made in three places
// and could silently drift. They now share this one implementation and supply
// only what genuinely differs per platform: how the job is read out of the body
// and what request-scoped context the platform contributes.

// PushDeliveryRequest is the HTTP body sent by an OJS server for push delivery.
type PushDeliveryRequest struct {
	Job        JobEvent `json:"job"`
	WorkerID   string   `json:"worker_id"`
	DeliveryID string   `json:"delivery_id"`
}

// PushDeliveryResponse is the HTTP response body for push delivery.
type PushDeliveryResponse struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *PushError      `json:"error,omitempty"`
}

// PushError describes a job processing failure.
type PushError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Push delivery response status values.
const (
	pushStatusCompleted = "completed"
	pushStatusFailed    = "failed"
)

// Push delivery error codes.
const (
	pushCodeInvalidRequest            = "invalid_request"
	pushCodeHandlerError              = "handler_error"
	pushCodeAuthenticationFailed      = "authentication_failed"
	pushCodeAuthenticationUnavailable = "authentication_unavailable"
)

// pushBinding describes the platform-specific parts of a push delivery.
type pushBinding struct {
	// decode extracts the job from the (size-limited) request body.
	decode func(io.Reader) (JobEvent, error)

	// decodeErrMsg is reported when decode fails.
	decodeErrMsg string

	// requestContext derives the handler context from the request. Optional.
	requestContext func(*http.Request) context.Context
}

// servePush runs one push delivery end to end: method check, size-limited body
// read, authentication, decode, job validation, execution, and response mapping.
func (h *LambdaHandler) servePush(w http.ResponseWriter, r *http.Request, b pushBinding) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body := http.MaxBytesReader(w, r.Body, h.maxBodySize)
	rawBody, err := io.ReadAll(body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePushError(
				w,
				http.StatusRequestEntityTooLarge,
				pushCodeInvalidRequest,
				"request body exceeds the configured size limit",
				false,
			)
			return
		}
		writePushError(w, http.StatusBadRequest, pushCodeInvalidRequest, b.decodeErrMsg, false)
		return
	}

	timestampValues := r.Header.Values(PushTimestampHeader)
	if len(timestampValues) > 1 {
		writePushError(
			w,
			http.StatusUnauthorized,
			pushCodeAuthenticationFailed,
			"invalid push authentication",
			false,
		)
		return
	}
	timestampHeader := ""
	if len(timestampValues) == 1 {
		timestampHeader = timestampValues[0]
	}
	if err := h.authenticatePush(
		timestampHeader,
		r.Header.Values(PushSignatureHeader),
		rawBody,
		time.Now(),
	); err != nil {
		h.writePushAuthenticationError(w, err)
		return
	}

	job, err := b.decode(bytes.NewReader(rawBody))
	if err != nil {
		writePushError(w, http.StatusBadRequest, pushCodeInvalidRequest, b.decodeErrMsg, false)
		return
	}

	if job.ID == "" || job.Type == "" {
		writePushError(w, http.StatusBadRequest, pushCodeInvalidRequest, "job id and type are required", false)
		return
	}

	ctx := r.Context()
	if b.requestContext != nil {
		ctx = b.requestContext(r)
	}

	if err := h.processJob(ctx, job); err != nil {
		h.logger.Error("job processing failed",
			"job_id", job.ID,
			"job_type", job.Type,
			"error", err,
		)
		writePushError(w, http.StatusOK, pushCodeHandlerError, err.Error(), true)
		return
	}

	h.logger.Info("job completed",
		"job_id", job.ID,
		"job_type", job.Type,
	)
	writeJSON(w, http.StatusOK, PushDeliveryResponse{Status: pushStatusCompleted})
}

func (h *LambdaHandler) writePushAuthenticationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPushAuthNotConfigured), errors.Is(err, errPushAuthInvalidConfig):
		h.logger.Error("push authentication unavailable", "error", err)
		writePushError(
			w,
			http.StatusServiceUnavailable,
			pushCodeAuthenticationUnavailable,
			"push authentication is unavailable",
			false,
		)
	case errors.Is(err, errPushAuthHeaderTooLarge):
		writePushError(
			w,
			http.StatusRequestHeaderFieldsTooLarge,
			pushCodeAuthenticationFailed,
			"invalid push authentication",
			false,
		)
	default:
		writePushError(
			w,
			http.StatusUnauthorized,
			pushCodeAuthenticationFailed,
			"invalid push authentication",
			false,
		)
	}
}

// decodePushDelivery reads a PushDeliveryRequest envelope and returns its job.
func decodePushDelivery(r io.Reader) (JobEvent, error) {
	var req PushDeliveryRequest
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return JobEvent{}, err
	}
	return req.Job, nil
}

// decodeBareJob reads a raw JobEvent with no envelope.
func decodeBareJob(r io.Reader) (JobEvent, error) {
	var job JobEvent
	if err := json.NewDecoder(r).Decode(&job); err != nil {
		return JobEvent{}, err
	}
	return job, nil
}

func writePushError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, PushDeliveryResponse{
		Status: pushStatusFailed,
		Error: &PushError{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
