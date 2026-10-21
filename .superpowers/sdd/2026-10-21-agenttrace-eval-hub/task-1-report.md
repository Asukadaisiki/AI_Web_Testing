# Task 1 Report: Isolated v3 Baseline and Local Infrastructure

## Implementation Summary

Created the v3 local-infrastructure baseline without changing v2 behavior:

- Added typed `config.Load()` configuration for PostgreSQL, Kafka, optional Redis,
  worker URL, artifacts, HTTP address, Kafka topics/group, and runtime concurrency.
- Added v3 API and runtime command skeletons. They validate configuration and then
  clearly report that later v3 tasks must wire process dependencies.
- Added Compose services `postgres`, `redis`, `kafka`, `worker`, `api`, `runtime`,
  and `web`. PostgreSQL 16, Redis 7, and Kafka 3.9 KRaft have health checks;
  API and Runtime wait for healthy PostgreSQL and Kafka.
- Added an environment example and renamed the web package for AgentTrace Eval Hub v3.
- Pinned the requested Go dependencies: pgx v5, franz-go, and go-redis v9.

Commit:

```text
37489f7 build: establish AgentTrace v3 infrastructure
```

## Files Changed

- `.env.example`
- `compose.yaml`
- `backend/go.mod`
- `backend/go.sum`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/cmd/agenttrace-api/main.go`
- `backend/cmd/agenttrace-runtime/main.go`
- `web/package.json`

## TDD RED Evidence

Test file was created before `config.Load()` existed:

```text
$ cd backend && go test ./internal/config -run TestLoad -v
# github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config [github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config.test]
internal/config/config_test.go:8:15: undefined: Load
internal/config/config_test.go:17:14: undefined: Load
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config [build failed]
FAIL
```

Exit status: `1`, as expected because the new configuration interface did not yet
exist.

## TDD GREEN Evidence

After implementing `config.Load()`:

```text
$ cd backend && go test ./internal/config -v
=== RUN   TestLoadRequiresPostgresAndKafka
--- PASS: TestLoadRequiresPostgresAndKafka (0.00s)
=== RUN   TestLoadAllowsRedisToBeDisabled
--- PASS: TestLoadAllowsRedisToBeDisabled (0.00s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config
```

## Test Commands and Results

| Command | Result |
| --- | --- |
| `cd backend && go test ./internal/config -run TestLoad -v` | RED: exit 1, `Load` undefined |
| `cd backend && go test ./internal/config -v` | PASS: 2 tests |
| `cd backend && go test ./cmd/agenttrace-api ./cmd/agenttrace-runtime -v` | PASS: both command packages compile |
| `cd backend && go test ./...` | PASS: v2 and v3 Go packages |
| `docker compose config --quiet` | PASS: exit 0 |
| `cd web && npm run build --cache /tmp/agenttrace-npm-cache` | PASS |
| `AGENTTRACE_POSTGRES_DSN=... AGENTTRACE_KAFKA_BROKERS=... go run ./cmd/agenttrace-api` | Expected exit 1 with `agenttrace-api is not wired yet; complete later v3 tasks before starting it` |

## Self-Review

- Verified the exact required public configuration structs and `Load()` signature exist.
- Verified `AGENTTRACE_POSTGRES_DSN` and `AGENTTRACE_KAFKA_BROKERS` are required
  configuration only. `Load()` does not attempt Kafka reachability.
- Verified an empty `AGENTTRACE_REDIS_ADDR` yields `Redis.Enabled == false`.
- Verified Compose has all seven required service names and API/Runtime only gate on
  PostgreSQL and Kafka health, not Redis.
- Verified `git diff --check` before commit.
- Verified no secret file was read, printed, or modified.
- No subagents were used, per task instruction.

## Concerns

- The requested `go get ...@latest` commands selected versions requiring Go 1.26, so
  `backend/go.mod` was upgraded from Go 1.24 to Go 1.26. Local verification used
  Go 1.26.4 and the Compose Go image is `golang:1.26`.
- The API and runtime commands intentionally terminate after successful configuration
  loading until Tasks 2-8 introduce database, Kafka, Redis, and HTTP/runtime wiring.
- Compose syntax was validated, but services were not started because Task 1 does not
  require live dependency behavior; Kafka reachability is explicitly outside this
  task's API acceptance scope.

## Fix Round 1/5

### Review Findings Addressed

- Restored `go 1.24` and changed API and runtime Compose images to `golang:1.24`.
- Pinned `github.com/jackc/pgx/v5 v5.8.0`,
  `github.com/twmb/franz-go v1.20.7`, and
  `github.com/redis/go-redis/v9 v9.20.1`. The Go 1.24 module graph selected
  `golang.org/x/sys v0.30.0` instead of the incompatible v0.48.0.
- Corrected the Kafka defaults in `config.Load()` and `.env.example` to:
  `agenttrace.run.commands.v1`, `agenttrace.run.events.v1`, and
  `agenttrace.run.dlq.v1`.
- Added a `web-node-modules` named volume and made the Web command run
  `npm ci --cache /tmp/npm-cache` before Vite.

### Files Changed

- `.env.example`
- `compose.yaml`
- `backend/go.mod`
- `backend/go.sum`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`

### TDD Evidence

The new topic-default test was written before changing `config.Load()`.

RED command:

```text
$ go test ./internal/config -run TestLoadUsesRunTopicDefaults -v
=== RUN   TestLoadUsesRunTopicDefaults
    config_test.go:39: command topic = "agenttrace.run-command.v1", want "agenttrace.run.commands.v1"
--- FAIL: TestLoadUsesRunTopicDefaults (0.00s)
FAIL
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config	0.996s
FAIL
```

Exit status: `1`.

GREEN command:

```text
$ GOTOOLCHAIN=go1.24.11 go test ./internal/config -count=1 -v
=== RUN   TestLoadRequiresPostgresAndKafka
--- PASS: TestLoadRequiresPostgresAndKafka (0.00s)
=== RUN   TestLoadAllowsRedisToBeDisabled
--- PASS: TestLoadAllowsRedisToBeDisabled (0.00s)
=== RUN   TestLoadUsesRunTopicDefaults
--- PASS: TestLoadUsesRunTopicDefaults (0.00s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config	0.470s
```

### Verification

```text
$ GOTOOLCHAIN=go1.24.11 go version
go version go1.24.11 darwin/arm64

$ GOTOOLCHAIN=go1.24.11 go list -m -f '{{.Path}} {{.Version}} {{.GoVersion}}' github.com/jackc/pgx/v5 github.com/twmb/franz-go github.com/redis/go-redis/v9 golang.org/x/sys
github.com/jackc/pgx/v5 v5.8.0 1.24.0
github.com/twmb/franz-go v1.20.7 1.24.0
github.com/redis/go-redis/v9 v9.20.1 1.24
golang.org/x/sys v0.30.0 1.18

$ GOTOOLCHAIN=go1.24.11 go test ./...
PASS: all backend packages; no failures.

$ GOTOOLCHAIN=go1.24.11 go mod verify
all modules verified

$ docker compose config --quiet
exit status: 0

$ npm ci --cache /tmp/agenttrace-npm-cache --ignore-scripts
added 73 packages, and audited 74 packages in 3s

$ npm run build --cache /tmp/agenttrace-npm-cache
> tsc --noEmit && vite build
✓ 46 modules transformed.
✓ built in 492ms
```

Focused clean-container command attempted:

```text
$ docker compose run --rm --no-deps web sh -c 'npm ci --cache /tmp/npm-cache && ./node_modules/.bin/vite --version'
Image node:22-alpine Error failed to resolve reference "docker.io/library/node:22-alpine":
failed to do request: Head "https://registry-1.docker.io/v2/library/node/manifests/22-alpine":
dial tcp 54.235.133.183:443: i/o timeout
```

The command used the named `web-node-modules` volume, so it does not depend on
host `node_modules`; it could not reach its `npm ci` step because Docker Hub was
unreachable. No subagents were used.

### Commit

```text
798ba43 fix: align v3 infrastructure compatibility
```

### Concern

- Container-level Web startup remains unverified only because the environment timed
  out resolving `node:22-alpine` from Docker Hub. Compose syntax, the exact startup
  command, lockfile installation, and Vite production build were verified.
