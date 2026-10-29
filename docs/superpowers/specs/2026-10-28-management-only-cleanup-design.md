# Management-only Cleanup Design

状态：清理完成；独立审查、回归及本地三 SDK/UI 验收通过，随本次清理提交保存。
基线：`c7772f5`，Langfuse Agent Runtime Management Phase 0–4。

## Goal

Keep one production product and execution path: Langfuse UI/BFF → authenticated
Go Control API → project Registry snapshots → fenced Runtime/Browser Workers.
Remove obsolete compatibility implementations, entrypoints and duplicate
deployment scenarios. Existing persisted history remains readable; no database,
profile, artifact or historical evidence is deleted.

## Required Behavior

- Management is mandatory, not an optional fallback to an unauthenticated API.
- Message creation accepts content, runtime_id, harness_id and idempotency_key.
  Old agent_id/model-only submissions are rejected.
- Only project-scoped Registry routes publish/select Harnesses. Global mutable
  Harness CRUD and `/agents` are removed.
- New Runs atomically resolve immutable project versions and a ready owned
  Environment. Runtime execution and evidence writes require a current Lease
  and fencing identity. Snapshotless/unfenced queued work fails closed.
- Preserve existing managed create/rerun idempotency hash serialization and
  replay-before-readiness ordering. Removing old request fields must not change
  previously stored managed hashes or response identities.
- The Node process supports the shared session-host protocol only. A Run selects
  an explicit supported SDK and validated Harness snapshot; no default-Pi,
  startup-module or disk-Skill fallback.
- Browser HTTP sessions and replay requests require project/environment/
  generation/fencing identity and internal authentication. Remove unmanaged
  production session/replay routes and branches.
- The native launcher always seeds the managed project and starts backend
  processes. It cannot start the old React console or a non-management stack.
- Retain Pi, Claude Code and Codex official SDKs, active native/flat provider
  adaptation, scripted deterministic tests, browser fixtures and current
  `pi.*` / JSONL protocol names. These are current capabilities, not obsolete
  compatibility simply because their names include `compatible` or `pi`.
- Remove old completion aliases once the active application backlog is audited.
  Canonical producers and consumers use `run.completed`.
- Keep applied additive migrations and historical database rows. A historical
  Run without an executable current snapshot can be inspected but not silently
  rerun under a different Harness.
- Remove unbound global project-management, old response/continuation and raw
  artifact-serving entrypoints when their only live purpose is the old product.
  Retain scoped authenticated trace/artifact access and controlled takeover.
- Remove `web/`, old Compose deployment and obsolete `integration/v3` scenarios.
  Extract still-used acceptance helpers before deleting legacy native scenarios.
  Keep current managed API/UI/load/fault acceptance and source capture.
- Update active README, architecture, contract, setup and Docker/source wiring.
  Dated historical acceptance reports retain their actual tested scope.

## Execution Safety

Work in the existing feature checkout; preserve unrelated files and the
Langfuse sibling. Source deletion is tracked by Git. Never delete private state,
stop an unrelated process, reset a database or inject faults into shared services.
Opt-in database tests use isolated `agenttrace_test_*` databases.

The implementation must retain idempotency, immutable rerun lineage,
environment ownership, provider affinity, controlled takeover, expiry quarantine,
password redaction and live grounded browser progress. No new database downgrade
or destructive cleanup is needed.

## Verification

Run meaningful rejection regressions for removed API/request modes, current
managed success/idempotency/rerun paths, shared host selection and unmanaged
browser refusal. Run full Go race, SDK Host and worker suites, current native
launcher/scenario tests and Langfuse source contracts. Run isolated PostgreSQL
management/leases/control coverage for changed persistence paths.

Finally verify scoped real SDK/browser execution on the updated native stack,
after all original Runs, controls and leases are quiescent. Preserve the retained
crash quarantine. Record removed paths, results and remaining deployment limits.
Independent task reviews and a final integration review precede completion.

Read-only native backlog audit before implementation found zero nonterminal
Runs, active leases, pending Outbox rows, legacy completion rows and pending
snapshotless commands. This is local deployment evidence, not permission to
delete historical records or assume an external deployment has drained.
