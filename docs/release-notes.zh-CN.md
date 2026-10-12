# 发布记录

[English](release-notes.md)

## 未发布

### 执行上下文的破坏性变更

HTTP Task Context 与 MCP `get_task_context` 的扁平 `artifacts` 字段替换为 `staged_artifacts`、`input_artifacts` 和 `shared_artifacts`。升级服务时需同步更新客户端与 Agent 指令；仓库控制台和 Skill 已使用新字段。输入/共享分组携带准确的 Task/Submission 来源与审核状态，详见 [API 参考](api-reference.zh-CN.md#artifact)。

本变更以尚无正式发布的 API 版本为前提。不需要数据库迁移：持久化 Artifact、Submission 与 Workflow 输入记录均保持不变。WorkItem Context、产物历史查询和查看者 Task Detail 的 `artifacts` 集合保持不变。

控制台的 Workflow 产物历史按 node → 执行实例 → Submission 分组，Blackboard 按独立 Task → Submission 分组；Task Detail 直接展示提交历史。展示审核状态和反馈，默认折叠较早的执行与提交。
