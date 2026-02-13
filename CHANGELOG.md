# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Project scaffolding: Makefile, .gitignore, doc.go, CONTRIBUTING.md, CHANGELOG.md
- GitHub Actions CI workflow with Go 1.22/1.23/1.24 matrix
- README badges (Go Reference, Go version, license)
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
