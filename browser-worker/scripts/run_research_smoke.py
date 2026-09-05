"""Run one structured research goal directly against the Browser Worker.

This is an isolated benchmark utility, not an official platform execution path.
Official runs must continue to use the Go control plane and its approval gate.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, Callable

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from app.runners.playwright_runner import execute_case_with_playwright
from app.schemas.dsl import DSLCase
from app.schemas.executions import StepExecutionEvidence


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_GOAL = (
    REPOSITORY_ROOT
    / "research"
    / "goals"
    / "automationexercise-blue-top-cart.json"
)

Executor = Callable[..., list[StepExecutionEvidence]]


def load_goal(path: Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("schema_version") != "research.goal.v1":
        raise ValueError("goal.schema_version must be research.goal.v1")
    for field in ("id", "objective", "target", "success_criteria", "dsl_case"):
        if not payload.get(field):
            raise ValueError(f"goal.{field} is required")
    DSLCase.model_validate(payload["dsl_case"])
    return payload


def run_goal(
    goal: dict[str, Any],
    *,
    execution_id: int,
    executor: Executor = execute_case_with_playwright,
) -> dict[str, Any]:
    case = DSLCase.model_validate(goal["dsl_case"])
    started_at = datetime.now(UTC)
    started = time.perf_counter()
    evidence = executor(
        case=case,
        execution_id=execution_id,
        base_url=case.base_url,
    )
    duration_ms = round((time.perf_counter() - started) * 1000)
    passed_steps = sum(step.status == "passed" for step in evidence)
    failed_steps = [step for step in evidence if step.status == "failed"]
    complete = len(evidence) == len(case.steps)
    successful = complete and not failed_steps
    step_durations = [
        step.duration_ms for step in evidence if step.duration_ms is not None
    ]

    return {
        "schema_version": "research.smoke-result.v1",
        "goal_id": goal["id"],
        "hypothesis": goal.get("hypothesis"),
        "execution_mode": "worker_baseline",
        "started_at": started_at.isoformat(),
        "finished_at": datetime.now(UTC).isoformat(),
        "success": successful,
        "metrics": {
            "task_success": successful,
            "execution_success": successful,
            "verification_success": successful,
            "passed_steps": passed_steps,
            "total_steps": len(case.steps),
            "recorded_steps": len(evidence),
            "average_step_duration_ms": (
                round(sum(step_durations) / len(step_durations))
                if step_durations
                else 0
            ),
            "duration_ms": duration_ms,
            "recovery_count": sum(bool(step.click_recovery) for step in evidence),
            "vision_calls": sum(bool(step.vlm_preverify_used) for step in evidence),
        },
        "failure": (
            {
                "step_index": failed_steps[0].step_index,
                "action": failed_steps[0].action,
                "target": failed_steps[0].target,
                "error_message": failed_steps[0].error_message,
            }
            if failed_steps
            else None
        ),
        "steps": [step.model_dump(mode="json") for step in evidence],
    }


def default_output_path(goal_id: str) -> Path:
    timestamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    return REPOSITORY_ROOT / "research" / "results" / f"{goal_id}-{timestamp}.json"


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Run a structured research smoke goal with the Browser Worker."
    )
    parser.add_argument("--goal", type=Path, default=DEFAULT_GOAL)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--execution-id", type=int, default=int(time.time()))
    args = parser.parse_args()

    goal = load_goal(args.goal)
    output_path = args.output or default_output_path(goal["id"])
    result = run_goal(goal, execution_id=args.execution_id)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(json.dumps({"output": str(output_path), **result["metrics"]}, indent=2))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
