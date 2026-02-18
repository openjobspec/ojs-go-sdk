# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 (2026-02-18)


### Features

* **client:** add client with enqueue, batch, workflow, and queue ops ([2401b57](https://github.com/openjobspec/ojs-go-sdk/commit/2401b577611e8616bd0b5b94afc1f40aaf57ac11))
* **client:** add schema registry CRUD operations ([b5173f0](https://github.com/openjobspec/ojs-go-sdk/commit/b5173f0c73c9f251d1cec2ef313c627d29ae3b33))
* **client:** return pagination metadata from ListQueues ([b2d4250](https://github.com/openjobspec/ojs-go-sdk/commit/b2d42502a77458f6cf87f8197e4a721b28e1f472))
* **core:** add error types with errors.Is/As support ([978696d](https://github.com/openjobspec/ojs-go-sdk/commit/978696d28370e9eee54fb639ff919844b01db4b7))
* **core:** add job types and functional options ([3529713](https://github.com/openjobspec/ojs-go-sdk/commit/352971390a43bb9ed26d40b21d32bb5b34dafee0))
* **errors:** add NonRetryable error wrapper ([9f2f5e9](https://github.com/openjobspec/ojs-go-sdk/commit/9f2f5e9b1ff73cb7c53671149439a4559e3a4303))
* **errors:** add rate limit and Retry-After header parsing ([7b1b264](https://github.com/openjobspec/ojs-go-sdk/commit/7b1b264bc8dc67761f7343049e38ed95f5e650c3))
* **middleware/otel:** add OpenTelemetry tracing and metrics ([6e1398c](https://github.com/openjobspec/ojs-go-sdk/commit/6e1398c09b9ef967f69b47bd3b65897676cdb950))
* **middleware:** add logging, recovery, and metrics middleware ([68bec23](https://github.com/openjobspec/ojs-go-sdk/commit/68bec233b0a97e15c8e2dd2aed35a3e01161ad7c))
* **ojstesting:** add fake mode test utilities package ([59ceaf8](https://github.com/openjobspec/ojs-go-sdk/commit/59ceaf841fbe4bb0e77a55db2950122357990991))
* **ojstesting:** add FakeClient for real client testing ([1a673c7](https://github.com/openjobspec/ojs-go-sdk/commit/1a673c70a99111bfd540157ae420f92d2ead6f92))
* **serverless:** add Lambda/SQS and HTTP push handler ([03618e2](https://github.com/openjobspec/ojs-go-sdk/commit/03618e269ffed3c8a520098ec4d461d406396ea9))
* **transport:** add HTTP transport layer for OJS protocol ([afb4371](https://github.com/openjobspec/ojs-go-sdk/commit/afb4371354798fb88ee1a3098aed604384d648ec))
* **validate:** add client-side validation for job type and queue ([625d2da](https://github.com/openjobspec/ojs-go-sdk/commit/625d2da744286da707010179eac1f564a81e44e4))
* **worker:** add RegisterTyped generic handler ([99b59f2](https://github.com/openjobspec/ojs-go-sdk/commit/99b59f213d1704f2cf874a51db4a7a626aeb2010))
* **worker:** add structured logging for operational events ([0bcc328](https://github.com/openjobspec/ojs-go-sdk/commit/0bcc3284247d1b641c12056870669550b652f0f5))
* **worker:** add worker with goroutine pool and middleware chain ([d91877f](https://github.com/openjobspec/ojs-go-sdk/commit/d91877f286cf17fc90aae6087320f8f6fd961cb2))
* **worker:** export NewJobContextForTest for testing middleware ([9349ad5](https://github.com/openjobspec/ojs-go-sdk/commit/9349ad505f1af838ed931edfbdc0c3582be78c01))


### Bug Fixes

* **client:** URL-encode path and query parameters ([bfa7b32](https://github.com/openjobspec/ojs-go-sdk/commit/bfa7b320ad621e4f56398c65d24912f86aa50831))
* **transport:** limit response body size and fix Content-Type on bodyless requests ([cf96e55](https://github.com/openjobspec/ojs-go-sdk/commit/cf96e5501e2a53bd007ec77329378d79efb81a61))
* **worker:** respect error retryability in NACK ([90d1a52](https://github.com/openjobspec/ojs-go-sdk/commit/90d1a52cf0039c162078ce4c8b1509644a492e7f))


### Performance Improvements

* **client:** eliminate duplicate argsToWire calls ([bff99f3](https://github.com/openjobspec/ojs-go-sdk/commit/bff99f3b6f65fde76dcee2e7640f2d72a6eee5e7))
* **test:** add benchmarks for args, JSON, middleware, and validation ([9624841](https://github.com/openjobspec/ojs-go-sdk/commit/962484182b85f0429ce6c35ad7c8836ad31fec11))
* **worker:** add exponential backoff on fetch errors ([c3f1368](https://github.com/openjobspec/ojs-go-sdk/commit/c3f13680e93a983a80aaf1c549e1f86c985f404c))

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
