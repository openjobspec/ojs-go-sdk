package serverless

import (
	"context"
	"encoding/json"
)

// This file owns the AWS SQS Lambda event contract: the shape of the event and
// the partial-batch-failure response. It changes when AWS changes that
// contract, independently of the OJS push-delivery binding in push.go.

// SQSEvent represents an AWS SQS event containing one or more messages.
type SQSEvent struct {
	Records []SQSMessage `json:"Records"`
}

// SQSMessage represents a single SQS message containing an OJS job.
type SQSMessage struct {
	MessageID     string            `json:"messageId"`
	Body          string            `json:"body"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	MD5OfBody     string            `json:"md5OfBody,omitempty"`
	EventSourceID string            `json:"eventSource,omitempty"`
	ReceiptHandle string            `json:"receiptHandle,omitempty"`
}

// SQSBatchResponse is the response format for SQS batch item failures.
// Returning failed message IDs tells SQS to retry only those messages.
type SQSBatchResponse struct {
	BatchItemFailures []BatchItemFailure `json:"batchItemFailures"`
}

// BatchItemFailure identifies a single failed message in an SQS batch.
type BatchItemFailure struct {
	ItemIdentifier string `json:"itemIdentifier"`
}

// HandleSQS processes an SQS event containing OJS jobs.
// It returns partial batch failures so SQS only retries failed messages.
func (h *LambdaHandler) HandleSQS(ctx context.Context, event SQSEvent) (SQSBatchResponse, error) {
	if len(event.Records) == 0 {
		return SQSBatchResponse{}, nil
	}

	var failures []BatchItemFailure

	for _, record := range event.Records {
		var job JobEvent
		if err := json.Unmarshal([]byte(record.Body), &job); err != nil {
			h.logger.Error("failed to unmarshal SQS message",
				"message_id", record.MessageID,
				"error", err,
			)
			failures = append(failures, BatchItemFailure{ItemIdentifier: record.MessageID})
			continue
		}

		if err := h.processJob(ctx, job); err != nil {
			h.logger.Error("job processing failed",
				"job_id", job.ID,
				"job_type", job.Type,
				"error", err,
			)
			failures = append(failures, BatchItemFailure{ItemIdentifier: record.MessageID})
			continue
		}

		h.logger.Info("job completed",
			"job_id", job.ID,
			"job_type", job.Type,
		)
	}

	return SQSBatchResponse{BatchItemFailures: failures}, nil
}
