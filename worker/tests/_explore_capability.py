"""临时探索脚本（非测试）：把 capability.html 的真实观测写成 JSON 文件。

用途：为 test_capability_gaps.py 的断言取证。直接写文件而不是打 stdout——
私有区字形（U+F002 等）在 Windows GBK 控制台上会触发 UnicodeEncodeError。

跑法：cd worker && uv run python tests/_explore_capability.py
产物：worker/capability-dump.json
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

TESTS_DIR = Path(__file__).resolve().parent
WORKER_ROOT = TESTS_DIR.parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.observer import observe_page  # noqa: E402

OUTPUT = WORKER_ROOT / "capability-dump.json"


async def main() -> None:
    with LocalSite() as site:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.goto(site.url("capability.html"))
            observation = await observe_page(page)

            payload = {
                "url": observation.url,
                "title": observation.title,
                "count": len(observation.elements),
                "elements": [
                    {
                        "ref": element.ref,
                        "tag": element.tag,
                        "role": element.role,
                        "name": element.name,
                        "text": element.text,
                        "value": element.value,
                        "visible": element.visible,
                        "enabled": element.enabled,
                        "locators": [locator.model_dump() for locator in element.locators],
                    }
                    for element in observation.elements
                ],
            }
            OUTPUT.write_text(
                json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8"
            )
            print(f"wrote {OUTPUT} ({payload['count']} elements)")


run(main())
