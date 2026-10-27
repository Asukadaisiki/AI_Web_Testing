"""Run two real-model browser tasks against Automation Exercise."""

from __future__ import annotations

import concurrent.futures
import json
import os
import sys
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from real_provider_load_scenario import (
    LoadConfig,
    RunFixture,
    collect_summary,
    request,
    wait_for_runs,
)


ROOT = Path(__file__).resolve().parents[2]
REPORT_PATH = Path(
    os.environ.get(
        "AGENTTRACE_AUTOMATION_EXERCISE_REPORT",
        str(ROOT / "artifacts/load-test/automation-exercise-report.json"),
    )
)
BASE_URL = "https://automationexercise.com"
STEP_TIMEOUT_MS = 60_000
CONDITION_TIMEOUT_MS = 45_000


@dataclass(frozen=True)
class ScenarioConfig:
    projects: int = 2
    conversations: int = 2
    runs: int = 2
    submit_workers: int = 1
    run_timeout_seconds: int = 1_500
    delivery_timeout_seconds: int = 90

    def as_load_config(self) -> LoadConfig:
        return LoadConfig(
            projects=self.projects,
            conversations_per_project=1,
            runs_per_conversation=1,
            submit_workers=self.submit_workers,
            run_timeout_seconds=self.run_timeout_seconds,
            delivery_timeout_seconds=self.delivery_timeout_seconds,
        )


@dataclass(frozen=True)
class BrowserTask:
    key: str
    title: str
    email: str
    case: dict[str, Any]


@dataclass(frozen=True)
class ScenarioRun:
    task: str
    project_id: str
    conversation_id: str
    run_id: str

    def load_fixture(self) -> RunFixture:
        return RunFixture(self.project_id, self.conversation_id, self.run_id)


def _condition(type_: str, value: str) -> dict[str, Any]:
    return {
        "type": type_,
        "value": value,
        "timeout_ms": CONDITION_TIMEOUT_MS,
    }


def _target(css: str, page_url: str, index: int, hint: str) -> dict[str, Any]:
    return {
        "locator": {"kind": "css", "css": css},
        "hint": hint,
        "grounding": {
            "observation_id": f"automation-exercise-observation-{index}",
            "page_state_id": f"automation-exercise-page-{index}",
            "candidate_id": f"element-{index}",
            "page_url": page_url,
        },
    }


def _goto(steps: list[dict[str, Any]], path: str, ready_text: str) -> None:
    url = f"{BASE_URL}{path}"
    steps.append(
        {
            "index": len(steps),
            "action": "goto",
            "intent": f"Open {url}",
            "value": url,
            "preconditions": [],
            "postconditions": [_condition("text_visible", ready_text)],
            "timeout_ms": 90_000,
        }
    )


def _action(
    steps: list[dict[str, Any]],
    action: str,
    css: str,
    *,
    page_path: str,
    intent: str,
    pre_text: str,
    post_type: str,
    post_value: str,
    value: str | None = None,
    submit: bool = False,
) -> None:
    index = len(steps)
    step: dict[str, Any] = {
        "index": index,
        "action": action,
        "intent": intent,
        "target": _target(css, f"{BASE_URL}{page_path}", index, intent),
        "preconditions": [_condition("text_visible", pre_text)],
        "postconditions": [_condition(post_type, post_value)],
        "timeout_ms": STEP_TIMEOUT_MS,
    }
    if value is not None:
        step["value"] = value
    if submit:
        step["submit"] = True
    steps.append(step)


def _assert_text(
    steps: list[dict[str, Any]], value: str, *, pre_url: str, intent: str
) -> None:
    steps.append(
        {
            "index": len(steps),
            "action": "assert_text",
            "intent": intent,
            "value": value,
            "preconditions": [_condition("url_contains", pre_url)],
            "postconditions": [_condition("text_visible", value)],
            "timeout_ms": STEP_TIMEOUT_MS,
        }
    )


def _input(
    steps: list[dict[str, Any]],
    css: str,
    value: str,
    *,
    page_path: str,
    pre_text: str,
    intent: str,
    submit: bool = False,
    post_type: str = "value_equals",
    post_value: str | None = None,
) -> None:
    _action(
        steps,
        "input",
        css,
        page_path=page_path,
        intent=intent,
        pre_text=pre_text,
        post_type=post_type,
        post_value=post_value or value,
        value=value,
        submit=submit,
    )


def _select(
    steps: list[dict[str, Any]],
    css: str,
    value: str,
    *,
    page_path: str,
    pre_text: str,
    intent: str,
    post_value: str | None = None,
) -> None:
    _action(
        steps,
        "select",
        css,
        page_path=page_path,
        intent=intent,
        pre_text=pre_text,
        post_type="value_equals",
        post_value=post_value or value,
        value=value,
    )


def _click(
    steps: list[dict[str, Any]],
    css: str,
    *,
    page_path: str,
    pre_text: str,
    post_type: str,
    post_value: str,
    intent: str,
) -> None:
    _action(
        steps,
        "click",
        css,
        page_path=page_path,
        intent=intent,
        pre_text=pre_text,
        post_type=post_type,
        post_value=post_value,
    )


def _registration_steps(email: str, name: str) -> list[dict[str, Any]]:
    steps: list[dict[str, Any]] = []
    _goto(steps, "/login", "New User Signup!")
    _input(
        steps,
        "[data-qa='signup-name']",
        name,
        page_path="/login",
        pre_text="New User Signup!",
        intent="Enter the new account name",
    )
    _input(
        steps,
        "[data-qa='signup-email']",
        email,
        page_path="/login",
        pre_text="New User Signup!",
        intent="Enter the unique disposable email",
        submit=True,
        post_type="text_visible",
        post_value="ENTER ACCOUNT INFORMATION",
    )
    _action(
        steps,
        "check",
        "#id_gender1",
        page_path="/signup",
        intent="Select the title",
        pre_text="ENTER ACCOUNT INFORMATION",
        post_type="element_state",
        post_value="checked",
    )
    _input(
        steps,
        "#password",
        "AeDemo!2026",
        page_path="/signup",
        pre_text="ENTER ACCOUNT INFORMATION",
        intent="Set the test account password",
    )
    _select(
        steps,
        "#days",
        "12",
        page_path="/signup",
        pre_text="Date of Birth",
        intent="Select the birth day",
    )
    _select(
        steps,
        "#months",
        "May",
        page_path="/signup",
        pre_text="Date of Birth",
        intent="Select the birth month",
        post_value="5",
    )
    _select(
        steps,
        "#years",
        "1990",
        page_path="/signup",
        pre_text="Date of Birth",
        intent="Select the birth year",
    )
    fields = (
        ("#first_name", "Agent", "Enter the first name"),
        ("#last_name", "Trace", "Enter the last name"),
        ("#company", "AgentTrace QA", "Enter the company"),
        ("#address1", "100 Test Avenue", "Enter the primary address"),
        ("#state", "California", "Enter the state"),
        ("#city", "San Francisco", "Enter the city"),
        ("#zipcode", "94105", "Enter the postal code"),
    )
    for css, value, intent in fields:
        _input(
            steps,
            css,
            value,
            page_path="/signup",
            pre_text="ADDRESS INFORMATION",
            intent=intent,
        )
    _select(
        steps,
        "#country",
        "United States",
        page_path="/signup",
        pre_text="ADDRESS INFORMATION",
        intent="Select the country",
    )
    _input(
        steps,
        "#mobile_number",
        "2025550147",
        page_path="/signup",
        pre_text="ADDRESS INFORMATION",
        intent="Enter the test phone number and create the account",
        submit=True,
        post_type="text_visible",
        post_value="ACCOUNT CREATED!",
    )
    _goto(steps, "/", f"Logged in as {name}")
    return steps


def _account_case(email: str, name: str) -> dict[str, Any]:
    steps = _registration_steps(email, name)
    _goto(steps, "/logout", "Login to your account")
    _input(
        steps,
        "[data-qa='login-email']",
        email,
        page_path="/login",
        pre_text="Login to your account",
        intent="Enter the registered email for login",
    )
    _input(
        steps,
        "[data-qa='login-password']",
        "AeDemo!2026",
        page_path="/login",
        pre_text="Login to your account",
        intent="Enter the account password for login",
        submit=True,
        post_type="text_visible",
        post_value=f"Logged in as {name}",
    )
    _goto(steps, "/delete_account", "ACCOUNT DELETED!")
    _assert_text(
        steps,
        "ACCOUNT DELETED!",
        pre_url="/delete_account",
        intent="Verify that the account was deleted",
    )
    return {
        "case_version": "loop.case.v1",
        "name": "Automation Exercise account lifecycle",
        "goal": (
            "Register a unique account, verify login, log out, log in again, "
            "delete the account, and verify deletion."
        ),
        "base_url": BASE_URL,
        "steps": steps,
    }


def _purchase_case(email: str, name: str) -> dict[str, Any]:
    steps = _registration_steps(email, name)
    _goto(steps, "/products", "ALL PRODUCTS")
    _input(
        steps,
        "#search_product",
        "Blue Top",
        page_path="/products",
        pre_text="ALL PRODUCTS",
        intent="Search for the Blue Top product",
    )
    _goto(steps, "/products?search=Blue%20Top", "SEARCHED PRODUCTS")
    _goto(steps, "/product_details/1", "Blue Top")
    _input(
        steps,
        "#quantity",
        "2",
        page_path="/product_details/1",
        pre_text="Blue Top",
        intent="Set the purchase quantity to two",
    )
    _click(
        steps,
        "button.cart",
        page_path="/product_details/1",
        pre_text="Blue Top",
        post_type="text_visible",
        post_value="Added!",
        intent="Add the selected quantity to the cart",
    )
    _goto(steps, "/view_cart", "Shopping Cart")
    _goto(steps, "/checkout", "Review Your Order")
    _input(
        steps,
        "textarea[name='message']",
        "Automation Exercise end-to-end order validation.",
        page_path="/checkout",
        pre_text="Review Your Order",
        intent="Add an order review note",
    )
    _goto(steps, "/payment", "Payment")
    payment_fields = (
        ("[data-qa='name-on-card']", "Agent Trace", "Enter the cardholder name"),
        ("[data-qa='card-number']", "4242424242424242", "Enter the demo card number"),
        ("[data-qa='cvc']", "123", "Enter the demo card security code"),
        ("[data-qa='expiry-month']", "12", "Enter the expiry month"),
    )
    for css, value, intent in payment_fields:
        _input(
            steps,
            css,
            value,
            page_path="/payment",
            pre_text="Payment",
            intent=intent,
        )
    _input(
        steps,
        "[data-qa='expiry-year']",
        "2030",
        page_path="/payment",
        pre_text="Payment",
        intent="Enter the expiry year and submit the fake demo-card payment",
        submit=True,
        post_type="text_visible",
        post_value="ORDER PLACED!",
    )
    _goto(steps, "/delete_account", "ACCOUNT DELETED!")
    _assert_text(
        steps,
        "ACCOUNT DELETED!",
        pre_url="/delete_account",
        intent="Verify cleanup of the purchase test account",
    )
    return {
        "case_version": "loop.case.v1",
        "name": "Automation Exercise purchase lifecycle",
        "goal": (
            "Register a unique account, search for a product, purchase quantity "
            "two with fake demo-card data, verify the order, and delete the account."
        ),
        "base_url": BASE_URL,
        "steps": steps,
    }


def build_tasks(batch: str) -> list[BrowserTask]:
    safe_batch = "".join(character for character in batch.lower() if character.isalnum())
    account_email = f"agenttrace.account.{safe_batch}@example.com"
    purchase_email = f"agenttrace.purchase.{safe_batch}@example.com"
    return [
        BrowserTask(
            key="account",
            title="Account lifecycle",
            email=account_email,
            case=_account_case(account_email, f"Account{safe_batch}"),
        ),
        BrowserTask(
            key="purchase",
            title="Purchase lifecycle",
            email=purchase_email,
            case=_purchase_case(purchase_email, f"Purchase{safe_batch}"),
        ),
    ]


def build_prompt(task: BrowserTask, session_id: str) -> str:
    arguments = json.dumps(
        {"session_id": session_id, "case": task.case},
        ensure_ascii=True,
        separators=(",", ":"),
    )
    return (
        f"Complete this real browser assignment: {task.case['goal']} "
        "Call browser_execute_case exactly once using the exact JSON arguments "
        "below. Do not modify, omit, or add fields. After the tool result, return "
        "one concise sentence stating whether the assignment passed.\n"
        f"{arguments}"
    )


def _create_project_and_conversation(task: BrowserTask, batch: str) -> tuple[str, str]:
    project = request(
        "/api/v1/projects",
        method="POST",
        body={
            "name": f"Automation Exercise {task.title} {batch}",
            "metadata": {
                "scenario": "automation-exercise",
                "batch": batch,
                "task": task.key,
            },
        },
        expected=201,
    )
    project_id = str(project["id"])
    conversation = request(
        f"/api/v1/projects/{project_id}/conversations",
        method="POST",
        body={
            "title": task.title,
            "metadata": {
                "scenario": "automation-exercise",
                "batch": batch,
                "task": task.key,
            },
        },
        expected=201,
    )
    return project_id, str(conversation["id"])


def _create_run(
    task: BrowserTask,
    project_id: str,
    conversation_id: str,
    *,
    agent_id: str,
    model: dict[str, str],
    batch: str,
) -> ScenarioRun:
    session_id = f"automation-exercise-{task.key}-{batch}-{uuid.uuid4().hex}"
    accepted = request(
        f"/api/v1/conversations/{conversation_id}/messages",
        method="POST",
        body={
            "content": build_prompt(task, session_id),
            "agent_id": agent_id,
            "model": model,
            "idempotency_key": f"automation-exercise-{batch}-{task.key}",
        },
        expected=202,
    )
    return ScenarioRun(
        task=task.key,
        project_id=project_id,
        conversation_id=conversation_id,
        run_id=str(accepted["run_id"]),
    )


def _submit_runs_serially(
    tasks: list[BrowserTask],
    locations: list[tuple[str, str]],
    *,
    agent_id: str,
    model: dict[str, str],
    batch: str,
    config: ScenarioConfig,
) -> tuple[list[ScenarioRun], dict[str, str]]:
    runs: list[ScenarioRun] = []
    statuses: dict[str, str] = {}
    for task, (project_id, conversation_id) in zip(tasks, locations):
        run = _create_run(
            task,
            project_id,
            conversation_id,
            agent_id=agent_id,
            model=model,
            batch=batch,
        )
        runs.append(run)
        statuses.update(
            wait_for_runs(
                [run.load_fixture()],
                timeout=config.run_timeout_seconds,
                workers=1,
            )
        )
    return runs, statuses


def _operation_matches_prompt(
    prompt: str, operation_input: dict[str, Any] | None
) -> bool:
    if not isinstance(operation_input, dict):
        return False
    try:
        expected_arguments = json.loads(prompt.rsplit("\n", 1)[1])
    except (IndexError, json.JSONDecodeError):
        return False
    return (
        operation_input.get("name") == "browser_execute_case"
        and json.dumps(
            operation_input.get("arguments"),
            sort_keys=True,
            separators=(",", ":"),
        )
        == json.dumps(expected_arguments, sort_keys=True, separators=(",", ":"))
    )


def _business_results(runs: list[ScenarioRun]) -> list[dict[str, Any]]:
    import psycopg

    by_run = {run.run_id: run.task for run in runs}
    with psycopg.connect(
        os.environ.get(
            "AGENTTRACE_POSTGRES_DSN",
            "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
        )
    ) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT run_id, status, result, artifacts, error
            FROM operation_results
            WHERE run_id = ANY(%s)
            ORDER BY created_at, id
            """,
            (list(by_run),),
        )
        rows = cursor.fetchall()
        cursor.execute(
            """
            SELECT run_id, input
            FROM spans
            WHERE run_id = ANY(%s) AND kind = 'operation'
            ORDER BY created_at, id
            """,
            (list(by_run),),
        )
        operation_inputs: dict[str, list[dict[str, Any]]] = {}
        for run_id, operation_input in cursor.fetchall():
            operation_inputs.setdefault(str(run_id), []).append(operation_input)
        cursor.execute(
            """
            SELECT run_id, content
            FROM messages
            WHERE run_id = ANY(%s) AND role = 'user'
            ORDER BY created_at, id
            """,
            (list(by_run),),
        )
        prompts: dict[str, list[str]] = {}
        for run_id, content in cursor.fetchall():
            prompts.setdefault(str(run_id), []).append(str(content))

    results: list[dict[str, Any]] = []
    for run_id, status, result, artifacts, error in rows:
        run_key = str(run_id)
        payload = result if isinstance(result, dict) else {}
        steps = payload.get("steps", [])
        failed_step = next(
            (
                {
                    "index": step.get("index"),
                    "action": step.get("action"),
                    "error": step.get("error"),
                    "url_after": step.get("url_after"),
                }
                for step in steps
                if step.get("status") != "passed"
            ),
            None,
        )
        evidence = artifacts if isinstance(artifacts, dict) else {}
        run_operation_inputs = operation_inputs.get(run_key, [])
        run_prompts = prompts.get(run_key, [])
        results.append(
            {
                "task": by_run[run_key],
                "run_id": run_key,
                "operation_status": str(status),
                "case_status": str(payload.get("status", "")),
                "operation_matches_prompt": (
                    len(run_operation_inputs) == 1
                    and len(run_prompts) == 1
                    and _operation_matches_prompt(
                        run_prompts[0], run_operation_inputs[0]
                    )
                ),
                "final_url": str(payload.get("final_url", "")),
                "passed_steps": sum(
                    step.get("status") == "passed" for step in steps
                ),
                "total_steps": len(steps),
                "failed_step": failed_step,
                "screenshot_paths": evidence.get("screenshot_paths", []),
                "operation_error": error,
            }
        )
    return sorted(results, key=lambda item: item["task"])


def validate_summary(summary: dict[str, Any]) -> None:
    failures: list[str] = []
    exact = {
        "requested_runs": 2,
        "submitted_runs": 2,
        "succeeded_runs": 2,
        "failed_runs": 0,
        "projects": 2,
        "conversations": 2,
        "operation_results": 2,
        "runs_with_operations": 2,
        "runs_with_screenshots": 2,
        "runs_with_spans": 2,
        "runs_with_usage": 2,
        "redis_streams": 2,
        "pending_outbox": 0,
        "dlq_events": 0,
        "unprocessed_commands": 0,
        "cross_project_runs": 0,
        "cross_conversation_messages": 0,
        "duplicate_message_ordinals": 0,
    }
    for name, expected in exact.items():
        actual = int(summary[name])
        if actual != expected:
            failures.append(f"{name}={actual}, expected={expected}")

    business = summary.get("business_results", [])
    observed_tasks = {str(item.get("task")) for item in business}
    if observed_tasks != {"account", "purchase"}:
        failures.append(f"business_tasks={sorted(observed_tasks)!r}")
    for item in business:
        if (
            item.get("operation_status") != "succeeded"
            or item.get("case_status") != "passed"
            or item.get("operation_matches_prompt") is not True
        ):
            failures.append(
                f"{item.get('task', 'unknown')} business result failed: "
                f"operation={item.get('operation_status')}, "
                f"case={item.get('case_status')}, "
                f"exact_prompt={item.get('operation_matches_prompt')}"
            )
    if failures:
        raise AssertionError(
            "Automation Exercise validation failed: " + "; ".join(failures)
        )


def _write_report(summary: dict[str, Any]) -> None:
    REPORT_PATH.parent.mkdir(parents=True, exist_ok=True)
    REPORT_PATH.write_text(
        json.dumps(summary, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def run_scenario(config: ScenarioConfig | None = None) -> dict[str, Any]:
    config = config or ScenarioConfig()
    health = request("/api/v1/health")
    if health.get("status") not in {"ok", "degraded"}:
        raise AssertionError(f"stack is not ready: {health}")
    agents = request("/api/v1/agents").get("agents", [])
    if len(agents) != 1:
        raise AssertionError(f"expected one configured agent: {agents}")
    agent = agents[0]
    provider = str(agent.get("model", {}).get("provider", ""))
    model_name = str(agent.get("model", {}).get("name", ""))
    reasoning_effort = str(agent.get("model", {}).get("reasoning_effort", ""))
    if provider != "openai-compatible":
        raise AssertionError(
            f"real-provider scenario requires openai-compatible, got {provider!r}"
        )

    batch = uuid.uuid4().hex[:12]
    tasks = build_tasks(batch)
    started = time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
        locations = list(
            executor.map(
                lambda task: _create_project_and_conversation(task, batch), tasks
            )
        )
    model = {"provider": provider, "name": model_name, "reasoning_effort": reasoning_effort}
    runs, statuses = _submit_runs_serially(
        tasks,
        locations,
        agent_id=str(agent["id"]),
        model=model,
        batch=batch,
        config=config,
    )
    load_runs = [run.load_fixture() for run in runs]
    wall_seconds = time.monotonic() - started
    deadline = time.monotonic() + config.delivery_timeout_seconds
    summary: dict[str, Any] = {}
    while time.monotonic() < deadline:
        summary = collect_summary(
            config.as_load_config(),
            load_runs,
            statuses,
            batch=batch,
            provider=provider,
            model_name=model_name,
            wall_seconds=wall_seconds,
        )
        if (
            summary["pending_outbox"] == 0
            and summary["unprocessed_commands"] == 0
            and summary["operation_results"] == config.runs
        ):
            break
        time.sleep(0.5)
    summary["scenario"] = "automation-exercise"
    summary["business_results"] = _business_results(runs)
    _write_report(summary)
    validate_summary(summary)
    return summary


def main() -> int:
    try:
        summary = run_scenario()
    except Exception as error:
        print(
            f"Automation Exercise scenario failed: {error}; report={REPORT_PATH}",
            file=sys.stderr,
        )
        return 1
    print(
        "Automation Exercise scenario passed: "
        + json.dumps(
            {
                "report": str(REPORT_PATH),
                "runs": summary["requested_runs"],
                "total_tokens": summary["total_tokens"],
                "wall_seconds": summary["wall_seconds"],
            },
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
