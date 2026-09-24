"""全部测试入口的唯一驱动：读 `test.config.json`，按顺序跑每一层。

为什么要有它：验证命令原来散在 README 与临时命令里，端口、目录、浏览器路径
各处各写一遍，改一处就会漂。现在只有 `test.config.json` 一处是权威。需要启动
配套服务的集成层自行申请端口、隔离数据并清理进程，runner 只负责调用与汇总。

用法（在仓库根运行）：
    python run_tests.py                # 跑全部层
    python run_tests.py --list         # 只列出层名
    python run_tests.py --layer 契约    # 只跑名字里含"契约"的层
    python run_tests.py --env          # 只打印配置导出的环境变量（给别的脚本用）
    python run_tests.py --verify       # 跑完各层后再跑真实浏览器页面验证

只用标准库：Go / Python / Node 都能原样读同一份 JSON，不引入任何依赖。
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CONFIG_PATH = ROOT / "test.config.json"


def force_utf8_stdio() -> None:
    """Windows 控制台默认 GBK，工具输出里的 ✓ 会让 print 直接抛异常。

    统一把标准流切成 UTF-8（编不过的字符替换而不是崩，避免一个 ✓ 拖死整个报告）。
    """
    for stream in (sys.stdout, sys.stderr):
        if stream is not None and hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def load_config() -> dict:
    if not CONFIG_PATH.is_file():
        raise SystemExit(f"找不到配置文件：{CONFIG_PATH}")
    with CONFIG_PATH.open(encoding="utf-8") as handle:
        return json.load(handle)


def build_env(config: dict) -> dict[str, str]:
    """把配置里的 env 段导出成进程环境变量，路径按仓库根解析。"""
    env = dict(os.environ)
    for key, value in (config.get("env") or {}).items():
        text = str(value)
        # 相对路径一律相对仓库根，避免"在哪个目录启动"改变行为。
        if key.startswith("LOOP_") and key.endswith(("_DIR", "_PATH")) and not Path(text).is_absolute():
            text = str((ROOT / text).resolve())
        env[key] = text
    return env


def resolve_command(command: list[str]) -> list[str]:
    """Windows 上 spawn npm/uv 这类脚本需要 .cmd 后缀；其它平台原样返回。

    `subprocess.run(["npm", ...])` 在 Windows 上不走 shell，而 npm 实际是
    `npm.cmd`，不解析后缀就会报“系统找不到指定的文件”。shutil.which 按下同
    PATHEXT 的顺序找出真实可执行文件，比手工拼后缀可靠。
    """
    if os.name != "nt":
        return command
    resolved = shutil.which(command[0])
    if resolved is None:
        return command  # 让 subprocess 报出原始错误，保持行为可预测
    return [resolved, *command[1:]]


def run_layer(config: dict, layer: dict) -> tuple[bool, str]:
    cwd = ROOT / layer["cwd"]
    command = resolve_command(list(layer["cmd"]))
    if not cwd.is_dir():
        return False, f"目录不存在：{cwd}"
    try:
        completed = subprocess.run(
            command,
            cwd=str(cwd),
            env=build_env(config),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except FileNotFoundError as exc:
        return False, f"命令不存在：{command[0]}（{exc}）"
    output = (completed.stdout or "") + (completed.stderr or "")
    return completed.returncode == 0, output


def tail(text: str, limit: int = 40) -> str:
    lines = [line for line in text.splitlines() if line.strip()]
    if len(lines) <= limit:
        return "\n".join(lines)
    return "\n".join(["…（前面省略）", *lines[-limit:]])


def main() -> int:
    force_utf8_stdio()
    parser = argparse.ArgumentParser(description="按 test.config.json 跑全部测试层")
    parser.add_argument("--list", action="store_true", help="只列出层名")
    parser.add_argument("--layer", default="", help="只跑名字里含该子串的层")
    parser.add_argument("--env", action="store_true", help="只打印导出的环境变量")
    parser.add_argument("--verify", action="store_true", help="各层之后再跑真实浏览器页面验证")
    args = parser.parse_args()

    config = load_config()

    if args.env:
        for key, value in sorted(build_env(config).items()):
            if key.startswith("LOOP_") or key.startswith("PLAYWRIGHT_"):
                print(f"{key}={value}")
        return 0

    layers = list(config.get("layers") or [])
    if args.layer:
        layers = [layer for layer in layers if args.layer in layer["name"]]
    if not layers:
        print("没有匹配的层", file=sys.stderr)
        return 2

    if args.list:
        for layer in layers:
            print(f"{layer['name']}  (cwd={layer['cwd']})")
        return 0

    results: list[tuple[str, bool]] = []
    for layer in layers:
        print(f"\n=== {layer['name']} ===", flush=True)
        print(f"    cd {layer['cwd']} && {' '.join(layer['cmd'])}", flush=True)
        ok, output = run_layer(config, layer)
        print(tail(output), flush=True)
        print(f"--> {'OK' if ok else 'FAILED'}", flush=True)
        results.append((layer["name"], ok))

    if args.verify:
        verify = config.get("verify") or {}
        print("\n=== 真实浏览器页面验证 ===", flush=True)
        script = ROOT / str(verify.get("script", "web/tests/verify_pages.py"))
        # 验证脚本要用 worker 的依赖（Playwright），所以用 uv run 起。
        command = resolve_command(["uv", "run", "python", str(script)])
        completed = subprocess.run(
            command,
            cwd=str(ROOT / "worker"),
            env=build_env(config),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        output = (completed.stdout or "") + (completed.stderr or "")
        print(tail(output, 60), flush=True)
        print(f"--> {'OK' if completed.returncode == 0 else 'FAILED'}", flush=True)
        results.append(("真实浏览器页面验证", completed.returncode == 0))

    print("\n=== 汇总 ===")
    for name, ok in results:
        print(f"  {'OK  ' if ok else 'FAIL'} {name}")
    failed = [name for name, ok in results if not ok]
    if failed:
        print(f"\nRESULT: FAILED（{len(failed)}/{len(results)} 层未通过）")
        return 1
    print(f"\nRESULT: OK（{len(results)} 层全通过）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
