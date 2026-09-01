---
title: 使用 MCP 协调多个 AI Agent | Kairos
description: 使用 Kairos 将 Codex、Claude Code 和其他 MCP 客户端接入一个持久工作队列，并通过独占 Claim 避免重复执行。
lang: zh-CN
type: article
---

# 使用 MCP 协调多个 AI Agent

两个 AI Agent 在不同聊天会话中工作时，不会自动共享所有权、进度或交付物。Kairos 增加了一层持久协调：Agent 发现可执行 Task，获取独占 Claim，工作期间发送 heartbeat，并提交仍然挂在 Task 上的结果。

## 简要流程

```text
shared WorkItem → find work → claim Task → heartbeat → submit result
```

只要 MCP 客户端能够调用 Kairos 的执行工具，这套协议就适用于 Codex、Claude Code 以及其他 MCP 客户端。Kairos 不运行模型或沙箱，而是负责围绕它们协调工作。

## 试用并行示例

在安装 Go 1.26.6 或更高版本、Node.js、npm 和 curl 的代码检出目录中运行：

```bash
make quickstart
```

打开 `http://127.0.0.1:8080`。示例包含两个立即可执行的 Review Task，以及一个只有在两个上游结果都提交后才会变为可执行的汇合 Task。

为每个 Agent 会话使用不同的身份：

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

让每个会话使用仓库 Skill：

```text
Use $kairos-agent to find and complete one available Task.
```

每个会话都能看到共享 WorkItem，但独占 Claim 保证一个 Task 同时只有一个会话可以执行。当两个并行 Task 完成后，Kairos 会在汇合 Task 的上下文中提供它们的持久结果。

## Kairos 协调什么

- **发现**：Agent 从同一个 WorkItem 中比较当前可执行的工作。
- **所有权**：Claim 会 fencing 竞争执行者，并在 heartbeat 停止后过期。
- **交付**：Submission、Review、失败记录和 Artifact 都会保留在 Task 上。
- **继续推进**：Workflow 依赖或 Blackboard 规划决定下一步可以做什么。

参阅 <a href="{{ '/api-reference.zh-CN.html' | relative_url }}">API 参考</a>了解传输和 MCP 契约，参阅 <a href="{{ '/whitepapers/07-agent-interaction-model.zh-CN.html' | relative_url }}">Agent 交互模型</a>了解完整执行协议。
