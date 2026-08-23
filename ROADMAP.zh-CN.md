# Kairos Roadmap

[English](ROADMAP.md)

Kairos 正在积极开发中。本 Roadmap 用于说明方向，不代表交付日期。当前实现状态以 [README](README.zh-CN.md#项目状态)为准。

## 当前基础

- Workflow 与 Blackboard 协调语义；
- 持久化 Claim、提交、Review、失败与执行上下文；
- HTTP 与 MCP 执行接口；
- SQLite 与 PostgreSQL 持久化；
- Trusted 与 Authenticated 身份模式；
- 托管和外部 Artifact；
- 面向 WorkItem、人工关注、Task Map、流程图和 Definition 编辑的 operations console。

## 近期优先级

1. **可靠发布**：提供可复现二进制与容器镜像、Checksum、Migration 指南和升级验证。
2. **Bridge 集成**：将符合条件的 Task 派发到外部 Agent Harness，同时保持 Kairos 与模型及沙箱管理解耦。
3. **运营流程**：完成剩余控制台操作，改善失败恢复的可见性，并提供实用的备份恢复指南。
4. **集成示例**：记录真实的多 Agent 工作流，并提供可复用的 Workflow 与 Blackboard 模板。
5. **可观测性**：提供有用的结构化日志和运行指标，同时不让遥测成为协调依赖。

## 后续探索

- 完整 WorkItem 的 Kanban 视图；
- 更丰富的 Artifact Store Adapter；
- 面向共享团队的部署配置；
- Definition 和 API 演进的兼容性工具。

## 非目标

Kairos 不负责选择模型、运行 Agent 沙箱或替代 Agent Harness，也不会把 Kanban 变成第三种协调模式。新工作应保持持久协调与执行器运行时管理之间的边界。

`v1.0.0` 之前，API 和 Schema 可能随着这些边界的验证而变化。破坏性变更必须在 Release Notes 中明确说明；影响持久化数据时，还必须提供 Migration 指南。
