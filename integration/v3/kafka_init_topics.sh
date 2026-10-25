#!/usr/bin/env bash
set -euo pipefail

topics_bin="${AGENTTRACE_KAFKA_TOPICS_BIN:-/opt/kafka/bin/kafka-topics.sh}"
bootstrap_server="${AGENTTRACE_KAFKA_BOOTSTRAP_SERVER:-kafka:9092}"
partition_count="${AGENTTRACE_KAFKA_PARTITIONS:-8}"
topics="${AGENTTRACE_KAFKA_TOPICS:-agenttrace.run.commands.v1 agenttrace.run.events.v1 agenttrace.run.dlq.v1 agenttrace.test.commands.v1}"

if [[ ! "$partition_count" =~ ^[1-9][0-9]*$ ]]; then
  echo "AGENTTRACE_KAFKA_PARTITIONS must be a positive integer" >&2
  exit 2
fi

for topic in $topics; do
  for attempt in $(seq 1 12); do
    if "$topics_bin" \
      --bootstrap-server "$bootstrap_server" \
      --create \
      --if-not-exists \
      --topic "$topic" \
      --partitions "$partition_count" \
      --replication-factor 1; then
      break
    fi
    if [[ "$attempt" -eq 12 ]]; then
      exit 1
    fi
    sleep 2
  done

  description="$(
    "$topics_bin" \
      --bootstrap-server "$bootstrap_server" \
      --describe \
      --topic "$topic"
  )"
  current_partitions="$(
    printf '%s\n' "$description" |
      awk '{
        for (field = 1; field <= NF; field++) {
          if ($field == "PartitionCount:") {
            print $(field + 1)
            exit
          }
        }
      }'
  )"
  if [[ ! "$current_partitions" =~ ^[1-9][0-9]*$ ]]; then
    echo "could not determine partition count for topic $topic" >&2
    exit 1
  fi
  if (( current_partitions < partition_count )); then
    "$topics_bin" \
      --bootstrap-server "$bootstrap_server" \
      --alter \
      --topic "$topic" \
      --partitions "$partition_count"
  fi
done
