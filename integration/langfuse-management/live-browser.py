#!/usr/bin/env python3
"""Real Chromium UI acceptance; JSON-line commands on stdin, sanitized evidence on disk.

Run with worker/.venv/bin/python integration/langfuse-management/live-browser.py.
The private credential file is read only for the actual login form. No API client,
DOM writes, forced clicks, animation overrides, or service lifecycle commands.
Commands and assertions are preserved in report.json as an audit trail, not a
blindly replayable workflow: generated IDs and parent readiness change per run.
"""

import argparse
import hashlib
import json
import re
import struct
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

from playwright.sync_api import expect, sync_playwright


ROOT = Path(__file__).resolve().parent
CREDENTIALS = Path("/Users/bytedance/Library/Caches/agenttrace-native/langfuse/credentials.json")
ENV = Path("/Users/bytedance/Library/Caches/agenttrace-management-acceptance-20261028/acceptance.env")
SKILL_INPUT = """---
name: ui-acceptance-evidence
description: A disposable real-browser acceptance capability.
---

# UI Acceptance Evidence

Use the browser MCP tools to inspect the requested page and collect actual
screenshots. Navigate to the requested HTTP URL, observe it, and assert the
visible text requested by the user. Never claim completion without browser
evidence. Do not alter login identity or submit unrelated forms.
"""


def now():
    return datetime.now(timezone.utc).isoformat()


class Acceptance:
    def __init__(self, page, directory, credentials, env):
        self.page = page
        self.directory = directory
        self.identity = credentials["userEmail"]
        self.secrets = [
            str(value) for key, value in credentials.items()
            if any(word in key.lower() for word in ("password", "secret", "key", "salt", "email", "database"))
        ]
        self.secrets += [
            value for key, value in env.items()
            if any(word in key.lower() for word in ("token", "password", "secret", "key", "dsn"))
        ]
        self.report = {
            "started_at": now(), "browser": "isolated Playwright Chromium",
            "method": "Visible UI actions only; read-only DOM inspection; no API substitution",
            "langfuse_project_id": "agenttrace-native-project",
            "agenttrace_project_id": "prj_2f2b5e037cfb0ce640841aa7702d50f9",
            "commands": [], "checks": [], "checkpoints": [], "network": [],
            "page_errors": [], "owned": {}, "gaps": [],
        }
        page.on("response", self.response)
        page.on("pageerror", lambda error: self.report["page_errors"].append(self.clean(str(error))))

    def clean(self, text):
        for secret in sorted(self.secrets, key=len, reverse=True):
            if len(secret) >= 4:
                text = text.replace(secret, "[REDACTED]")
        text = re.sub(r"(?i)(token|password|secret|authorization)=([^&\s]+)", r"\1=[REDACTED]", text)
        return text

    def save(self):
        self.report["updated_at"] = now()
        target = self.directory / "report.json"
        target.write_text(json.dumps(self.report, indent=2) + "\n")

    def response(self, response):
        path = urlsplit(response.url).path
        if "/trpc/browserRuns." in path or "/api/agenttrace/control/" in path or "/api/agenttrace/artifacts/" in path:
            self.report["network"].append({
                "at": now(), "method": response.request.method,
                "path": path, "status": response.status,
            })

    def locator(self, command):
        scope = self.page
        if command.get("scope_role"):
            scope = scope.get_by_role(command["scope_role"], name=command.get("scope_name"), exact=True)
        if command.get("scope_text"):
            scope = scope.filter(has_text=command["scope_text"])
        if "label" in command:
            target = scope.get_by_label(command["label"], exact=True)
        elif "text" in command:
            target = scope.get_by_text(command["text"], exact=True)
        elif "placeholder" in command:
            target = scope.get_by_placeholder(command["placeholder"], exact=True)
        else:
            target = scope.get_by_role(command["role"], name=command.get("name"), exact=True)
        if "index" in command:
            target = target.nth(command["index"])
        return target

    def checkpoint(self, name):
        filename = f"{len(self.report['checkpoints']) + 1:02d}-{name}.png"
        identity = self.page.get_by_role("button", name=re.compile(re.escape(self.identity)))
        self.page.screenshot(path=str(self.directory / filename), full_page=True, mask=[identity])
        text = self.clean(self.page.locator("body").inner_text())
        self.report["checkpoints"].append({
            "at": now(), "name": name, "screenshot": filename,
            "url": urlsplit(self.page.url)._replace(query="", fragment="").geturl(),
            "visible_text": text,
        })
        self.save()
        return {"checkpoint": filename, "visible_text": text[:5000]}

    def state(self):
        return {
            "url": urlsplit(self.page.url)._replace(query="", fragment="").geturl(),
            "text": self.clean(self.page.locator("body").inner_text())[:16000],
            "controls": self.page.locator("select").evaluate_all(
                "(els) => els.map(e => ({label:e.closest('label')?.innerText,"
                "value:e.value, options:[...e.options].map(o => ({text:o.text,value:o.value,disabled:o.disabled}))}))"
            ),
            "buttons": self.page.get_by_role("button").evaluate_all(
                "(els) => els.filter(e => e.getClientRects().length).map(e => "
                "({text:e.innerText,aria:e.getAttribute('aria-label'),disabled:e.disabled}))"
            ),
        }

    def command(self, command):
        operation = command["op"]
        if operation == "state":
            return self.state()
        self.report["commands"].append(command)
        try:
            if operation == "click":
                target = self.locator(command)
                target.click(**({"position": command["position"]} if "position" in command else {}))
            elif operation == "fill":
                self.locator(command).fill(command["value"])
            elif operation == "select":
                target = self.locator(command)
                if "option_label" in command:
                    target.select_option(label=command["option_label"])
                else:
                    target.select_option(command["value"])
            elif operation == "check_box":
                self.locator(command).set_checked(command["checked"])
            elif operation == "upload":
                self.locator(command).set_input_files(command["path"])
            elif operation == "press":
                self.page.keyboard.press(command["key"])
            elif operation == "wait":
                target = self.locator(command)
                assertion = command.get("assert", "visible")
                timeout = command.get("timeout", 30000)
                if assertion == "visible":
                    expect(target).to_be_visible(timeout=timeout)
                elif assertion == "hidden":
                    expect(target).to_be_hidden(timeout=timeout)
                elif assertion == "enabled":
                    expect(target).to_be_enabled(timeout=timeout)
                elif assertion == "disabled":
                    expect(target).to_be_disabled(timeout=timeout)
                elif assertion == "contains":
                    expect(target).to_contain_text(command["value"], timeout=timeout)
                elif assertion == "value":
                    expect(target).to_have_value(command["value"], timeout=timeout)
                elif assertion == "count":
                    expect(target).to_have_count(command["value"], timeout=timeout)
                else:
                    raise ValueError("Unsupported assertion")
                if command.get("check"):
                    self.report["checks"].append({"name": command["check"], "passed": True, "at": now(), "assertion": command})
            elif operation == "screenshot":
                return self.checkpoint(command["name"])
            elif operation == "inspect_animation":
                result = self.page.get_by_role("dialog").evaluate(
                    "(e) => ({opacity:getComputedStyle(e).opacity,pointerEvents:getComputedStyle(e).pointerEvents,"
                    "animations:e.getAnimations().map(a => ({currentTime:a.currentTime,startTime:a.startTime,playState:a.playState}))})"
                )
                self.report["isolated_dialog_animation"] = result
                return result
            elif operation == "inspect":
                target = self.locator(command)
                result = target.evaluate(
                    "(e) => ({text:e.innerText,tag:e.tagName,value:e.value,"
                    "naturalWidth:e.naturalWidth,naturalHeight:e.naturalHeight,"
                    "complete:e.complete,src:e.tagName==='IMG' ? e.getAttribute('src')?.split('?')[0] : undefined})"
                )
                result.pop("src", None)
                self.report.setdefault("inspections", []).append({"at": now(), "command": command, "result": result})
                return result
            elif operation == "owned":
                self.report["owned"][command["key"]] = command["value"]
            elif operation == "gap":
                self.report["gaps"].append(command["reason"])
            elif operation == "finish":
                self.report["finished_at"] = now()
                self.report["conclusion"] = command["conclusion"]
            else:
                raise ValueError("Unsupported UI operation")
            self.save()
            return {"ok": True, "op": operation}
        except Exception as error:
            detail = self.clean(str(error))
            self.report["checks"].append({"name": command.get("check", operation), "passed": False, "at": now(), "error": detail})
            self.save()
            return {"ok": False, "error": detail}


def finalize(directory):
    """Exclude the account email with a disclosed center crop, using macOS sips."""
    path = directory / "report.json"
    report = json.loads(path.read_text())
    for checkpoint in report["checkpoints"]:
        image_path = directory / checkpoint["screenshot"]
        raw = image_path.read_bytes()
        if not raw.startswith(b"\x89PNG\r\n\x1a\n"):
            raise ValueError("Expected real PNG evidence")
        width, height = struct.unpack(">II", raw[16:24])
        original_height = max(height, 1100)
        if height > 1048:
            subprocess.run(
                ["sips", "--cropToHeightWidth", "1048", str(width), str(image_path)],
                check=True, capture_output=True,
            )
        offset = (original_height - 1048) // 2
        checkpoint["privacy_crop"] = {
            "reason": "Center crop excludes the fixed sidebar account email; no page pixels synthesized",
            "original_dimensions": [width, original_height],
            "retained_bounds": [0, offset, width, offset + 1048],
        }
        checkpoint["sha256"] = hashlib.sha256(image_path.read_bytes()).hexdigest()
        checkpoint["bytes"] = image_path.stat().st_size
        checkpoint["dimensions"] = list(struct.unpack(">II", image_path.read_bytes()[16:24]))
    report["corrections"] = report.get("owned", {}).get("assessment_corrections", [])
    report["totals"] = {
        "passed_checks": sum(check["passed"] is True for check in report["checks"]),
        "failed_checks_preserved": sum(check["passed"] is False for check in report["checks"]),
        "screenshots": len(report["checkpoints"]),
    }
    report["coverage_limits"] = [
        "Only Pi was run through the UI; other SDKs belong to separate parent API acceptance.",
        "No live viewer or RBAC-role-switch acceptance; contract tests are not browser evidence.",
        "Drain/Resume used an offline idle project runtime registration, not a live worker.",
        "The intentional cancellation workload did not create a case or fresh-context dry run.",
        "Existing MCP binding was inspected and selected, not rewritten.",
    ]
    report["artifact_finalized_at"] = now()
    path.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"finalized": str(path), "screenshots": len(report["checkpoints"])}))


def verify(directory):
    report = json.loads((directory / "report.json").read_text())
    assert report["owned"]["submissions_done"] is True
    assert report["owned"]["cleanup_final"]["run_status"] == "cancelled"
    assert report["owned"]["cleanup_final"]["takeover"] == "stopped"
    assert report["owned"]["cleanup_final"]["worker_drain"] == "restored"
    assert len(report["checkpoints"]) == report["totals"]["screenshots"]
    assert sum(not check["passed"] for check in report["checks"]) == report["totals"]["failed_checks_preserved"]
    assert report["corrections"], "Exploratory results require explicit corrections"
    for checkpoint in report["checkpoints"]:
        raw = (directory / checkpoint["screenshot"]).read_bytes()
        assert raw.startswith(b"\x89PNG\r\n\x1a\n")
        assert hashlib.sha256(raw).hexdigest() == checkpoint["sha256"]
        assert len(raw) == checkpoint["bytes"]
        assert list(struct.unpack(">II", raw[16:24])) == checkpoint["dimensions"]
    private = json.loads(CREDENTIALS.read_text())
    values = [
        str(value) for key, value in private.items()
        if any(word in key.lower() for word in ("password", "secret", "key", "salt", "email", "database"))
    ]
    for line in ENV.read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            key, value = line.split("=", 1)
            if any(word in key.lower() for word in ("token", "password", "secret", "key", "dsn")):
                values.append(value.strip().strip("'\""))
    for artifact in (directory / "report.json", directory / "VALIDATION.md"):
        text = artifact.read_text()
        assert not any(value in text for value in values if len(value) >= 4), "Private value found in text evidence"
    print(json.dumps({"verified": True, **report["totals"], "text_secret_scan": "passed"}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / ("live-browser-evidence-" + datetime.now().strftime("%Y%m%d-%H%M%S")))
    parser.add_argument("--finalize", type=Path, help="Finalize existing evidence without launching a browser")
    parser.add_argument("--verify", type=Path, help="Verify existing hashes, counts, corrections and text redaction")
    args = parser.parse_args()
    if args.verify:
        verify(args.verify)
        return
    if args.finalize:
        finalize(args.finalize)
        return
    args.output.mkdir(parents=True, exist_ok=True)
    credentials = json.loads(CREDENTIALS.read_text())
    env = {}
    for line in ENV.read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            key, value = line.split("=", 1)
            env[key] = value.strip().strip("'\"")
    with tempfile.TemporaryDirectory(prefix="langfuse-ui-skill-") as skill_directory, sync_playwright() as playwright:
        (Path(skill_directory) / "SKILL.md").write_text(SKILL_INPUT)
        browser = playwright.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1100})
        page = context.new_page()
        page.set_default_timeout(20000)
        acceptance = Acceptance(page, args.output, credentials, env)
        try:
            page.goto("http://127.0.0.1:3300/auth/sign-in", wait_until="domcontentloaded")
            page.get_by_label("Email", exact=True).fill(credentials["userEmail"])
            page.get_by_label("Password (forgot password?)", exact=True).fill(credentials["userPassword"])
            page.get_by_role("button", name="Sign in", exact=True).click()
            page.wait_for_url(lambda url: "/auth/" not in str(url), timeout=45000)
            acceptance.report["checks"].append({"name": "actual-login-ui", "passed": True, "at": now()})
            page.goto("http://127.0.0.1:3300/project/agenttrace-native-project/browser-runs", wait_until="domcontentloaded")
            page.get_by_role("button", name="New conversation", exact=True).wait_for(timeout=45000)
            print(acceptance.clean(json.dumps({
                "ready": True, "output": str(args.output),
                "skill_input_directory": skill_directory, "state": acceptance.state(),
            })), flush=True)
            commands = (json.loads(line) for line in sys.stdin if line.strip())
            for command in commands:
                result = acceptance.command(command)
                print(acceptance.clean(json.dumps(result)), flush=True)
                if command["op"] == "finish":
                    break
        except Exception as error:
            acceptance.report["fatal_error"] = acceptance.clean(str(error))
            print(json.dumps({"fatal": acceptance.report["fatal_error"]}), flush=True)
        finally:
            acceptance.save()
            context.close()
            browser.close()


if __name__ == "__main__":
    main()
