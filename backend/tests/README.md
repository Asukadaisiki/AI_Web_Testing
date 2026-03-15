# Backend Tests

后端测试当前按下面的职责拆分：

- `unit/`
  - 默认测试入口。
  - 覆盖 schema、service、route、locator 规则、报告聚合等不依赖真实浏览器的回归。
- `integration/`
  - 浏览器级真实联调回归。
  - 当前包含 2 条本地夹具页人工干预回归：`needs_intervention -> correction -> rerun -> Tier 0 hit` 与“错误修正连续失败 3 次后自动停用”。
- `fixtures/`
  - 本地静态测试页面和轻量服务辅助。
  - 用于提供可控页面，不依赖 `example.com` 或真实业务站点。

## 运行方式

默认单元测试：

```powershell
cd backend
uv run pytest
```

浏览器级集成测试：

```powershell
cd backend
uv run pytest tests/integration -m browser_integration
```

若集成测试提示 Chromium 未安装，先执行：

```powershell
cd backend
uv run playwright install chromium
```
