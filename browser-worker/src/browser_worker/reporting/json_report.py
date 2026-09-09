"""Helpers to normalize execution reports."""

from __future__ import annotations

from typing import Literal

from browser_worker.contracts.executions import ExecutionReport, StepExecutionEvidence


def build_execution_report(
    *,
    status: str,
    steps: list[StepExecutionEvidence],
    dsl_profile: Literal["legacy-v1", "research-v1", "research-v2"] | None = None,
) -> ExecutionReport:
    return ExecutionReport(
        status=status,
        dsl_profile=dsl_profile,
        steps=steps,
    )
