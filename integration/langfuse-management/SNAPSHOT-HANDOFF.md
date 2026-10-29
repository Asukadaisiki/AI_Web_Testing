# Immutable Snapshot Boundary Handoff

## Implemented And Verified UI/BFF

- Native management tabs, dynamic conversation selection and lifecycle,
  environment recovery and authenticated takeover, registry version selectors,
  service metrics, and immutable rerun intent keys.
- Strict request schemas with sanitized output schemas.
- Correct Go wire contracts for title/state, flat skill versions, MCP bindings,
  worker draining and one-time takeover token consumption.
- Build `-03` and client-secret scan passed. 81 server tests, 55 client tests
  and typecheck passed before the provenance follow-up.
- Real BFF binding/list/create/archive and all six registry/service reads passed;
  evidence files are in this directory.

## Backend-Owned Verification

Parent code now implements `publicRunHarnessSnapshot` and `toPublicRun` in
`backend/internal/api/v3/runs.go`. The backend projection is no longer missing.
Backend owner should verify its deployment and immutable replay behavior:

1. GET `/api/v1/runs/{id}` and conversation run listings project the persisted
   command snapshot, not the current registry.
2. `harness_snapshot.skill_version_ids` identifies source skill versions.
   `mcp_binding_ids` identifies PROJECT BINDINGS, never MCP version IDs.
3. Snapshot includes sanitized Harness identity/version/revision/digest, allowed
   runtime IDs, skill provenance and MCP capability/version/digest only.
4. Top-level public model identity, environment_id and profile_generation are
   copied from the persisted command. No prompt, skill bytes, MCP config,
   secret refs, provider session IDs, internal URLs or credentials are returned.
5. Registry updates, revocation, and retries preserve original provenance.
   Historical snapshots without binding IDs retain empty lists, not guessed IDs.
6. Exporter Session ID is `agenttrace-session-{conversationId}`; Trace ID is
   `agenttrace-trace-{runId}`. Session links must land on this deterministic group.

No backend code or service control is delegated back to the UI agent.

## UI-Owned Follow-Up

In progress: retain the richer sanitized projection in the BFF output schema,
render a typed provenance panel (no current-registry lookup), and correct the
native Session link prefix. Feature tests, recapture and a new build are needed
before these follow-up edits appear in the deployed UI.
