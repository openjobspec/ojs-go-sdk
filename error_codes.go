package ojs

// Standardized OJS error codes as defined in the OJS SDK Error Catalog
// (spec/ojs-error-catalog.md). Each code maps to a canonical wire-format
// string code from the OJS Error Specification.

// ErrorCodeEntry describes a single entry in the OJS error catalog.
type ErrorCodeEntry struct {
	// Code is the OJS-XXXX numeric identifier (e.g., "OJS-1000").
	Code string
	// Name is the human-readable error name (e.g., "InvalidPayload").
	Name string
	// CanonicalCode is the SCREAMING_SNAKE_CASE wire-format code (e.g., "INVALID_PAYLOAD").
	CanonicalCode string
	// HTTPStatus is the default HTTP status code, or 0 for client-side errors.
	HTTPStatus int
	// Message is the default human-readable description.
	Message string
	// Retryable indicates the default retryability.
	Retryable bool
}

// OJS-1xxx: Client Errors
var (
	CodeInvalidPayload         = ErrorCodeEntry{"OJS-1000", "InvalidPayload", "INVALID_PAYLOAD", 400, "Job envelope fails structural validation", false}
	CodeInvalidJobType         = ErrorCodeEntry{"OJS-1001", "InvalidJobType", "INVALID_JOB_TYPE", 400, "Job type is not registered or does not match the allowlist", false}
	CodeInvalidQueue           = ErrorCodeEntry{"OJS-1002", "InvalidQueue", "INVALID_QUEUE", 400, "Queue name is invalid or does not match naming rules", false}
	CodeInvalidArgs            = ErrorCodeEntry{"OJS-1003", "InvalidArgs", "INVALID_ARGS", 400, "Job args fail type checking or schema validation", false}
	CodeInvalidMetadata        = ErrorCodeEntry{"OJS-1004", "InvalidMetadata", "INVALID_METADATA", 400, "Metadata field is malformed or exceeds the 64 KB size limit", false}
	CodeInvalidStateTransition = ErrorCodeEntry{"OJS-1005", "InvalidStateTransition", "INVALID_STATE_TRANSITION", 409, "Attempted an invalid lifecycle state change", false}
	CodeInvalidRetryPolicy     = ErrorCodeEntry{"OJS-1006", "InvalidRetryPolicy", "INVALID_RETRY_POLICY", 400, "Retry policy configuration is invalid", false}
	CodeInvalidCronExpression  = ErrorCodeEntry{"OJS-1007", "InvalidCronExpression", "INVALID_CRON_EXPRESSION", 400, "Cron expression syntax cannot be parsed", false}
	CodeSchemaValidationFailed = ErrorCodeEntry{"OJS-1008", "SchemaValidationFailed", "SCHEMA_VALIDATION_FAILED", 422, "Job args do not conform to the registered schema", false}
	CodePayloadTooLarge        = ErrorCodeEntry{"OJS-1009", "PayloadTooLarge", "PAYLOAD_TOO_LARGE", 413, "Job envelope exceeds the server's maximum payload size", false}
	CodeMetadataTooLarge       = ErrorCodeEntry{"OJS-1010", "MetadataTooLarge", "METADATA_TOO_LARGE", 413, "Metadata field exceeds the 64 KB limit", false}
	CodeConnectionError        = ErrorCodeEntry{"OJS-1011", "ConnectionError", "", 0, "Could not establish a connection to the OJS server", true}
	CodeRequestTimeout         = ErrorCodeEntry{"OJS-1012", "RequestTimeout", "", 0, "HTTP request to the OJS server timed out", true}
	CodeSerializationError     = ErrorCodeEntry{"OJS-1013", "SerializationError", "", 0, "Failed to serialize the request or deserialize the response", false}
	CodeQueueNameTooLong       = ErrorCodeEntry{"OJS-1014", "QueueNameTooLong", "QUEUE_NAME_TOO_LONG", 400, "Queue name exceeds the 255-byte maximum length", false}
	CodeJobTypeTooLong         = ErrorCodeEntry{"OJS-1015", "JobTypeTooLong", "JOB_TYPE_TOO_LONG", 400, "Job type exceeds the 255-byte maximum length", false}
	CodeChecksumMismatch       = ErrorCodeEntry{"OJS-1016", "ChecksumMismatch", "CHECKSUM_MISMATCH", 400, "External payload reference checksum verification failed", false}
	CodeUnsupportedCompression = ErrorCodeEntry{"OJS-1017", "UnsupportedCompression", "UNSUPPORTED_COMPRESSION", 400, "The specified compression codec is not supported", false}
)

// OJS-2xxx: Server Errors
var (
	CodeBackendError        = ErrorCodeEntry{"OJS-2000", "BackendError", "BACKEND_ERROR", 500, "Internal backend storage or transport failure", true}
	CodeBackendUnavailable  = ErrorCodeEntry{"OJS-2001", "BackendUnavailable", "BACKEND_UNAVAILABLE", 503, "Backend storage system is unreachable", true}
	CodeBackendTimeout      = ErrorCodeEntry{"OJS-2002", "BackendTimeout", "BACKEND_TIMEOUT", 504, "Backend operation timed out", true}
	CodeReplicationLag      = ErrorCodeEntry{"OJS-2003", "ReplicationLag", "REPLICATION_LAG", 500, "Operation failed due to replication consistency issue", true}
	CodeInternalServerError = ErrorCodeEntry{"OJS-2004", "InternalServerError", "", 500, "Unclassified internal server error", true}
)

// OJS-3xxx: Job Lifecycle Errors
var (
	CodeJobNotFound         = ErrorCodeEntry{"OJS-3000", "JobNotFound", "NOT_FOUND", 404, "The requested job, queue, or resource does not exist", false}
	CodeDuplicateJob        = ErrorCodeEntry{"OJS-3001", "DuplicateJob", "DUPLICATE_JOB", 409, "Unique job constraint was violated", false}
	CodeJobAlreadyCompleted = ErrorCodeEntry{"OJS-3002", "JobAlreadyCompleted", "JOB_ALREADY_COMPLETED", 409, "Operation attempted on a job that has already completed", false}
	CodeJobAlreadyCancelled = ErrorCodeEntry{"OJS-3003", "JobAlreadyCancelled", "JOB_ALREADY_CANCELLED", 409, "Operation attempted on a job that has already been cancelled", false}
	CodeQueuePaused         = ErrorCodeEntry{"OJS-3004", "QueuePaused", "QUEUE_PAUSED", 422, "The target queue is paused and not accepting new jobs", true}
	CodeHandlerError        = ErrorCodeEntry{"OJS-3005", "HandlerError", "HANDLER_ERROR", 0, "Job handler threw an exception during execution", true}
	CodeHandlerTimeout      = ErrorCodeEntry{"OJS-3006", "HandlerTimeout", "HANDLER_TIMEOUT", 0, "Job handler exceeded the configured execution timeout", true}
	CodeHandlerPanic        = ErrorCodeEntry{"OJS-3007", "HandlerPanic", "HANDLER_PANIC", 0, "Job handler caused an unrecoverable error", true}
	CodeNonRetryableError   = ErrorCodeEntry{"OJS-3008", "NonRetryableError", "NON_RETRYABLE_ERROR", 0, "Error type matched non_retryable_errors in the retry policy", false}
	CodeJobCancelled        = ErrorCodeEntry{"OJS-3009", "JobCancelled", "JOB_CANCELLED", 0, "Job was cancelled while it was executing", false}
	CodeNoHandlerRegistered = ErrorCodeEntry{"OJS-3010", "NoHandlerRegistered", "", 0, "No handler is registered for the received job type", false}
)

// OJS-4xxx: Workflow Errors
var (
	CodeWorkflowNotFound    = ErrorCodeEntry{"OJS-4000", "WorkflowNotFound", "", 404, "The specified workflow does not exist", false}
	CodeChainStepFailed     = ErrorCodeEntry{"OJS-4001", "ChainStepFailed", "", 422, "A step in a chain workflow failed, halting subsequent steps", false}
	CodeGroupTimeout        = ErrorCodeEntry{"OJS-4002", "GroupTimeout", "", 504, "A group workflow did not complete within the allowed timeout", true}
	CodeDependencyFailed    = ErrorCodeEntry{"OJS-4003", "DependencyFailed", "", 422, "A required dependency job failed, preventing execution", false}
	CodeCyclicDependency    = ErrorCodeEntry{"OJS-4004", "CyclicDependency", "", 400, "The workflow definition contains circular dependencies", false}
	CodeBatchCallbackFailed = ErrorCodeEntry{"OJS-4005", "BatchCallbackFailed", "", 422, "The batch completion callback job failed", true}
	CodeWorkflowCancelled   = ErrorCodeEntry{"OJS-4006", "WorkflowCancelled", "", 409, "The entire workflow was cancelled", false}
)

// OJS-5xxx: Authentication & Authorization Errors
var (
	CodeUnauthenticated    = ErrorCodeEntry{"OJS-5000", "Unauthenticated", "UNAUTHENTICATED", 401, "No authentication credentials provided or credentials are invalid", false}
	CodePermissionDenied   = ErrorCodeEntry{"OJS-5001", "PermissionDenied", "PERMISSION_DENIED", 403, "Authenticated but lacks the required permission", false}
	CodeTokenExpired       = ErrorCodeEntry{"OJS-5002", "TokenExpired", "TOKEN_EXPIRED", 401, "The authentication token has expired", false}
	CodeTenantAccessDenied = ErrorCodeEntry{"OJS-5003", "TenantAccessDenied", "TENANT_ACCESS_DENIED", 403, "Operation on a tenant the caller does not have access to", false}
)

// OJS-6xxx: Rate Limiting & Backpressure Errors
var (
	CodeRateLimited         = ErrorCodeEntry{"OJS-6000", "RateLimited", "RATE_LIMITED", 429, "Rate limit exceeded", true}
	CodeQueueFull           = ErrorCodeEntry{"OJS-6001", "QueueFull", "QUEUE_FULL", 429, "The queue has reached its configured maximum depth", true}
	CodeConcurrencyLimited  = ErrorCodeEntry{"OJS-6002", "ConcurrencyLimited", "", 429, "The concurrency limit has been reached", true}
	CodeBackpressureApplied = ErrorCodeEntry{"OJS-6003", "BackpressureApplied", "", 429, "The server is applying backpressure", true}
)

// OJS-7xxx: Extension Errors
var (
	CodeUnsupportedFeature   = ErrorCodeEntry{"OJS-7000", "UnsupportedFeature", "UNSUPPORTED_FEATURE", 422, "Feature requires a conformance level the backend does not support", false}
	CodeCronScheduleConflict = ErrorCodeEntry{"OJS-7001", "CronScheduleConflict", "", 409, "The cron schedule conflicts with an existing schedule", false}
	CodeUniqueKeyInvalid     = ErrorCodeEntry{"OJS-7002", "UniqueKeyInvalid", "", 400, "The unique key specification is invalid or malformed", false}
	CodeMiddlewareError      = ErrorCodeEntry{"OJS-7003", "MiddlewareError", "", 500, "An error occurred in the middleware chain", true}
	CodeMiddlewareTimeout    = ErrorCodeEntry{"OJS-7004", "MiddlewareTimeout", "", 504, "A middleware handler exceeded its allowed execution time", true}
)

// AllErrorCodes contains every defined OJS error catalog entry.
var AllErrorCodes = []ErrorCodeEntry{
	// OJS-1xxx
	CodeInvalidPayload, CodeInvalidJobType, CodeInvalidQueue, CodeInvalidArgs,
	CodeInvalidMetadata, CodeInvalidStateTransition, CodeInvalidRetryPolicy,
	CodeInvalidCronExpression, CodeSchemaValidationFailed, CodePayloadTooLarge,
	CodeMetadataTooLarge, CodeConnectionError, CodeRequestTimeout, CodeSerializationError,
	CodeQueueNameTooLong, CodeJobTypeTooLong, CodeChecksumMismatch, CodeUnsupportedCompression,
	// OJS-2xxx
	CodeBackendError, CodeBackendUnavailable, CodeBackendTimeout, CodeReplicationLag,
	CodeInternalServerError,
	// OJS-3xxx
	CodeJobNotFound, CodeDuplicateJob, CodeJobAlreadyCompleted, CodeJobAlreadyCancelled,
	CodeQueuePaused, CodeHandlerError, CodeHandlerTimeout, CodeHandlerPanic,
	CodeNonRetryableError, CodeJobCancelled, CodeNoHandlerRegistered,
	// OJS-4xxx
	CodeWorkflowNotFound, CodeChainStepFailed, CodeGroupTimeout, CodeDependencyFailed,
	CodeCyclicDependency, CodeBatchCallbackFailed, CodeWorkflowCancelled,
	// OJS-5xxx
	CodeUnauthenticated, CodePermissionDenied, CodeTokenExpired, CodeTenantAccessDenied,
	// OJS-6xxx
	CodeRateLimited, CodeQueueFull, CodeConcurrencyLimited, CodeBackpressureApplied,
	// OJS-7xxx
	CodeUnsupportedFeature, CodeCronScheduleConflict, CodeUniqueKeyInvalid,
	CodeMiddlewareError, CodeMiddlewareTimeout,
}

// LookupByCode returns the ErrorCodeEntry for an OJS-XXXX numeric code
// (e.g., "OJS-1000"). Returns nil if the code is not recognized.
func LookupByCode(code string) *ErrorCodeEntry {
	for i := range AllErrorCodes {
		if AllErrorCodes[i].Code == code {
			return &AllErrorCodes[i]
		}
	}
	return nil
}

// LookupByCanonicalCode returns the ErrorCodeEntry for a canonical wire-format
// code (e.g., "INVALID_PAYLOAD"). Returns nil if the canonical code is not
// recognized or is empty.
func LookupByCanonicalCode(canonical string) *ErrorCodeEntry {
	if canonical == "" {
		return nil
	}
	for i := range AllErrorCodes {
		if AllErrorCodes[i].CanonicalCode == canonical {
			return &AllErrorCodes[i]
		}
	}
	return nil
}

