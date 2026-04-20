# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0](https://github.com/openjobspec/ojs-go-sdk/compare/v0.3.0...v0.4.0) (2026-04-20)


### Features

* add graceful shutdown signal handler ([a9dafc5](https://github.com/openjobspec/ojs-go-sdk/commit/a9dafc58b27da0f1639417ebbf755f3e399ed0e8))
* add initial project structure ([9f5df58](https://github.com/openjobspec/ojs-go-sdk/commit/9f5df582de5c31f6b3851fe7980d04c6f4a826f4))
* add initial project structure ([d8f68bd](https://github.com/openjobspec/ojs-go-sdk/commit/d8f68bde919db1a13d8a54f68fbabf82f6b04b9c))
* add rate limiter and subscription support ([bf8b3d4](https://github.com/openjobspec/ojs-go-sdk/commit/bf8b3d4a8e91c79d173e3fe64a7419ea79b560bc))
* add retry backoff configuration ([78d7166](https://github.com/openjobspec/ojs-go-sdk/commit/78d71667ec3c0cb7dbaa5b35c4103dfd353e14cd))
* add retry support to worker and extend test utilities ([d25be26](https://github.com/openjobspec/ojs-go-sdk/commit/d25be26ae624e6d2f26ee70fc705a7dda0ce94e7))
* add type-safe job handler registration with generics ([9ed7041](https://github.com/openjobspec/ojs-go-sdk/commit/9ed704190520e8c22e6693b2f6029f2e767c8f43))
* add workflow chain primitive ([86ed169](https://github.com/openjobspec/ojs-go-sdk/commit/86ed169b791c493ea0d39d67322772c3dc42be8d))
* expose batch enqueue endpoint ([44052cd](https://github.com/openjobspec/ojs-go-sdk/commit/44052cd07d55e4d7ea58366c87443f0cc5377355))
* implement core handler interfaces ([10152dc](https://github.com/openjobspec/ojs-go-sdk/commit/10152dc1304ef0a73ac4964864efb805148a1549))
* implement core handler interfaces ([d72362d](https://github.com/openjobspec/ojs-go-sdk/commit/d72362dd1eb84d3288aa62df80838de54b30d79b))


### Bug Fixes

* correct job state transition guard ([1f589e7](https://github.com/openjobspec/ojs-go-sdk/commit/1f589e7d86fc5135ef4e37c51c9ee0a77830be06))
* correct timestamp serialization ([94c3943](https://github.com/openjobspec/ojs-go-sdk/commit/94c39432860aba3161180237de48daadbc77cde1))
* correct timestamp serialization ([ffbc05a](https://github.com/openjobspec/ojs-go-sdk/commit/ffbc05a974d7a5adef89c842c65d185c353ce721))
* handle nil pointer in middleware chain ([7d4fcf6](https://github.com/openjobspec/ojs-go-sdk/commit/7d4fcf631b261b3addbec509a4165c21067a9100))
* handle nil response in poll loop ([ece1aeb](https://github.com/openjobspec/ojs-go-sdk/commit/ece1aebc8df4edb988ebcec310c044ab57d442ec))
* prevent double-close on worker pool ([1e0c46a](https://github.com/openjobspec/ojs-go-sdk/commit/1e0c46a64d7bba1d2729d67b9febad9716b02c5a))
* resolve edge case in input validation ([e562ce0](https://github.com/openjobspec/ojs-go-sdk/commit/e562ce08dce2a16360d5f561e8665d60e26a2e8f))
* resolve edge case in input validation ([52d3866](https://github.com/openjobspec/ojs-go-sdk/commit/52d3866495b681a678f6c62b87296c4476327b4d))


### Performance Improvements

* cache compiled regex patterns ([a446f7e](https://github.com/openjobspec/ojs-go-sdk/commit/a446f7e4e286387a9a6d9910ab04f28a03b71e7e))
* optimize data processing loop ([f8b89fe](https://github.com/openjobspec/ojs-go-sdk/commit/f8b89fed62eb5f8c015b64d4d42053c57966bf51))
* optimize data processing loop ([5a09893](https://github.com/openjobspec/ojs-go-sdk/commit/5a09893b1a7b91075cfe904d1405b593ffe72877))
* reduce allocations in hot path ([390c7f7](https://github.com/openjobspec/ojs-go-sdk/commit/390c7f7375aa96bd6dde2d10366154b3258ea6cb))

## [Unreleased]

## [0.4.0] - 2026-04-20

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
