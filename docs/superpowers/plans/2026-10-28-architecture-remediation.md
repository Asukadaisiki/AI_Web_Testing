# Architecture remediation implementation plan

**Goal:** Complete the seven corrections in `docs/architecture-assessment-2026-10-28.md` and verify the real GLM browser workflow through every retained component.

**Architecture:** PostgreSQL remains authoritative. Kafka transports durable events to independent Redis notification and Langfuse consumers. The host owns sessions, Pi owns the model loop, and the selected Harness owns tools and verdicts.

**Constraints:** Use the existing local model credentials without logging them. Default real model is `glm-5.3-flash`. Keep the existing React task entry point. Do not add orchestration or plugin frameworks. Preserve existing workspace changes. User correction: all services run natively on the host; do not run Docker.

## 1. Explicit model configuration

- [x] Add configuration regression tests for an explicit provider independent of API credentials, reasoning effort, and invalid providers.
- [x] Update config, API model snapshot, runtime defaults and Compose to carry provider/model/reasoning separately from runtime secrets.
- [x] Remove unused script configuration; fixture execution remains an explicit provider.
- [x] Verify the local provider with a real request and record only sanitized evidence.

## 2. Durable completion and accurate observations

- [x] Add shared Run completion type/payload tests covering success, failure and cancellation.
- [x] Update PostgreSQL producer and exporter together; test duplicate delivery and export retries.
- [x] Record actual Run input, model call intervals and token usage; export unknown data as absent.
- [x] Run affected Go tests and actual PostgreSQL/export integration.

## 3. Redis notification and cancellation simplification

- [x] Add an independently checkpointed Kafka notification consumer which writes durable event sequence notifications to Redis.
- [x] Wire notifications into API lifecycle and preserve PostgreSQL SSE replay during Redis failure.
- [x] Remove unconsumed leases and cancel hints; use bounded durable cancellation polling.
- [x] Verify real Redis records, replay and degraded operation.

## 4. Harness boundaries and persistence failure

- [x] Add tests for a Harness requiring no browser address and browser extension validation.
- [x] Separate generic protocol from browser evidence contracts and Go browser projections.
- [x] Require the selected Harness verdict; remove browser fallback from generic completion.
- [x] On first event persistence failure cancel only the affected session and await cleanup.
- [x] Verify failure stops subsequent tool actions while concurrent sessions continue.

## 5. Joint deployment and acceptance

- [x] Supply a reproducible deployment including Langfuse and exporter with initialized local observation credentials.
- [x] Start native PostgreSQL, Kafka, Redis, browser Worker, API, Runtime, React and Langfuse services with a reproducible local runner.
- [x] Run real GLM browser success, failed goal and cancellation; inspect PostgreSQL, Kafka, Redis, SSE, artifacts and Langfuse.
- [x] Exercise Redis, exporter, Kafka and persistence failure boundaries.
- [x] Run appropriate Go, TypeScript, Python and frontend checks; update README, operations and final acceptance evidence.

Implementation proceeds in this already authorized session. Independent domains are delegated with disjoint file ownership; integration and final acceptance remain with the coordinating agent.

Completed evidence: [remediation acceptance](../../remediation-acceptance-2026-10-28.md).
