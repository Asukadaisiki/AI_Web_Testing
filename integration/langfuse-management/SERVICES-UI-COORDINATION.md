# Final04 Services UI Coordination

For backend Services owner `arun_6ae1fb5641670d5cd270058e`, 2026-10-28.

Direct-message tool is not exposed to this agent. A CLI queue attempt returned
`No active session found matching` the supplied agent ID; delivery is not claimed.

Parent subsequently confirmed final Services code/schema coordination.
UI contract is aligned with current `WorkerServices`,
`serviceprobe.Measurement` and
`TestWorkerServicesReportsUnknownMetricsWithoutFalseZero`:

- `dlq_count`: nullable number, retained project outbox handoffs, not Kafka depth.
- `dlq_scope`: explicit scope string.
- `recent_registry_audit`: entries with `id`, `project_id`, `actor_id`, `action`,
  `resource_id`, `occurred_at`; UI shows action, resource and time only.
- `dependencies`: named entries with `status`, optional `mode`, optional `reason`.
- `observability`: named entries with `status`, `scope`, nullable numeric
  `value` and optional `reason`; measured Kafka omits reason and may include
  `consumer_group`, `topic`, `observed_at`. UI retains and displays these.
- Existing project queue counters remain numeric. Null oldest outbox age remains
  unavailable, never silently converted into a zero.

Unknown fields are stripped at the authenticated BFF. Backend reason strings
must be sanitized and must not contain private endpoint details or credentials.
No service restart or additional UI submissions will be made by this agent.
Parent owns final04 restart and invalid-name/no-Run read acceptance.

Final04 build and native client scan passed; separate static bundle/source
private-value scan passed with zero matches. Source captured after all changes.
Focused client/contract coverage (30 tests), management server coverage
(21 tests), full client suite (87 tests) and typecheck passed. Full server suite
retains one unrelated foreign-snapshot provenance failure (85/86 passed).

## Additive Telemetry Boundary

Parent authorized actual exporter/relay heartbeats and durable lease/stale
fencing counters after final04 was built. Final04 already parses/displays new
named `observability` entries without a rebuild. Project trusted telemetry into
the existing `serviceprobe.Measurement` shape, with nullable numeric value,
status, scope, optional sanitized reason and optional observed_at.
Measured zero and unmeasured null must remain distinct.

The new raw `service_health`, `lease_contention` and
`stale_fencing_rejections` top-level repository fields are stripped by this
BFF. They must be projected by the API into `observability` for the final04 UI
to display them. A raw heartbeat table is not part of the final04 UI contract.
Generic projection/display compatibility is covered by dedicated client and
contract tests; do not claim actual telemetry is displayed before the backend
API projection is in place.
