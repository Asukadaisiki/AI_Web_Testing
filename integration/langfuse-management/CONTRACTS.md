# Exact Backend Contracts

Prefix: `/api/v1`. All requests are authenticated with the headers in README.
IDs are encoded path components. Extra upstream internal fields are stripped
before product output. Redirects are rejected. Errors are sanitized.

## Project And Conversations

| Method and path | Request | Response |
| --- | --- | --- |
| GET `/projects/{at}/binding` | none | `{agenttrace_project_id,langfuse_project_id,status:"active"}` (`project_id` alias accepted) |
| GET `/projects/{at}/conversations` | none | `{conversations:[{id,project_id,title?,state}]}` |
| POST `/projects/{at}/conversations` | `{title}` | conversation |
| PATCH `/conversations/{id}` | `{state:"archived"}` | conversation |
| DELETE `/conversations/{id}` | none | JSON acknowledgement or 204 |
| GET `/conversations/{id}` | none | `{conversation,messages,runs}` for the selected conversation |
| GET `/conversations/{id}/environment` | none | environment |
| POST `/environments/{id}/reset` | `{}` | environment |
| POST `/environments/{id}/review` | `{}` | environment |

Conversation wire states: active, archived, deleting, deleted. BFF normalizes
`title` to UI `name` (omitted titles become "Untitled conversation") and `state`
to UI `status`. The current backend supports archive only; restoration is not
advertised in the UI.
Environment: `{id,project_id,conversation_id,state,profile_generation,
current_run_id?,last_used_at?,lease?:{attempt_id,expires_at}}`.
States: provisioning, ready, leased, resetting, needs_review, deleting, broken.
Only ready permits new runs or reruns. Archived conversations are read-only.
Reset/review confirm external effects deliberately; reset has a second confirmation.
Backend must enforce lease ownership and lifecycle conflicts independently.

Parent confirmed the scoped binding GET and `/review` POST are now registered;
review expects `{}` and returns Environment with HTTP 200. The environment's
active-run/lease fields are optional; adding them enables richer lease details.

## Registry

| Method and path | Request | Response |
| --- | --- | --- |
| GET `/projects/{at}/skills` | none | `{skills:[SkillVersion]}`; BFF groups versions by `skill_id` |
| POST `/projects/{at}/skills/uploads` | `{name,files:[{path,content}],archive_base64?}` | draft SkillVersion |
| POST `/projects/{at}/skills/{skillId}/versions` | `{version_id}` | published SkillVersion |
| POST `/projects/{at}/skill-versions/{versionId}/revoke` | `{}` | 204 or JSON acknowledgement |
| GET `/mcp-definitions` | none | `{mcp_versions:[McpVersion]}` |
| GET `/projects/{at}/mcp-bindings` | none | `{mcp_bindings:[Binding]}` (`bindings` alias accepted) |
| POST `/projects/{at}/mcp-bindings` | BindingInput | Binding |
| PATCH `/projects/{at}/mcp-bindings/{bindingId}` | BindingInput | Binding |
| GET `/projects/{at}/harnesses` | none | `{harnesses:[HarnessVersion]}` |
| POST `/projects/{at}/harnesses` | HarnessDefinition | published HarnessVersion |
| POST `/projects/{at}/harnesses/{harnessId}/versions` | HarnessDefinition | published HarnessVersion |

SkillVersion: `{id,skill_id,project_id,name,version,status,files,content_digest}`.
Status is draft/published/revoked. Folder upload requires root SKILL.md,
package-relative safe unique paths, <=100 files and <=2 MiB combined text.
ZIP uses empty files plus base64 archive; server validates extraction and contents.
Admin publication/revocation is separate from upload.
Skill upload name must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, at most
128 characters. BFF and UI share the same schema and never trim/normalize it.

McpVersion: `{id,name,version,config_schema:{properties,required?},
secret_schema:{properties,required?},revoked_at?}`. Each property has `type` and
optional `description`. No command/args/env/executable fields are editable.
BindingInput: `{mcp_version_id,config,secret_refs,enabled}`. Config contains
schema primitives, secret refs have `secret://...` syntax. Binding adds
`id,project_id`. Definitions are read-only for project admins in this feature.
System-admin publication remains outside this UI.

HarnessDefinition: `{name,system_prompt,skill_version_ids,mcp_binding_ids,
runtime_ids}`. HarnessVersion adds `{id,harness_id,project_id,version,
content_digest,revoked_at?}`; `id` is the immutable version ID. Editing uses
`harness_id`, not the old version ID. Defaults: pi, claude-code, codex.
Selectors expose published skills, enabled MCP bindings and allowed runtimes.

## Run Execution

GET `/runtimes` returns `{runtimes:[{id,name,model,available,unavailable_reason?}]}`.
POST `/conversations/{id}/messages` sends `{content,runtime_id,harness_id,
idempotency_key}`; `harness_id` is the selected version ID. Optional URL is
normalized into the goal content, not a separate backend command field.
Response: `{run_id,message_id,...}`.

GET `/runs/{id}`, GET `/runs/{id}/trace`, POST `/runs/{id}/cancel`,
POST `/runs/{id}/reruns` with `{idempotency_key}`, and GET `/runs/{id}/events`
retain first-loop contracts. BFF verifies project ownership before proxying.
SSE and canonical artifact fetches use the same authenticated project headers.

Run optional immutable snapshot:
`{harness_id,harness_version_id,content_digest,skill_version_ids,
mcp_binding_ids,runtime_id,environment_id}` at `harness_snapshot`.
Retry lineage uses `retry_of_run_id`. Native trace ID is
`agenttrace-trace-{runId}`; native Session ID is the conversation ID.

## Takeover And Services

See TAKEOVER-CONTRACT for start/consume/screenshot/input/delete. BFF consumes
the one-time token server-side and returns only a token-free authenticated LF URL.
Screenshot/input requests repeat membership checks and upstream project/actor
headers; backend enforces session identity, expiry and consumed state.

GET `/projects/{at}/services` returns:
`{workers,runs:{status:count},environments:{state:count},active_leases,
expired_leases,lost_attempts,pending_outbox,oldest_outbox_seconds:null|number,
dependencies:{name:{status,mode?,reason?}},stream?,stream_scope?,
dlq_count:null|number,dlq_scope,recent_registry_audit,
observability:{name:{status,scope,value:null|number,reason?,
consumer_group?,topic?,observed_at?}}}`.
Worker: `{id,role,project_id?,version,capacity,in_use,draining,alive,
last_seen_at,runtime_ids?}`. BFF strips internals and normalizes metrics for UI.
`dlq_count` measures retained project outbox handoffs, not Kafka queue depth.
Kafka lag is deployment consumer-group scoped, not project lag. Unmeasured
values remain null with status/scope/reason, never synthetic zero.
Audit entries contain `{id,project_id,actor_id,action,resource_id,occurred_at}`;
BFF retains only id/action/resource/time for display. Additions are optional in
legacy normalized responses; absence does not manufacture measurements.
PATCH `/projects/{at}/workers/{workerId}` sends **`{draining:boolean}`** and
returns Worker. The UI action is called drain/resume; its wire field is draining.
Shared deployment workers without project_id remain read-only for project admins.
