# Takeover Integration Handoff

For takeover agent `arun_6ae1e9df41670d5cd26ff680`:

- Langfuse BFF POSTs `/api/v1/environments/{id}/control-session` with the authenticated bridge bearer and project, Langfuse project, membership role and actor headers.
- Response: `{id,environment_id,expires_at,control_url,state}`; optional `project_id` is validated when present.
- Current API source returns a separate `token` field alongside the relative `control_url`. BFF will consume this field server-side; a relative URL with a `token` query parameter is also supported. Neither is exposed to the browser and BFF never follows the URL.
- In the same start operation BFF POSTs `{token}` to the fixed API-origin `/api/v1/control-sessions/{id}/consume`, with the SAME project and actor headers. Response session must match `id` and `environment_id`.
- Browser receives only `{id,expires_at,url:"/api/agenttrace/control/{id}?projectId={langfuseProjectId}"}`.
- Authenticated Langfuse GET on this URL proxies `/api/v1/control-sessions/{id}/screenshot` (PNG). POST proxies `/input` using `{action:"click",x,y}`, `{action:"key",key}`, `{action:"type",text}` or `{action:"navigate",url}`. URL navigation is HTTP(S) only.
- Session expiry, project/actor ownership and consumed-state authorization must be enforced on EVERY backend screenshot/input/delete call. Langfuse repeats project membership checks; it cannot independently inspect session ownership without a session-read endpoint.
- Stop is authenticated DELETE `/api/v1/control-sessions/{id}` through the Langfuse tRPC BFF.
- No iframe, worker URL, bridge credential or one-time token appears in the product browser.

Verified against `backend/internal/api/v3/control_sessions.go`: dedicated session GET is available, session ownership is scoped to project and actor, consume activates the provider, DELETE returns the closed session. Screenshot size is 1280x800; BFF input bounds will match this viewport.
