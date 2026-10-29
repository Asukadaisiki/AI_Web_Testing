# Langfuse First Closed Loop Acceptance

Date: 2026-10-28

Status: **ACCEPTED**

Langfuse is the primary UI for this closed loop. The accepted flow selects Pi,
Claude Code, or Codex plus an independent Harness, executes through the real
SDK boundary, preserves durable lifecycle state, and opens the corresponding
native Langfuse Trace. No acceptance trace was fabricated.

## Bound Scope

- Langfuse project: `agenttrace-native-project`
- Go project: `prj_7947f1c9add6acdf4fd26a01b2872b97`
- Go conversation: `conv_8b365a5b14fa99401b1ad864ccdc591b`
- Deployment: native processes only, no Docker
- Running production dist: `.next-first-loop-20261028-b`

## Real Runtime Results

| Runtime | Run | Status | Usage |
| --- | --- | --- | --- |
| Pi | `run_bff0299875cad52a2af7f1b618e09968` | succeeded | 286 tokens |
| Claude Code | `run_bcb60b4f2dcf6df105a9ac56e4db79a7` | succeeded | 471 tokens |
| Codex | `run_2d2ea05d3d0c4ceb3d5d8cf797f46410` | succeeded | 577 tokens |

All three Runs used generic Harness
`har_5702a36aee9f00555a66dbd12d9330ca` revision 1 and returned the exact
acceptance marker. The Harness was edited to revision 2 and deleted; each
historical Run retained revision 1, its original snapshot hash, usage and native
Trace.

## Browser Lifecycle

- Browser success: `run_1a2bf11d67d39e7be1579462fc5d7982`
  completed `open_page`, two committed `assert_text` operations and
  `finish_case`. Canonical evidence contains 12 spans, 9 attempts, 4 usage
  records, 4 operation records and 6 derived nodes.
- Active cancellation: `run_852bf9c3c837abe487afac3bddde6247`
  was cancelled after observable browser work and retained
  `cancel_requested_at` plus canonical operation evidence.
- Intentional failure: `run_67e64bea11e4cc2b355fadca03339710`
  failed on a nonexistent-text assertion.
- UI rerun: `run_f82001f51ba65301bf55be7c85dcd7f7`
  failed as expected and retained `retry_of_run_id`, Runtime, Harness ID,
  Harness revision 1, execution hash and Harness snapshot hash from the
  original failed Run.
- Browser Harness `har_2a1a8f3d8c1e4d2101f25ffaa0873278`
  was edited and deleted without altering either historical Run.

The native Trace for the successful browser Run renders the model/tool tree,
usage, AgentTrace metadata, passed dry-run evidence and four real 1280x800 PNG
images.

## Responsive Acceptance

At 390x844, the document width is 390px, long immutable Run IDs wrap inside
their table cell, and the remaining columns are available through a table-local
horizontal viewport. The New agent run dialog fits the viewport and was
captured only after computed opacity reached 1 with a visible, opaque white
background.

## Security And Scope

- Health without a bridge token: 200
- Runtime catalog without a token: 401
- Bound Go Run: 200
- Foreign Go Run: 403
- Disallowed Go route: 403
- Foreign Go artifact: 403
- Bound BFF Run: 200
- Foreign BFF Run/artifact: rejected as 412 by the intentional upstream
  401/403-to-`PRECONDITION_FAILED` mapping
- Exact canonical artifact: 200 `image/png`, valid PNG magic, 20,785 bytes
- Arbitrary BFF artifact path: 404
- Foreign exporter completion: acknowledged once, no Langfuse Trace created

Browser code contains none of the bridge, provider or Langfuse credentials.
The production client scan checked 1,226 assets against 15 actual private
values and found zero hits.

## Verification

- Go backend: `go test -p 1 ./...` passed for all packages
- Browser Runs client/server-unit: 15 files, 102 tests passed
- Web typecheck: exit 0, no diagnostics
- Scoped strict ESLint: exit 0, no findings
- Native launcher tests: 11/11 passed
- Upstream client scan: 681 files, clean
- Independent final review: APPROVED, 0 Critical, 0 Important, 0 Minor
- Final service health: Langfuse web, Go API, browser Worker, fixture,
  ClickHouse and MinIO all 200
- Final durable drain: 0 active Runs, 0 pending outbox events

Operational build and restart requirements are documented in
`integration/local/langfuse.md`.

## Residual Risks

1. The upstream Langfuse Worker completes graceful shutdown but retains an open
   process handle. Restarts required identity verification, complete resource
   closure, zero active Redis jobs, then termination of only that residual
   process. The launcher remains fail-safe and never force-kills by default.
2. SSE and export recovery are bounded. A prolonged outage requires the visible
   reconnect action or page refresh.
3. Accepted-but-lost creation followed by a Harness catalog edit/deletion or
   full page reload was not exercised natively. Immediate unchanged retries and
   failed-Run reruns were covered; Go remains authoritative for immutable
   accepted snapshots.
4. Storybook extensionless-import and Sentry deprecation warnings are inherited
   validation noise and were not changed.

## Rulings

1. Existing non-main branches and dirty worktrees were retained without commits
   or new worktrees. If wrong, saved baselines are required to review the
   incremental changes; user changes must not be overwritten.
2. Intermediate-only artifacts were kept under the persistent Cache directory.
   If wrong, only scratch placement changes; source deliverables remain intact.
3. This release binds one Project/Conversation and exposes an
   installation-level Harness catalog with `project:update` writes. If wrong,
   multi-project tenancy requires a later storage migration.
4. Native acceptance used real UI/API execution instead of seeded traces. If
   wrong, acceptance must be repeated, but no canonical data was fabricated.
5. Installed Playwright Chromium was used for 390x844 verification because the
   connected browser exposed no resize control. If wrong, repeat the same layout
   check in a resizable primary browser.
6. The generated sandbox owner's password hash was synchronized to the private
   runtime credential for local login. If wrong, restore the isolated backup
   hash; no other account was touched.
7. An identity-verified residual Worker was terminated only after complete
   graceful closure and zero active jobs. If wrong, repeat acceptance after
   restoring persistent services; no data directory or unrelated process was
   touched.
8. Initial unloaded Trace images were ruled to be below-fold lazy loading after
   direct 200 PNG responses and successful scroll-triggered loads. If wrong,
   repeat with lazy loading disabled in a disposable browser; no source change
   was made.
