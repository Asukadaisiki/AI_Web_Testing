"""loop_worker —— v2 的精简 Playwright 执行器。

唯一权威契约是 `v2/CONTRACT.md`；本包只做一件事：把 case 跑成带证据的 ExecutionResult。

模块分工：

- `contracts.py`  pydantic 镜像（case / 观测 / 执行结果 + 条件阶段表）
- `observer.py`   观测采集 + 可访问名（`accessible_name`）的唯一实现
- `locators.py`   定位器就地验证与偏好排序（role → text → css）
- `conditions.py` 条件评估（pre 快照 / post 轮询；value_equals = target 元素的 value）
- `actions.py`    goto / click / input
- `runner.py`     case 执行循环
- `evidence.py`   截图 / console / network
- `sessions.py`   内存会话管理
- `main.py`       FastAPI app 工厂
"""

from __future__ import annotations

__version__ = "0.1.0"

__all__ = ["__version__"]
