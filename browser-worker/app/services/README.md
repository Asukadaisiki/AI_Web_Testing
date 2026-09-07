# Services

Browser Worker 侧少量纯函数服务。

当前只保留无数据库依赖的 failure signal 分类。Job 领取、执行持久化、报告聚合和
业务控制面都属于 Go AgentService / Go execution worker。
