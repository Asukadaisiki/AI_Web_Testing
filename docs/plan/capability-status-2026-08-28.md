# 休眠能力状态清单

日期：2026-08-28

| 能力 | Owner | 状态 | 仓外消费者 | 决策 | 复核/移除期限 |
|---|---|---|---|---|---|
| `rank_candidates_by_vision` | Backend / Locator | public-dormant | 待确认 | 保留公开 API；若无仓外消费者则在 P4 收窄为内部能力 | 2026-09-30 |
| `describe_page_layout` | Backend / Locator | public-dormant | 待确认 | 保留公开 API；与 VLM 默认关闭策略一并复核 | 2026-09-30 |
| `hash_password` | Backend / Auth | test-support | 无 | 保留密码哈希统一入口，供 seed/test 使用 | 无移除计划 |
| `cleanup_orphan_data.py` | Backend / Operations | active-cli | 运维 CLI | 已加默认 dry-run、保护条件和确认门禁，继续保留 | 每次 schema 变更后复核 |

## 状态定义

- `planned`：已有后续阶段接入计划。
- `dormant`：后端能力存在但当前 UI 未消费。
- `public-dormant`：仓内无生产调用，但公开符号可能有仓外消费者。
- `test-support`：仅测试或开发支撑使用，暂不视为孤儿。
- `deprecated`：已明确替代路径，等待兼容期结束。
- `active-cli`：独立命令入口，不以源码 import 判断可达性。
