# Langfuse Management Implementation

Approved design: `docs/superpowers/specs/2026-10-28-langfuse-agent-runtime-management-design.md`.
Execution authorized in the named sibling checkout; no commits or backend/worker/host edits.

- [x] Inspect existing feature, native UI primitives, RBAC and contracts.
- [x] Test and implement authenticated project binding and dynamic conversation routing.
- [x] Test and implement registry, environment, services and control-session BFF contracts.
- [x] Test and implement native management UI, run snapshots and retry lineage.
- [x] Verify feature server/client suites, typecheck and scoped static review.
- [x] Capture scoped reusable source assets, bootstrap and exact backend contracts.
- [x] Fresh native final04 build and native client scan, with no live service restart.
- [x] Final Skill-name/Services tests, typecheck, private bundle/source scan and source capture.
- [x] Record all final outcomes, including the independent full-server-suite failure.
- [x] Parent: final05 LF3300 restart and read-only invalid-name/valid-name/Services retest.

The prior actual UI acceptance is complete: 33 screenshots and all mutations
finished. Do not replay submissions. Final04 client suite: 87 passed;
management server suite: 21 passed; broader server suite: 85 passed, 1 failed.
The parent reproduced and fixed that provenance failure, then verified all 86
server tests and the final05 deployed UI. Historical failure remains recorded
in `VALIDATION.md`.

Transport: every call carries server-only Bearer plus project, Langfuse project,
membership-derived role and current actor headers. Verify
`GET /api/v1/projects/{id}/binding` before accessing any project resource.
Harnesses publish fixed references only; raw command/env inputs are removed.
Control-session transport stays on the API origin behind authenticated Langfuse routes.
