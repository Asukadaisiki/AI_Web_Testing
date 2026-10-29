# Native Local Langfuse

This runs the real Langfuse web application and ingestion worker on macOS arm64.
No Docker, Colima, VM, mock Langfuse, or substitute S3 implementation is used.
It does not stop or reconfigure the existing PostgreSQL, Redis, Kafka, or old
Langfuse processes. No Langfuse checkout `.env` changes are needed.

## Prerequisites

- Native PostgreSQL at `127.0.0.1:5432`, with permission to create a new database.
- Native Redis at `127.0.0.1:6379`, supporting BullMQ.
- Node 24 and an installed Langfuse checkout in the sibling directory
  `../langfuse-agenttrace`, or set `LANGFUSE_NATIVE_SOURCE` to its location.
- Langfuse commit `05fe6fe3c310299f4eb3a19989fc14c9a931878f`, version `3.225.14`,
  installed `node_modules`, built shared package, and installed pnpm `11.10.0`.
  `build` prepares a fresh production directory; `up` uses the selected build.
- Native Homebrew binaries:

```bash
brew install --cask clickhouse
brew install minio golang-migrate
```

Installed for this acceptance run: ClickHouse `26.10.2.103`,
MinIO `RELEASE.2025-10-15T17-29-55Z`, golang-migrate `4.20.1`, Node `24.18.0`.
MinIO's upstream is archived and its old direct binary URL returns HTTP 410;
the Homebrew arm64 bottle still supplies the real MinIO server.

To prepare a new checkout, run these commands from this project's root.
The checkout and builds belong to Langfuse and are kept outside this repository:

```bash
git clone https://github.com/langfuse/langfuse.git ../langfuse-agenttrace
git -C ../langfuse-agenttrace checkout 05fe6fe3c310299f4eb3a19989fc14c9a931878f
corepack enable
cd ../langfuse-agenttrace
pnpm install --frozen-lockfile
pnpm --filter @langfuse/shared run db:generate
pnpm --filter @langfuse/shared run build
cd ../AI_Web_Testing
```

Use Node 24 and pnpm `11.10.0`, pinned by upstream `packageManager`.
These commands prepare dependencies only; private runtime configuration and
migrations are supplied by `setup`, then the native helper builds below.

## Setup And Fresh Build

From the AI_Web_Testing repository, keep the same private native root/source and
Node 24 PATH for all commands. The coordinator's selected fresh target is
`.next-first-loop-20261028-a` (verified absent before selection). Do not reuse it
after a build attempt; select another fresh name for a subsequent attempt.

```bash
export LANGFUSE_NATIVE_NEXT_DIST_DIR=.next-first-loop-20261028-a
bash integration/local/langfuse.sh setup
bash integration/local/langfuse.sh build
```

`build` is synchronous and uses the existing private `env(config())`; it
requires setup credentials, Node major 24, and installed pnpm `11.10.0`.
The dist must be an explicit single web-local basename containing only ASCII
letters, digits, dots, underscores or hyphens, not `.next`, `.` or `..`.
Existing files/directories and symlinks are refused. It never overwrites the
live `.next` tree or deletes a failed build directory.

It runs `pnpm exec next build` in the checkout's `web`, then Node 24 runs
`scripts/scan-client-bundle.mjs` against `web/<selected-dist>/static` from the
checkout root. Either command failing prevents success. Both commands log
privately to `<native-root>/build.log`; diagnostics never echo child output,
secret argv, or environment. A successful message names only `web/<selected-dist>`.
The upstream dotenv build wrapper is not used and the checkout's root `.env`
is not sourced or modified. Real upstream environment/type validation stays
enabled: do not use `DOCKER_BUILD`, `SKIP_ENV_VALIDATION` or
`NEXT_IGNORE_BUILD_ERRORS`. Inherited legacy `NEXT_PUBLIC_*`, Sentry and
cloud build values are filtered; native cloud mode and Sentry upload credentials
are disabled. `INLINE_RUNTIME_CHUNK=false` retains the upstream build setting.
Build does not migrate/provision data or start/stop services.

`setup` generates private credentials once, creates only
`agenttrace_langfuse_local`, starts dedicated ClickHouse and MinIO, creates the
real S3 buckets, deploys the upstream Prisma migrations, and runs the upstream
unclustered ClickHouse migrations with golang-migrate. It deliberately does not
invoke the checkout's ClickHouse shell script, which sources its legacy `.env`.
Rerunning `setup` preserves the existing credentials and databases.

For an existing stack, the coordinator first drains active and pending Go work
to `0/0`, then performs owned shutdown/startup with the same selected dist:

```bash
# Only after the operator confirms the Go drain and fresh build/scan success:
bash integration/local/langfuse.sh down
bash integration/local/langfuse.sh up
bash integration/local/langfuse.sh status
```

The launcher does not implement a Go drain. Do not restart while work is active,
and do not repeat setup merely to switch builds. On a first installation without
owned running processes, `up` follows setup and fresh build directly.

`up` starts the real worker from its TypeScript entrypoint and Next.js from its
selected production build via `LANGFUSE_NATIVE_NEXT_DIST_DIR`. Node 24 is
required. Environment variables are supplied directly by the launcher.
It waits for worker and web health endpoints. Native builds do not rely on
`NEXT_PUBLIC_LANGFUSE_RUN_NEXT_INIT`, so `setup` and `up` explicitly
run `langfuse.provision.cjs`. That bootstrap mirrors upstream `web/src/initialize.ts`
using the actual installed Prisma client, upstream `createAndAddApiKeysToDb`,
upstream cache invalidation, and Langfuse's bcrypt-12 owner password convention.
It creates a real organization, project, owner, and API keys in the dedicated
PostgreSQL database. No Langfuse behavior or API is replaced.

## Synthetic Health Verify

Optional generic health verification, separate from first-loop acceptance:

```bash
bash integration/local/langfuse.sh verify
```

`verify` writes synthetic ingestion health evidence. It checks all four HTTP
health endpoints, authenticates the real owner
through NextAuth's CSRF/credentials/session endpoints, sends a trace and span through
`POST /api/public/ingestion`, requires HTTP 207 with two successful events,
polls `GET /api/public/traces/{id}` until both persist, and checks that the real
MinIO event bucket contains ingestion objects. It prints only sanitized
evidence and writes `verification.json` outside the workspace.
Those synthetic trace/span writes are not actual Agent runs or SDK/UI acceptance.
The first-loop SDK/UI acceptance must not call `verify` or fabricate traces.

## First-Loop Binding

The native operate driver supplies these five values privately to the Langfuse
server for both build and up. They are server-only BFF configuration, never
`NEXT_PUBLIC_*`, browser credentials, or values to paste into logs:

| Value | Binding |
| --- | --- |
| `AGENTTRACE_API_URL` | Native Go API URL, reachable by the Langfuse server |
| `AGENTTRACE_BRIDGE_TOKEN` | Private bearer token shared with the Go API |
| `AGENTTRACE_LANGFUSE_PROJECT_ID` | Authorized Langfuse project, `agenttrace-native-project` for this stack |
| `AGENTTRACE_PROJECT_ID` | Bound AgentTrace project ID |
| `AGENTTRACE_CONVERSATION_ID` | Bound AgentTrace conversation ID |

The Go API must receive the same `AGENTTRACE_BRIDGE_TOKEN`,
`AGENTTRACE_PROJECT_ID` and `AGENTTRACE_CONVERSATION_ID`; partial or mismatched
bindings are not valid. The exporter must additionally receive
`AGENTTRACE_EXPORT_PROJECT_ID=<bound AgentTrace project ID>` to restrict ingestion
to that project. This is distinct from `AGENTTRACE_LANGFUSE_PROJECT_ID`, which is
the Langfuse destination project. The generated `agenttrace.env` below supplies
destination credentials only, not the five BFF values or exporter project filter.
The launcher preserves privately supplied `AGENTTRACE_*` values; it does not
generate or provision Go bindings.

This first release binds one project and one conversation and uses
installation-level Harnesses. In the bound Langfuse project, open **Agent runs**,
enter a prompt and optional URL, choose Runtime and Harness, and submit.
Runtime chooses the execution engine; Harness chooses the installed behavior
configuration. Observe real run events and completion; cancel an active run,
rerun a failed run, and follow the native **Trace** link once actual SDK events
have been ingested and exported. The optional URL is a run input, not a requirement
for every Harness. These operations are exercised through the authenticated
Langfuse BFF and the bound Go API, not a generic synthetic ingestion probe.

## Integration Interface

```bash
set -a
source /tmp/agenttrace-native/langfuse/agenttrace.env
set +a
# Start the project's API/exporter using its normal local command here.
```

The private environment file provides:

```text
AGENTTRACE_LANGFUSE_ENABLED=true
AGENTTRACE_LANGFUSE_BASE_URL=http://127.0.0.1:3300
AGENTTRACE_LANGFUSE_PUBLIC_KEY=<generated>
AGENTTRACE_LANGFUSE_SECRET_KEY=<generated>
AGENTTRACE_LANGFUSE_PROJECT_ID=agenttrace-native-project
LANGFUSE_HOST=http://127.0.0.1:3300
LANGFUSE_PUBLIC_KEY=<same generated public key>
LANGFUSE_SECRET_KEY=<same generated secret key>
```

The web UI is `http://localhost:3300`. The headless owner email is
`agenttrace-local@example.test`; the generated login password is the
`userPassword` field in the private runtime `credentials.json`.
Do not put the runtime credentials in the repository or paste them into logs.

| Service | Address | Isolation |
| --- | --- | --- |
| Langfuse web | `127.0.0.1:3300` | Separate process, own environment |
| Langfuse worker | `127.0.0.1:3330` | Separate process, own environment |
| PostgreSQL | `127.0.0.1:5432` | Database `agenttrace_langfuse_local` |
| Redis | `127.0.0.1:6379/13` | Key/queue prefix `agenttrace-native-langfuse` |
| ClickHouse HTTP | `127.0.0.1:8123` | Dedicated native process and data directory |
| ClickHouse native | `127.0.0.1:9000` | Database `agenttrace_langfuse_local` |
| MinIO S3 | `127.0.0.1:9090` | Dedicated native process and data directory |
| MinIO console | `127.0.0.1:9091` | Loopback only |

Langfuse runtime environment uses `CLICKHOUSE_CLUSTER_ENABLED=false`,
`LANGFUSE_RATE_LIMITS_ENABLED=false` for this loopback acceptance stack,
English locale and UTC to keep upstream HTTP date headers ASCII,
`CLICKHOUSE_USER=agenttrace`, generated password, `REDIS_CONNECTION_STRING`
as above, `NEXTAUTH_URL=http://localhost:3300`, and generated `NEXTAUTH_SECRET`,
`SALT`, and 32-byte hex `ENCRYPTION_KEY`. S3 event/media upload endpoints are
`http://127.0.0.1:9090`, region `us-east-1`, path-style enabled, with private
MinIO credentials. Buckets are `agenttrace-langfuse-events` and
`agenttrace-langfuse-media`. ClickHouse's empty-password default user is
loopback-only and used only for local administrative migrations.

## Runtime And Stop

Runtime files default to `/tmp/agenttrace-native/langfuse`: credentials,
exporter environment, PID records, logs, verification evidence, and
ClickHouse/MinIO data. Credentials/logs are private, generated files have mode
0600, and newly created runtime directories have mode 0700. `/tmp` is not a
durable storage guarantee. For persistence choose `LANGFUSE_NATIVE_ROOT` before
the first setup, then keep the same value for every command.

```bash
bash integration/local/langfuse.sh down
```

`down` sends SIGTERM only to launcher-owned process groups after checking the
saved PID, locale-stable start time, and command. It leaves PostgreSQL, Redis,
Kafka, the old Langfuse services, installed packages, and all data intact.
Port conflicts cause a failure, never termination of an unknown service.

Optional launcher variables:

- `LANGFUSE_NATIVE_ROOT`: runtime directory.
- `LANGFUSE_NATIVE_SOURCE`: installed checkout location, still revision-checked.
- `LANGFUSE_NATIVE_NEXT_DIST_DIR`: explicit fresh web-local directory for `build`;
  the same selected directory for `up` (default `.next` only for legacy startup).
- `LANGFUSE_NATIVE_DATABASE_URL`: PostgreSQL credentials on first setup only.
  It must target `agenttrace_langfuse_local` on `127.0.0.1:5432`; default uses the
  current OS username without a password. Do not pass secret URLs in shell
  history.

Troubleshoot using the private `clickhouse.log`, `minio.log`, `worker.log`,
`web.log`, `build.log`, `provision.log`, and `migrations.log`. No Kafka connection is required by Langfuse;
Kafka remains owned by the main project's local stack.

Launcher build/ownership regression checks use temporary fake checkouts and
test-owned processes only; they do not touch running native services:

```bash
# Use Node 24:
node --test integration/local/langfuse.test.mjs
```

## Historical Health Evidence

The earlier generic synthetic health probe, not first-loop SDK/UI acceptance:
on `2026-10-28T04:41:44Z`, the native stack passed all four health checks, real
owner login, HTTP 207 ingestion with two accepted events, authenticated
trace/span retrieval, and real MinIO object persistence. Trace:
`80847f77-2e3f-4bdf-bbdd-6b0ae6a9324a`; span:
`94f8ad5a-f0a8-4ada-bb22-33dca64689b3`. Four launcher ownership tests passed.
An idempotent second `setup` completed without changing credentials or stopping
running services. The original web/worker PIDs `37735`/`93316` remained running.
