# 平台 API 链路白盒测试设计

**日期**: 2026-04-17
**状态**: 已确认

## 背景

验证 AI Web Testing 平台自身 API 链路的正确性，通过模拟真实用户从会话登录到执行测试用例的完整流程，确保各 API 端点协作正常。

## 认证架构现状

平台存在两种认证模式：
- **会话认证** (`require_authenticated_user`)：用于 `/auth/*` 端点，检查 session cookie 中的 user_id
- **Demo 用户** (`require_demo_user`)：用于 `/cases/*` 和 `/executions/*` 端点，直接获取 user_id=1，不检查会话

因此测试需要覆盖两个层面：
1. 会话层：验证 `/auth/*` 端点的登录、身份验证、未授权拒绝
2. 业务层：验证通过 demo 用户模式调用 cases 和 executions 的完整链路

## API 调用链路

| 步骤 | HTTP 方法 | API 端点 | 认证模式 | 验证点 |
|------|-----------|----------|----------|--------|
| 1. 未登录访问 | GET | `/api/v1/auth/me` | 会话认证 | 无 cookie 返回 401 |
| 2. 平台登录 | POST | `/api/v1/auth/login` | 无 | 会话 cookie 设置，返回 user 信息 |
| 3. 获取当前用户 | GET | `/api/v1/auth/me` | 会话认证 | 返回一致的用户信息 |
| 4. 创建测试用例 | POST | `/api/v1/cases` | Demo 用户 | 用例创建成功，DSL 验证通过 |
| 5. 执行用例 | POST | `/api/v1/cases/{id}/execute` | Demo 用户 | Playwright 执行器走完步骤 |
| 6. 查看执行结果 | GET | `/api/v1/executions/{id}` | 无 | 所有步骤 passed，证据完整 |

## DSL 测试用例

```json
{
  "name": "The Internet - 正向登录验证",
  "description": "验证用户使用正确账号密码可以成功登录并进入安全页面",
  "base_url": null,
  "steps": [
    {"action": "goto", "value": "https://the-internet.herokuapp.com/login"},
    {"action": "input", "target": "username", "value": "tomsmith"},
    {"action": "input", "target": "password", "value": "SuperSecretPassword!"},
    {"action": "click", "target": "Login"},
    {"action": "assert_url_contains", "value": "/secure"},
    {"action": "assert_text", "target": "flash", "value": "You logged into a secure area!"}
  ]
}
```

## 技术方案

- **框架**: pytest + FastAPI TestClient（底层 httpx，支持 cookie jar）
- **复用 conftest**: 使用现有 `db_session`、`app_instance`、`client`、`anonymous_client` fixture
- **会话验证**: 使用 `anonymous_client` 测试未登录场景，使用 `client` 测试已登录场景
- **Playwright**: 依赖真实浏览器环境，执行步骤会调用外部网站
- **断言策略**: 每步 API 调用后立即断言状态码和关键字段

## 文件结构

```
tests/integration/
  test_platform_api_chain.py    # 新增：API 链路白盒测试
```

复用现有 `tests/conftest.py` 的 fixture。

## 测试用例清单

1. `test_unauthenticated_access_returns_401` - 未登录访问 `/auth/me` 返回 401
2. `test_login_sets_session_and_returns_user` - 登录后返回用户信息，后续请求保持会话
3. `test_session_persists_across_requests` - 登录后连续调用 `/auth/me` 两次，结果一致
4. `test_create_case_with_valid_dsl` - 创建包含有效 DSL 的测试用例，返回 201 和 Location header
5. `test_execute_login_case_and_verify_results` - 执行登录测试用例，验证结果状态为 passed
6. `test_full_api_chain_e2e` - 完整链路端到端：登录 → 创建用例 → 执行 → 查看结果

## 范围限制

- 仅测试正向登录成功链路
- 不覆盖异常登录场景（错误密码、空字段等）
- 不覆盖登出流程
- 不覆盖兼容性、性能、安全测试
- 测试目标为平台自身 API 链路的正确性
- Playwright 执行需要真实网络环境（访问 herokuapp.com）
