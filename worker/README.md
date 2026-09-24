# loop_worker —— v2 精简 Playwright 执行器

唯一权威契约是 `../CONTRACT.md`。本目录只做一件事：把 case 跑成带证据的 `ExecutionResult`。

## 运行

```bash
# 依赖（uv 管理，Python 3.12+）
cd v2/worker && uv sync

# 起服务（Go 侧默认连 http://127.0.0.1:8100）
$env:PLAYWRIGHT_BROWSERS_PATH = "D:\PlaywrightBrowsers"
uv run uvicorn loop_worker.main:app --port 8100
```

## 测试（离线、不花钱、不访问外网）

```bash
cd v2/worker
$env:PLAYWRIGHT_BROWSERS_PATH = "D:\PlaywrightBrowsers"
uv run python -m compileall -q loop_worker tests
uv run python -m unittest discover -s tests -t .
```

测试用 `fixtures/site/` 下的本地静态站点（标准库 `http.server` 绑 127.0.0.1 随机端口），
不依赖 `file://`，也不访问任何真实站点。

## 环境变量

| 变量 | 说明 |
|---|---|
| `PLAYWRIGHT_BROWSERS_PATH` | 浏览器安装目录；未设置时回退到本机 `D:\PlaywrightBrowsers` |
| `LOOP_ARTIFACTS_DIR` | 证据产物目录；默认 `v2/data/artifacts` |

## 模块

| 文件 | 职责 |
|---|---|
| `loop_worker/contracts.py` | pydantic 镜像（case / 观测 / 执行结果 + 条件阶段表） |
| `loop_worker/observer.py` | 观测采集 + `accessible_name`（可访问名唯一实现）+ action candidate 编译 |
| `loop_worker/locators.py` | 定位器就地验证与偏好排序（role → text → css） |
| `loop_worker/blockers.py` | blocker 识别、hit-test 可达性检查与安全恢复 |
| `loop_worker/conditions.py` | 条件评估（pre 快照 / post 轮询） |
| `loop_worker/actions.py` | goto / click / input / select / check / uncheck / scroll / hover / dismiss / upload |
| `loop_worker/runner.py` | case 执行循环 |
| `loop_worker/evidence.py` | 截图 / console / network |
| `loop_worker/sessions.py` | 内存会话管理 |
| `loop_worker/main.py` | FastAPI app 工厂 |
