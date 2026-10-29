# Langfuse Agent Runtime Management Acceptance

Date: 2026-10-28. Status: passed with documented deployment limitations.

The approved Phase 0–4 implementation is present in the working tree. Langfuse
is the product UI; PostgreSQL owns execution facts, Kafka delivers durable
commands, and Redis supplies optional wakeups.

## Implemented behavior

- Inbox claims commit before external execution. Duplicate delivery cannot
  start another attempt, and uncertain external execution is not replayed.
- Project-scoped Skill uploads, immutable versions, reviewed MCP capabilities,
  bindings and Harness snapshots replace arbitrary command/env execution.
- Each dynamic Conversation owns an encrypted Browser Environment. Creation,
  archive, deletion, provisioning, reset and review enforce ownership and
  delivery/lease guards.
- Runtime/browser registration, capacity, drain, assignments, exclusive leases
  and fencing reject stale writes. Browser provider affinity preserves identity.
- One-time takeover provides authenticated screenshots and controlled browser
  input. Generation reset invalidates prior writes. Langfuse adds Registry,
  Environment and admin Services alongside native Trace and browser evidence.

## Evidence

| Verification | Observed result |
| --- | --- |
| Full Go race regression | Passed; opt-in integration tests are separately scoped |
| SDK Host full regression | 481 passed, 2 opt-in skips; both opt-ins separately passed |
| Additional real password-worker test | Passed; actual private input and fresh-context dry run, no public canary |
| Additional configured live Codex/MCP test | Passed |
| Python browser worker full suite | 142 passed, 107.396 seconds |
| Local launcher and scenario unit tests | 42 passed |
| Real PostgreSQL/Chromium control gateway | Passed, including encrypted identity persistence and reset |
| Registry, snapshot, inbox, leases and managed deletion | Dedicated PostgreSQL race tests passed |
| Real management API scenario | All 16 checks passed; API-only report remains provisional |
| Real Langfuse browser session | 61 checks passed, 8 exploratory failures retained with corrections; 33 screenshots |
| Final independent review | Reproduced privacy/TTL/provenance failures fixed; no unresolved P1/P2 in final narrow review |
| Langfuse final feature tests | 86 server and 87 client tests passed; typecheck/build passed |
| Services PostgreSQL race tests | All 5 passed, including real lease rejection counters and completed relay heartbeats |
| Concurrent runtime crash | 9 real Pi Runs; 7 executed checks passed; crash quarantined without replay |
| Infrastructure faults | Real isolated Redis/Kafka/PostgreSQL faults passed; 4 negative controls caught |
| Final05 read-only UI | Invalid/valid Skill names and Services heartbeat/lag/counter/audit rendering passed; 3 screenshots |
| Final deployed three-SDK smoke | Pi, Claude Code and Codex succeeded; each had live tools, 10 PNG records and no fixture password in public Trace |

The real API scenario used Pi, Claude Code and Codex with configured model
providers, nonzero usage, actual browser tools and PNG artifacts. It verified
success, failure/rerun, cancellation, immutable snapshots after publication and
revocation, takeover, generation reset, drain restoration and deletion.
All three SDKs retained Conversation A's actual browser identity across runs,
kept Conversation B signed out, and verified reset removed A's identity.

The [browser report](../integration/langfuse-management/live-browser-evidence-20261028-212222/VALIDATION.md)
records visible UI actions, native Trace navigation, canonical PNG rendering,
takeover input effects, version retention, archive/delete and cleanup. Invalid
Skill input and exploratory locator failures remain recorded. It does not claim
live viewer/RBAC or active-worker drain behavior.

Machine-readable source digests, API checks, runtime IDs and topology are in
[the consolidated JSON](management-runtime-acceptance-2026-10-28.json).
Separate [runtime fault evidence](management-faults-2026-10-28.md) and
[infrastructure fault evidence](management-infrastructure-faults-2026-10-28.md)
retain their own test scope and measurements.

The [final review](management-runtime-review-2026-10-28.md) records confirmed
failures and their fixes. Public model telemetry is selectively deferred and
bounded to 256 events / 4 MiB; grounded browser progress remains live.
Unclassified browser errors omit model content. Control close crossing expiry
retains `needs_review`, and the BFF checks snapshot project ownership.

Final05 was built with Node24, passed the native client scan and a separate
1,298-file scan against 13 private values with zero matching files. Local
services were restarted only after verifying zero active Runs, leases and
control sessions. Encrypted profile history and the crash quarantine were retained.
Final SDK smoke used 62,784 / 16,934 / 84,192 tokens respectively, verified
fresh-context cases and deleted only its own Conversation after release.
The final API, browser, Langfuse, runtime and relay/exporter health checks passed.
An expanded final scan including published evidence checked 1,308 files against
the same private values, with zero matches.

## Deployment and limitations

The local topology uses Langfuse 3300, API 8180, browser provider 8110,
PostgreSQL database `agenttrace_test_management_acceptance_20261028`,
Kafka 9192 with eight partitions, and Redis database 4. The browser provider
declares capacity four. Follow [Management Runtime Setup](management-runtime-setup.md)
for feature flags, credentials, seed, persistent encryption key and rollback.

Finite local tests do not establish production capacity, a long-running soak,
cross-region recovery or distributed browser-store failover. Browser identity
remains tied to its registered provider; failover needs shared encrypted storage,
key and locking, or a shared managed browser service.

MCP execution supports deployed reviewed capabilities. Third-party sandboxing,
arbitrary shell execution and generic host filesystem access are outside scope.
The deployed browser capability has no nonempty Secret schema; scoped reference
validation is tested separately. Real third-party 2FA and live viewer role
switching were not exercised.

Source changes and existing uncommitted work are retained. No commit or push
was performed. Source/build identity is recorded in the consolidated JSON.
Historical crash measurements predate the final privacy/TTL changes; final
SDK smoke and targeted regressions validate those changes separately.
Service counters are best-effort observations rather than exact historical
totals; exporter heartbeat measures liveness rather than export delivery.
