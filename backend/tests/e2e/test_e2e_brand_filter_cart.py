"""E2E 测试: 品牌筛选+购物车验证 — 通过 REST API 自动化 AI 规划流程。

前置条件: 后端已启动 (uv run backend-dev)
运行方式: uv run pytest tests/e2e/test_e2e_brand_filter_cart.py -v -s
"""

from __future__ import annotations

import json
import sys
import time
from pathlib import Path

import httpx
import pytest

BASE_URL = "http://127.0.0.1:8000"
API = f"{BASE_URL}/api/v1"
TIMEOUT = 900  # AI 规划+页面探索可能需要较长时间
TEST_FILE = Path(__file__).resolve().parents[3] / "test_brand_filter_cart"

SUPPORTED_ACTIONS = {"goto", "click", "input", "wait_for", "assert_text", "assert_url_contains", "capture_text"}
pytestmark = pytest.mark.e2e_api


# ── helpers ──────────────────────────────────────────────────────────────


def _read_test_file() -> str:
    """读取 test_brand_filter_cart 文件内容。"""
    content = TEST_FILE.read_text(encoding="utf-8").strip()
    assert content, f"测试文件为空: {TEST_FILE}"
    return content


def _check_backend_alive(client: httpx.Client) -> None:
    """验证后端是否存活。"""
    resp = client.get(f"{API}/health", timeout=10)
    assert resp.status_code == 200, f"后端不可用: {resp.status_code}"
    print(f"  [OK] 后端存活: {resp.json()}")


def _create_session(client: httpx.Client) -> int:
    """创建 AI 规划会话，返回 session_id。"""
    resp = client.post(f"{API}/ai-planning/sessions", json={}, timeout=30)
    assert resp.status_code == 201, f"创建会话失败: {resp.status_code} {resp.text}"
    data = resp.json()
    sid = data["session"]["id"]
    print(f"  [OK] 会话已创建: session_id={sid}")
    return sid


def _create_project(client: httpx.Client, session_id: int) -> int:
    """在会话中创建项目，返回 project_id。"""
    unique_name = f"E2E-BrandCart-{int(time.time())}"
    resp = client.post(
        f"{API}/ai-planning/sessions/{session_id}/projects:create",
        json={"name": unique_name, "description": "自动化品牌筛选购物车测试"},
        timeout=30,
    )
    assert resp.status_code == 201, f"创建项目失败: {resp.status_code} {resp.text}"
    pid = resp.json()["id"]
    print(f"  [OK] 项目已创建: project_id={pid}")
    return pid


def _send_message(client: httpx.Client, session_id: int, content: str) -> dict:
    """发送消息到规划会话，返回 AI 响应。"""
    print(f"  --> 发送消息 ({len(content)} 字符)...")
    resp = client.post(
        f"{API}/ai-planning/sessions/{session_id}/messages",
        json={"content": content},
        timeout=TIMEOUT,
    )
    assert resp.status_code == 200, f"发送消息失败: {resp.status_code} {resp.text}"
    data = resp.json()
    print(f"  [OK] AI 响应: next_action={data['next_action']}, status={data['session_status']}")
    if data.get("plan") and data["plan"].get("scenarios"):
        keys = [s["scenario_key"] for s in data["plan"]["scenarios"]]
        print(f"    scenarios: {keys}")
    if data.get("missing_slots"):
        print(f"    missing_slots: {data['missing_slots']}")
    return data


def _generate_drafts(client: httpx.Client, session_id: int, scenario_keys: list[str]) -> dict:
    """生成 DSL 草案。"""
    print(f"  --> 生成草案: scenario_keys={scenario_keys}")
    resp = client.post(
        f"{API}/ai-planning/sessions/{session_id}/drafts:generate",
        json={"scenario_keys": scenario_keys},
        timeout=TIMEOUT,
    )
    assert resp.status_code == 200, f"生成草案失败: {resp.status_code} {resp.text}"
    data = resp.json()
    draft_count = len(data.get("drafts", []))
    print(f"  [OK] 草案已生成: {draft_count} 个")
    return data


# ── DSL 质量验证 ────────────────────────────────────────────────────────


def _validate_dsl_quality(drafts: list[dict]) -> list[str]:
    """验证所有草案的 DSL 质量，返回问题列表。"""
    issues: list[str] = []

    for draft in drafts:
        title = draft.get("title", "unknown")
        status = draft.get("status", "unknown")
        scenario_key = draft.get("scenario_key", "unknown")

        if status == "failed":
            issues.append(f"[{scenario_key}] 草案生成失败: {draft.get('error_message', '未知错误')}")
            continue

        dsl = draft.get("dsl_case")
        if not dsl:
            issues.append(f"[{scenario_key}] dsl_case 为空")
            continue

        # 基本结构检查
        if not dsl.get("name"):
            issues.append(f"[{scenario_key}] 缺少 name")
        if not dsl.get("base_url"):
            issues.append(f"[{scenario_key}] 缺少 base_url")

        steps = dsl.get("steps", [])
        if not steps:
            issues.append(f"[{scenario_key}] steps 为空")
            continue

        # 步骤级检查
        actions_found = set()
        for i, step in enumerate(steps):
            action = step.get("action")
            actions_found.add(action)

            # 支持的 action 检查
            if action not in SUPPORTED_ACTIONS:
                issues.append(f"[{scenario_key}] step[{i}] 不支持的 action: {action}")

            # 交互步骤必须有 target
            if action in ("click", "input", "wait_for", "assert_text", "capture_text"):
                if not step.get("target"):
                    issues.append(f"[{scenario_key}] step[{i}] {action} 缺少 target")

            # input 必须有 value
            if action == "input" and not step.get("value"):
                issues.append(f"[{scenario_key}] step[{i}] input 缺少 value")

            # assert_text 必须有 value
            if action == "assert_text" and not step.get("value"):
                issues.append(f"[{scenario_key}] step[{i}] assert_text 缺少 value")

            # goto 必须有 value
            if action == "goto" and not step.get("value"):
                issues.append(f"[{scenario_key}] step[{i}] goto 缺少 value")

        # 业务流覆盖检查
        if "goto" not in actions_found:
            issues.append(f"[{scenario_key}] 缺少 goto 步骤 (无入口 URL)")
        if "click" not in actions_found:
            issues.append(f"[{scenario_key}] 缺少 click 步骤")
        if "assert_text" not in actions_found and "assert_url_contains" not in actions_found:
            issues.append(f"[{scenario_key}] 缺少断言步骤 (assert_text/assert_url_contains)")

        # 步骤数合理性
        if len(steps) < 5:
            issues.append(f"[{scenario_key}] 步骤数过少 ({len(steps)}), 可能遗漏关键流程")

        # warnings 检查
        warnings = draft.get("warnings", [])
        severe_warnings = [w for w in warnings if "未找到" in w or "失败" in w or "错误" in w]
        if severe_warnings:
            for w in severe_warnings:
                issues.append(f"[{scenario_key}] 严重警告: {w}")

        print(f"    [{scenario_key}] {len(steps)} 步, actions={actions_found}, warnings={len(warnings)}")

    return issues


def _print_dsl_details(drafts: list[dict]) -> None:
    """打印 DSL 详细信息供人工审查。"""
    print("\n" + "=" * 70)
    print("DSL 详细内容")
    print("=" * 70)
    for draft in drafts:
        scenario_key = draft.get("scenario_key", "?")
        title = draft.get("title", "?")
        dsl = draft.get("dsl_case")
        if not dsl:
            print(f"\n[{scenario_key}] {title} — 无 DSL")
            continue

        print(f"\n[{scenario_key}] {title}")
        print(f"  base_url: {dsl.get('base_url')}")
        print(f"  steps ({len(dsl.get('steps', []))}):")
        for i, step in enumerate(dsl.get("steps", [])):
            action = step.get("action")
            target = step.get("target", "")
            value = step.get("value", "")
            loc = step.get("locator_confidence", "")
            desc = f"    [{i}] {action}"
            if target:
                desc += f"  target={target!r}"
            if value:
                desc += f"  value={value!r}"
            if loc:
                desc += f"  confidence={loc}"
            print(desc)

        warnings = draft.get("warnings", [])
        if warnings:
            print(f"  warnings ({len(warnings)}):")
            for w in warnings:
                print(f"    - {w}")


# ── 测试用例 ────────────────────────────────────────────────────────────


@pytest.mark.e2e_api
class TestE2EBrandFilterCart:
    """品牌筛选+购物车验证 E2E 测试。"""

    def test_full_flow(self):
        """完整流程: 创建会话 --> 发送需求 --> 生成草案 --> 验证 DSL 质量。"""
        client = httpx.Client(base_url=BASE_URL, follow_redirects=True)

        # Step 0: 后端存活检查
        print("\n[Step 0] 检查后端状态")
        _check_backend_alive(client)

        # Step 1: 创建会话
        print("\n[Step 1] 创建规划会话")
        session_id = _create_session(client)

        # Step 2: 创建项目
        print("\n[Step 2] 创建项目")
        project_id = _create_project(client, session_id)

        # Step 3: 发送测试需求
        print("\n[Step 3] 发送测试需求")
        test_content = _read_test_file()
        print(f"  测试文件: {TEST_FILE}")
        response = _send_message(client, session_id, test_content)

        # Step 4: 检查 AI 响应，获取 scenarios
        print("\n[Step 4] 检查 AI 响应")
        next_action = response.get("next_action")
        plan = response.get("plan")

        if next_action == "ask_followup":
            # AI 追问 — 用建议问题中的第一个作为回复
            suggested = response.get("suggested_questions", [])
            if suggested:
                followup = suggested[0]
                print(f"  AI 追问，自动回复: {followup}")
                response = _send_message(client, session_id, followup)
                next_action = response.get("next_action")
                plan = response.get("plan")

        # AI 可能直接生成草案 (next_action=drafts_generated)
        # 或需要用户选择场景 (next_action=select_scenarios)
        drafts = response.get("drafts", [])

        if next_action == "drafts_generated" and drafts:
            print(f"  [OK] AI 直接生成了 {len(drafts)} 个草案")
        elif next_action == "select_scenarios":
            assert plan is not None, "AI 未返回 plan"
            scenarios = plan.get("scenarios", [])
            assert scenarios, "plan 中无 scenarios"
            # 只选择第一个 (通常是主场景) 以避免生成所有草案超时
            scenario_keys = [scenarios[0]["scenario_key"]]
            print(f"  [OK] 获取到 {len(scenarios)} 个 scenarios, 选择第一个: {scenario_keys}")

            # Step 5: 生成 DSL 草案
            print("\n[Step 5] 生成 DSL 草案")
            draft_response = _generate_drafts(client, session_id, scenario_keys)
            drafts = draft_response.get("drafts", [])
            assert drafts, "未生成任何草案"
        else:
            pytest.fail(f"未预期的 next_action: {next_action}, drafts: {len(drafts)}")

        # Step 6: 验证 DSL 质量
        print("\n[Step 6] 验证 DSL 质量")
        _print_dsl_details(drafts)

        issues = _validate_dsl_quality(drafts)

        if issues:
            print(f"\n[WARN] 发现 {len(issues)} 个质量问题:")
            for issue in issues:
                print(f"  - {issue}")

        # 不直接 fail，而是输出诊断信息供分析
        # 严重问题才 fail
        critical = [i for i in issues if "生成失败" in i or "为空" in i or "steps 为空" in i]
        if critical:
            pytest.fail(f"DSL 严重问题:\n" + "\n".join(f"  {i}" for i in critical))

        print("\n[OK] E2E 测试完成")
