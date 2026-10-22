# Operations

## Health and normal operation

Start the standard local stack with:

```bash
docker compose up -d --build
curl -s http://127.0.0.1:8080/api/v1/health
```

`GET /api/v1/health` returns an overall `ok`, `degraded`, or `down` status and
independent dependency entries for `postgres`, `kafka`, `redis`, `worker`, and
`langfuse`. PostgreSQL is required: its failure returns HTTP 503 and `down`.
Kafka, Redis, and Worker can make the response `degraded` when unavailable.
When Redis is down, the response marks it with `mode: "postgres_poll"` because
SSE falls back to durable PostgreSQL events.

The API currently has no probe for the profile-gated Langfuse exporter, so its
independent `langfuse` field is always `disabled`. Do not interpret API health
as an exporter availability check and do not describe an exporter outage as
causing API health to become `degraded`.

Inspect services and logs without destroying evidence:

```bash
docker compose ps
docker compose logs --tail=200 api runtime worker
```

## Durable delivery and recovery

API writes a run transition and an outbox row in one PostgreSQL transaction.
The API/runtime relays publish pending rows to Kafka. Check backlog from the
PostgreSQL container:

```bash
docker compose exec -T postgres psql -U agenttrace -d agenttrace -c \
  "SELECT topic, COUNT(*) AS pending
   FROM outbox_events
   WHERE published_at IS NULL
   GROUP BY topic
   ORDER BY topic;"
```

Kafka's retained delivery topics are `agenttrace.run.commands.v1` and
`agenttrace.run.events.v1`; failures are sent to
`agenttrace.run.dlq.v1`. Inspect a DLQ record before retrying or changing
state:

```bash
docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic agenttrace.run.dlq.v1 --from-beginning --timeout-ms 5000
```

Restart the runtime after an interruption. PostgreSQL checkpoints and Kafka
consumer-group offsets let it resume safely:

```bash
docker compose restart runtime
docker compose logs --tail=200 runtime
```

Redis is live-experience acceleration only. Its outage does not erase facts or
cancel a run; SSE readers poll PostgreSQL until Redis is restored:

```bash
docker compose restart redis
```

The optional Langfuse exporter is asynchronous. Start it only under the
`langfuse` profile with valid local environment values:

```bash
docker compose --profile langfuse up -d exporter
docker compose logs --tail=200 exporter
```

Diagnose an exporter issue from its own retry state, durable DLQ handoff, and
Kafka consumer state. These paths never change an already completed local run:

```bash
docker compose exec -T postgres psql -U agenttrace -d agenttrace -c \
  "SELECT event_id, attempt_count, updated_at
   FROM consumer_retry_counts
   WHERE consumer = 'agenttrace-langfuse-exporter'
   ORDER BY updated_at DESC;"

docker compose exec -T postgres psql -U agenttrace -d agenttrace -c \
  "SELECT event_id, key, created_at, published_at, attempt_count, last_error
   FROM outbox_events
   WHERE topic = 'agenttrace.run.dlq.v1'
   ORDER BY created_at DESC;"

docker compose exec -T kafka /opt/kafka/bin/kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group agenttrace-langfuse-exporter --describe
```

## Data reset

The following command is destructive for this Compose project: it stops the
stack and removes its PostgreSQL, artifact, and Node volumes. It permanently
removes all local projects, conversations, runs, screenshots, and Kafka state.

```bash
docker compose down -v
```

Only run that command from this repository when a full clean-state reset is
intended. Do not use it against another Compose project or a shared database.
Bring back a clean local environment with:

```bash
docker compose up -d --build
```

`run_v3_tests.py` does not reset volumes. It creates and drops only a uniquely
named `agenttrace_test_v3_runner_*` PostgreSQL database for Go integration
tests, leaving the default `agenttrace` database and logs intact. Destructive
Redis integration tests run only inside the Compose `api` container against
`redis:6379`, database `15`; they reject the default Redis database and any
host-provided address. Do not override either test target with a live service.
