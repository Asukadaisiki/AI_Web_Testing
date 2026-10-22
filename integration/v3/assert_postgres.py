"""PostgreSQL assertions for one canonical AgentTrace v3 run."""

from __future__ import annotations

from typing import Any


def _one(cursor: Any, query: str, values: tuple[object, ...]) -> tuple[Any, ...]:
    cursor.execute(query, values)
    row = cursor.fetchone()
    if row is None:
        raise AssertionError(f"query returned no row: {query}")
    return tuple(row)


def assert_canonical_run(
    dsn: str,
    *,
    project_id: str,
    conversation_id: str,
    run_id: str,
) -> None:
    """Assert durable state through PostgreSQL, scoped only to this scenario."""
    import psycopg

    with psycopg.connect(dsn) as connection:
        with connection.cursor() as cursor:
            (project_count,) = _one(
                cursor,
                "SELECT count(*) FROM projects WHERE id = %s AND name = 'Storefront QA'",
                (project_id,),
            )
            if project_count != 1:
                raise AssertionError(f"expected one scoped Storefront QA project, got {project_count}")

            (conversation_count,) = _one(
                cursor,
                "SELECT count(*) FROM conversations WHERE id = %s AND project_id = %s",
                (conversation_id, project_id),
            )
            if conversation_count != 1:
                raise AssertionError(f"expected one scoped conversation, got {conversation_count}")

            (user_messages, assistant_messages, all_messages) = _one(
                cursor,
                """
                SELECT
                  count(*) FILTER (WHERE role = 'user'),
                  count(*) FILTER (WHERE role = 'assistant'),
                  count(*)
                FROM messages
                WHERE conversation_id = %s
                """,
                (conversation_id,),
            )
            if (user_messages, assistant_messages, all_messages) != (1, 1, 2):
                raise AssertionError(
                    "expected exactly one user and one assistant message for the "
                    f"scenario conversation, got user={user_messages}, "
                    f"assistant={assistant_messages}, total={all_messages}"
                )

            (run_count, status) = _one(
                cursor,
                """
                SELECT count(*), coalesce(max(status), '')
                FROM runs
                WHERE id = %s AND project_id = %s AND conversation_id = %s
                """,
                (run_id, project_id, conversation_id),
            )
            if (run_count, status) != (1, "succeeded"):
                raise AssertionError(
                    f"expected one succeeded scoped run, got count={run_count}, status={status!r}"
                )

            (root_spans, canonical_shape, report_output) = _one(
                cursor,
                """
                WITH root AS (
                  SELECT span_id
                  FROM spans
                  WHERE run_id = %s AND kind = 'run'
                )
                SELECT
                  (SELECT count(*) FROM root),
                  array_agg(
                    spans.kind || ':' || spans.name
                    ORDER BY spans.started_at, spans.id
                  ) FILTER (
                    WHERE spans.parent_span_id IN (SELECT span_id FROM root)
                  ),
                  string_agg(spans.output::text, '') FILTER (WHERE spans.kind = 'report')
                FROM spans
                WHERE run_id = %s
                """,
                (run_id, run_id),
            )
            expected_shape = ["model:plan", "operation:operation", "report:report", "model:final"]
            if root_spans != 1 or canonical_shape != expected_shape:
                raise AssertionError(
                    "expected canonical Trace shape run -> model plan -> operation -> report -> "
                    f"model final, got root={root_spans}, children={canonical_shape}"
                )
            if "grounded local storefront case passed" not in str(report_output):
                raise AssertionError(f"expected report summary in report span, got {report_output}")

            (operation_results, evidence_with_screenshot) = _one(
                cursor,
                """
                SELECT
                  count(*),
                  count(*) FILTER (
                    WHERE artifacts ? 'screenshot_paths'
                      AND jsonb_array_length(artifacts -> 'screenshot_paths') > 0
                  )
                FROM operation_results
                WHERE run_id = %s AND status = 'succeeded'
                """,
                (run_id,),
            )
            if (operation_results, evidence_with_screenshot) != (1, 1):
                raise AssertionError(
                    "expected one successful browser operation with screenshot metadata, "
                    f"got operations={operation_results}, evidence={evidence_with_screenshot}"
                )

            (usage_records, total_tokens) = _one(
                cursor,
                """
                SELECT count(*), coalesce(sum(total_tokens), 0)
                FROM usage_records
                WHERE run_id = %s
                """,
                (run_id,),
            )
            if usage_records < 2 or total_tokens != 0:
                raise AssertionError(
                    "expected final scripted-model usage records with zero billed tokens, "
                    f"got records={usage_records}, total_tokens={total_tokens}"
                )

            (command_published, runtime_processed) = _one(
                cursor,
                """
                SELECT
                  count(*) FILTER (
                    WHERE published_at IS NOT NULL
                      AND topic = 'agenttrace.run.commands.v1'
                  ),
                  (
                    SELECT count(*)
                    FROM inbox_events
                    WHERE consumer = 'agenttrace-runtime'
                      AND processed_at IS NOT NULL
                      AND event_id IN (
                        SELECT event_id
                        FROM outbox_events
                        WHERE key = %s
                          AND topic = 'agenttrace.run.commands.v1'
                      )
                  )
                FROM outbox_events
                WHERE key = %s
                """,
                (run_id, run_id),
            )
            if command_published < 1 or runtime_processed < 1:
                raise AssertionError(
                    "expected Kafka command publication and runtime inbox processing, "
                    f"got published={command_published}, processed={runtime_processed}"
                )

            (completion_events, unpublished_completion_events) = _one(
                cursor,
                """
                SELECT
                  count(*) FILTER (
                    WHERE payload ->> 'event_type' = 'runtime.completed'
                  ),
                  count(*) FILTER (
                    WHERE payload ->> 'event_type' = 'runtime.completed'
                      AND published_at IS NULL
                  )
                FROM outbox_events
                WHERE key = %s
                """,
                (run_id,),
            )
            if (completion_events, unpublished_completion_events) != (1, 0):
                raise AssertionError(
                    "expected one published completion event and no unpublished completion "
                    f"event, got completion={completion_events}, "
                    f"unpublished={unpublished_completion_events}"
                )
