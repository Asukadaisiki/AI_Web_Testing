# Validation And Deployment Handoff

## Final05 Deployed Verification

Parent reproduced the foreign snapshot-project failure, added the project
check before trace access and retained the original test. Fresh complete feature
server suite: 86 passed. Typecheck passed. Source was recaptured.
Fresh `.next-management-20261028-05` production build/native client scan passed;
separate static/captured-source scan: 1,298 files, 13 private values, zero matches.

Final05 now serves LF3300. API heartbeat and relay observer are wired;
Services projects durable telemetry into `observability`. Final read-only
browser checks passed for invalid Skill feedback/disabled upload, valid-name
enabled upload and Services heartbeat/lag/counter/audit display:
[three screenshots and report](live-final05-evidence-r2/report.json).
The prior directory-input locator failure is retained at
`live-final05-evidence/report.json`; the test requires a directory path.
Neither read-only attempt submitted a package or Run.

Pi, Claude Code and Codex separately passed the final deployed browser/password
smoke with live tools, real PNG and nonzero usage. Their own Conversation was
deleted after release. See [consolidated acceptance](../../docs/management-runtime-acceptance-2026-10-28.md).
The Final04 and Final03 sections below preserve historical verification,
including the subsequently resolved test failure.

## Final04 Source And Build

2026-10-28, final Skill-name fix and Services projection in the authorized sibling.
Source implementation, verification and artifact preparation are complete.
No LF3300, API8180 or original49449 restart was performed.
No UI submissions, commits, resets or unrelated source edits were made.

| Verification | Actual Result |
| --- | --- |
| Focused Skill/Services client and contract coverage | 30 tests across 3 files passed in the final full-feature run |
| Full feature client suite | 87 tests, 17 files passed, exit 0 |
| Focused management server suite | 21 tests passed, exit 0 |
| Full feature server suite | 85 passed, 1 failed, 86 total across 8 files; not all green |
| Isolated existing snapshot-provenance test file | Same failure reproduced: 12 passed, 1 failed |
| Node 24 web typecheck | Passed, exit 0 |
| Targeted ESLint for 7 edited/new files | 0 errors, 3 existing no-void warnings, exit 0 |
| Sibling `git diff --check` | Passed, exit 0 |
| Scoped source capture | Completed after source changes; feature and server-test trees match sibling |
| Captured SHA-256 manifest verification | Passed, exit 0 |
| Native production build and client soundness scan | Passed, exit 0, `.next-management-20261028-04` |
| Separate private-value static bundle and captured-source scan | Passed after final recapture: 1,298 files, 270,051,880 bytes, 12 private values, zero matching files |
| Restart and final deployed read-only browser check | Parent-owned; not performed by this agent |

`SharedSkillNameSchema` is used by `SkillUploadSchema.name` and the upload UI:
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, maximum 128 characters, no trimming or
silent normalization. The additional whitespace check prevents JavaScript's
end-anchor from accepting a trailing newline that Go rejects. The input has
`aria-describedby`, inline help/error and invalid upload gating. A valid
128-character payload remains unchanged.

Services retains real `dlq_count` and `dlq_scope`, dependency status/mode/reason,
recent registry audit and `observability` status/scope/reason/nullable value.
Measured Kafka lag may omit reason and includes configured consumer group,
topic and observation time. Missing measurements display `Unavailable`, not
zero. Real zero is preserved. Audit UI exposes action/resource/time, not actor
identity or internal payloads. See `SERVICES-UI-COORDINATION.md`.

Additive exporter/relay heartbeat and lease/fencing compatibility tests were
added after the build; production source is unchanged, so final04 needs no
rebuild for new named `observability` measurements. The backend must project
its raw `service_health`, `lease_contention` and `stale_fencing_rejections`
fields into that measurement map. Raw top-level telemetry is stripped by the
BFF; generic parsing/display tests are not a claim that the backend has finished
that API projection or that live heartbeat data was browser-validated.

The broader server failure is outside this change:
`agenttrace-client.servertest.ts`, "rejects foreign snapshot provenance before
returning a run or reading its trace", expects `NOT_FOUND` but receives
`INTERNAL_SERVER_ERROR`. Current `assertRunProject` checks the outer run project
only, so this existing test reaches trace parsing with a run-shaped response.
The dirty provenance test and production code were preserved, not weakened or
changed to make the suite green. Parent must account for this independent issue.

Final restart target:
`/Users/bytedance/project/langfuse-agenttrace/web/.next-management-20261028-04`.
Built using `/usr/local/bin/node` v24.18.0, pnpm 11.10.0 and the native wrapper,
with private `agenttrace-management-acceptance-20261028/acceptance.env` and native
root `/Users/bytedance/Library/Caches/agenttrace-native/langfuse`.
Build log and credentials remain private (0600). The separate privacy comparison
reads credential values privately and checks literal, URI-encoded, base64 and
JSON-escaped forms; only counts are reported.

Completed actual UI evidence remains unchanged at
`live-browser-evidence-20261028-212222/VALIDATION.md`: 61 passing checks,
8 preserved exploratory failures, 33 screenshots, all mutations finished.
This final source fix does not relabel that historical session as green.
Parent's final04 check is read-only: load the already-owned package, verify
invalid names disable upload with accessible feedback and a valid slug enables
it, without clicking Upload draft or submitting any new run.

## Historical Final03 Verification

2026-10-28, Langfuse sibling checkout:

| Verification | Result |
| --- | --- |
| Feature server suite | 81 tests, 8 files passed (Go conversation and catalog metadata regressions) |
| Feature client suite (post-format run) | 55 tests, 14 files passed |
| `pnpm --filter web run typecheck` | Passed, exit 0 |
| Feature ESLint | 0 errors, 17 non-blocking warnings (no-void, unused prop types, type-import convention) |
| `git diff --check` | Passed |
| Scoped source capture | Completed, full source plus tests and small wiring patch |
| Production Next build | Passed, exit 0: `.next-management-20261028-03` under `/usr/local/bin/node` v24.18.0 with private acceptance.env |
| Native client-secret scan | Passed for corrected `-03` build |
| Real API BFF binding/list/create/archive | Passed; statuses 200,200,201,200,200,200; `live-evidence.json` |
| Real API BFF runtimes/registry/services | All 6 checks passed after metadata stripping fix; `live-catalog-evidence.json` |
| LF3300 restart | Not performed; awaiting parent coordination |
| Actual browser acceptance | Parent-owned, pending new management stack and coordinated restart |

Environment warnings: shell uses Node 22.23.4 while upstream requires Node 24;
pnpm 11.10.0 is installed. Tests also report existing Storybook extensionless
imports and Sentry deprecation warnings. Production build should use Node 24
through the parent's native wrapper.

BFF coverage includes authenticated project binding, current actor and role
headers, viewer/admin rejection before transport, explicit conversation scope,
foreign run/environment rejection, flat registry response normalization,
204 skill revoke, worker draining contract, server-only takeover token
consumption, same-origin browser inputs, PNG validation and sanitized errors.
Client coverage includes conversation lifecycle confirmations, archived and
environment run gating, review/reset confirmations, capability-only Harness
publication, draft preview and MCP binding, retry intent keys, trace evidence,
bounded live update recovery and token-free takeover inputs.

Parent confirmed scoped `/projects/{id}/binding` GET and
`/api/v1/environments/{id}/review` POST are registered. The BFF conversation
contract now sends `{title}` for create and `{state:"archived"}` for archive,
normalizing wire `title`/`state` to UI `name`/`status`. Unsupported restoration
is no longer advertised. Real API list/create/archive passed against port8180
using the new stack's private acceptance env. See `live-evidence.json`.

No backend, worker or host files were changed. No commits, resets or service
operations were made. An earlier agent-owned fresh-dist build was cancelled
before launching the parent's authorized target; LF83340 was never touched.
Source was recaptured after the title/state correction.

Superseded build target from the historical handoff:
`/Users/bytedance/project/langfuse-agenttrace/web/.next-management-20261028-03`.
Use `LANGFUSE_NATIVE_ROOT=/Users/bytedance/Library/Caches/agenttrace-native/langfuse`
and the SAME dist name with the private new acceptance env.

Late real-catalog correction: output schemas inherited strict input parsing and
rejected valid backend `revision`, `created_by`, `created_at` metadata. Harness
and MCP binding output schemas now strip unknown fields. Inputs remain strict.
Live reads all passed after this correction. Typecheck and all 81 server tests
also passed. Do NOT use the superseded `-02` build for management acceptance.

Backend snapshot coordination: current `publicRun` was observed to omit
`harness_snapshot`; UI renders the approved sanitized immutable snapshot
projection once supplied. Do not expose raw internal specification contents.
