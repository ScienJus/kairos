# 发布记录

[English](release-notes.md)

## 未发布

### 人工关注中的 Workflow 恢复

人工关注新增 `workflow_recovery` 条目，显示失败原因并进入 WorkItem 恢复入口。客户端需识别新类型；此前作为 `human_task` 返回的失败 Task 改为按 WorkItem 聚合。筛选和移出规则见 [API 参考](api-reference.zh-CN.md#人工关注)。本变更基于 `v0.1.0-alpha.1`，不需要数据库迁移，也不改变恢复命令。

### 首个 Alpha 候选版本

目标为面向早期试用的 `v0.1.0-alpha.1`，不承诺生产可用。范围包括 Workflow/Blackboard 协调、认证 HTTP/MCP、操作控制台、托管 Artifact、Core/Daemon 双二进制和隔离示例，使用方式见[安装指南](installation.zh-CN.md)。这不是发布公告，Snapshot 仍是未发布的候选包。

发布流水线验证 Linux/macOS 发布包的无模型本机执行；实际 Provider/CLI 组合的验证范围更窄。不保证 API/Schema 兼容、跨版本升级或生产备份恢复；Daemon 崩溃后的执行清理可能需要操作员介入。macOS 包没有 Developer ID 签名或公证。

### 执行上下文的破坏性变更

HTTP Task Context 与 MCP `get_task_context` 的扁平 `artifacts` 字段替换为 `staged_artifacts`、`input_artifacts` 和 `shared_artifacts`。升级服务时需同步更新客户端与 Agent 指令；仓库控制台和 Skill 已使用新字段。输入/共享分组携带准确的 Task/Submission 来源与审核状态，详见 [API 参考](api-reference.zh-CN.md#artifact)。

本变更以尚无正式发布的 API 版本为前提。不需要数据库迁移：持久化 Artifact、Submission 与 Workflow 输入记录均保持不变。WorkItem Context、产物历史查询和查看者 Task Detail 的 `artifacts` 集合保持不变。

WorkItem 的产物历史整体默认折叠，避免长结果挤占工作图和验收入口；含文件路径的来源标题在窄屏可收缩换行。控制台的 Workflow 产物历史按 node → 执行实例 → Submission 分组，Blackboard 按独立 Task → Submission 分组；Task Detail 直接展示提交历史。展示审核状态和反馈，默认折叠较早的执行与提交。
