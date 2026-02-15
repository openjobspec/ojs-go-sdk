# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `NonRetryable(err)` error wrapper — handlers can signal non-retryable failures
- `isHandlerRetryable` internal helper for worker NACK retryability decisions
- Exponential backoff on consecutive fetch errors in worker (capped at 30s)
- Dependabot configuration for GitHub Actions version updates
- `RegisterTyped[T]` generic handler — auto-unmarshals job args into typed structs
- `middleware/otel` subpackage — OpenTelemetry tracing and metrics middleware (separate Go module)
- `ojstesting.FakeClient(t)` — returns a real `*ojs.Client` backed by the in-memory fake store
- Fuzz tests for Job JSON unmarshaling, args wire conversion, and validation
- Tests for `NonRetryable`, `isHandlerRetryable`, worker NACK retryability, fetch backoff, URL encoding, typed handlers, FakeClient
- Project scaffolding: Makefile, .gitignore, doc.go, CONTRIBUTING.md, CHANGELOG.md
- GitHub Actions CI workflow with Go 1.22/1.23/1.24 matrix
- README badges (Go Reference, Go version, license)
- `WithLogger(*slog.Logger)` worker option for structured operational logging
- Tests for queue operations (ListQueues, GetQueueStats, PauseQueue, ResumeQueue)
- Tests for dead letter operations (ListDeadLetterJobs, RetryDeadLetterJob, DiscardDeadLetterJob)
- Tests for cron operations (ListCronJobs, RegisterCronJob, UnregisterCronJob)
- Tests for Manifest endpoint
- Tests for Group and Batch workflow primitives
- Tests for GetWorkflow and CancelWorkflow
- Tests for worker error paths (handler errors, missing handlers, nack)
- Tests for JobContext.Heartbeat and JobContext.Context
- Tests for UseNamed middleware
- Tests for retry/unique policy wire conversion
- Tests for error sentinel matching and Error string formatting
- Tests for worker state transitions (quiet, terminate, quiet-to-running)
- Tests for InsertBefore and InsertAfter middleware chain operations
- Tests for all enqueue options (WithPriority, WithTimeout, WithDelay, WithScheduledAt, WithExpiresAt, WithUnique, WithTags, WithMeta, WithVisibilityTimeout)
- Tests for worker options (WithWorkerAuth, WithWorkerHTTPClient, WithLogger)
- Tests for NewJobContextForTest
- Tests for worker logError/logWarn helpers
- Unit tests for `ojstesting` package (Fake, assertions, Drain, match options)

### Changed
- `ListQueues` now returns pagination metadata `(*Pagination)` alongside queues

### Fixed
- Worker NACK now respects error retryability (`NonRetryable` → `retryable=false` in NACK)
- Missing handler NACK now sends `retryable=false` (no point retrying without a handler)
- URL-encode path parameters in all client methods (job IDs, queue names, cron names, workflow IDs)
- URL-encode queue query parameter in `ListDeadLetterJobs`
- Eliminate duplicate `argsToWire` call in `Enqueue` and `EnqueueBatch`
- Worker ACK/NACK/fetch errors are now logged instead of silently discarded
- Transport only sets `Content-Type` header on requests with a body (POST)
- Transport limits response body reads to 10 MB to prevent unbounded memory usage

## [0.1.0] - 2026-02-12

### Added
- Client with enqueue, batch, workflow, and queue operations
- Worker with goroutine pool, middleware chain, and graceful shutdown
- HTTP transport layer for OJS protocol (v1.0.0-rc.1)
- Job types with 8-state lifecycle and functional options
- Error types with `errors.Is`/`errors.As` support
- Workflow primitives: Chain, Group, Batch
- Dead letter queue operations
- Cron job operations
- Queue management (list, stats, pause, resume)
- Server health and manifest introspection
- Three example programs (basic, worker, workflow)
- Unit and integration tests
- Apache 2.0 license
