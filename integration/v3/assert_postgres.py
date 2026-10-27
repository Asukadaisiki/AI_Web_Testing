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
            (assistant_content,) = _one(
                cursor,
                """
                SELECT content
                FROM messages
                WHERE conversation_id = %s AND role = 'assistant'
                ORDER BY ordinal DESC
                LIMIT 1
                """,
                (conversation_id,),
            )
            if assistant_content != "Final: Alpha x 2 is in the cart.":
                raise AssertionError(
                    f"expected canonical Pi final assistant message, got {assistant_content!r}"
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

            (
                root_spans,
                completed_model_spans,
                browser_operation_spans,
                finish_spans,
            ) = _one(
                cursor,
                """
                SELECT
                  count(*) FILTER (WHERE kind = 'run'),
                  count(*) FILTER (
                    WHERE kind = 'model'
                      AND status = 'succeeded'
                      AND ended_at IS NOT NULL
                  ),
                  count(*) FILTER (
                    WHERE kind = 'operation'
                      AND name LIKE 'mcp__browser_harness__%%'
                  ),
                  count(*) FILTER (
                    WHERE kind = 'operation'
                      AND name = 'mcp__browser_harness__finish_case'
                  )
                FROM spans
                WHERE run_id = %s
                """,
                (run_id,),
            )
            if (
                root_spans != 1
                or completed_model_spans < 1
                or browser_operation_spans < 1
                or finish_spans != 1
            ):
                raise AssertionError(
                    "expected one root, completed model spans, browser MCP operations, "
                    f"and one finish span; got root={root_spans}, "
                    f"models={completed_model_spans}, operations={browser_operation_spans}, "
                    f"finish={finish_spans}"
                )

            (
                finish_results,
                evidence_with_screenshot,
                evidence_with_steps,
            ) = _one(
                cursor,
                """
                SELECT
                  count(*),
                  count(*) FILTER (
                    WHERE operation.artifacts ? 'screenshot_paths'
                      AND jsonb_array_length(artifacts -> 'screenshot_paths') > 0
                  ),
                  count(*) FILTER (
                    WHERE jsonb_typeof(operation.result -> 'steps') = 'array'
                      AND jsonb_array_length(operation.result -> 'steps') > 0
                  )
                FROM operation_results AS operation
                JOIN span_attempts AS attempt
                  ON attempt.id = operation.span_attempt_id
                JOIN spans AS span ON span.id = attempt.span_id
                WHERE operation.run_id = %s
                  AND operation.status = 'passed'
                  AND span.name = 'mcp__browser_harness__finish_case'
                """,
                (run_id,),
            )
            if (finish_results, evidence_with_screenshot, evidence_with_steps) != (
                1,
                1,
                1,
            ):
                raise AssertionError(
                    "expected one linked passed finish result with screenshot metadata "
                    f"and dry-run steps, got operations={finish_results}, "
                    f"screenshots={evidence_with_screenshot}, steps={evidence_with_steps}"
                )

            (usage_records, total_tokens) = _one(
                cursor,
                """
                SELECT count(*), coalesce(sum(usage.total_tokens), 0)
                FROM usage_records AS usage
                JOIN span_attempts AS attempt ON attempt.id = usage.span_attempt_id
                JOIN spans AS span ON span.id = attempt.span_id
                WHERE usage.run_id = %s AND span.kind = 'model'
                """,
                (run_id,),
            )
            if usage_records < 1 or total_tokens <= 0:
                raise AssertionError(
                    "expected positive model token usage, "
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
                    WHERE payload ->> 'event_type' = 'harness.completed'
                  ),
                  count(*) FILTER (
                    WHERE payload ->> 'event_type' = 'harness.completed'
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
