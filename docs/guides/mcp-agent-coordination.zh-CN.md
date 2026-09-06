---
title: 使用 MCP 让多个 AI Agent 协同推进工作 | Kairos
description: 让 Codex、Claude Code 和其他 MCP 客户端跨会话共享 Task、执行责任、工作结果和下一步。
lang: zh-CN
type: article
---

# 使用 MCP 让多个 AI Agent 协同推进工作

彼此独立的 Agent 会话并不知道其他会话领取了什么、完成了什么。缺少共享记录时，两个 Agent 可能同时开始同一个 Task，也可能看不到上游结果，甚至在会话关闭后把工作丢在半路。Kairos 用一个共享队列和明确的 Task 负责人解决这些问题。

## 从发现工作到提交结果

```text
shared WorkItem → find work → claim Task → heartbeat → submit result
```

Agent 先找到可执行的工作，领取一个 Task，在执行期间定期续租，最后提交结果。Claim 会阻止其他 Agent 重复领取同一个 Task；结果会保留在工作记录中，不会随着聊天会话结束而消失。

Codex、Claude Code 和其他能够调用 Kairos 工具的 MCP 客户端都使用同一套过程。Kairos 不运行模型，也不提供沙箱；它只负责让这些执行环境围绕同一份工作保持同步。

## 试用并行示例

在安装 Go 1.26.6 或更高版本、Node.js、npm 和 curl 的代码检出目录中运行：

```bash
make quickstart
```

打开 `http://127.0.0.1:8080`，你会看到两个可以立即开始的 Review Task。只有两份 Review 都有结果后，最后的汇总 Task 才会出现。

为每个 Agent 会话设置不同的身份：

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

在每个会话中，让 Agent 使用仓库 Skill：

```text
Use $kairos-agent to find and complete one available Task.
```

两个会话会看到同一个 WorkItem。它们可以分别领取不同的 Task，但不能抢走对方已经领取的工作。两个并行 Task 完成后，汇总 Task 会自动开放，并直接带上两份上游结果。

## 哪些信息会保持同步

- **当前可做的工作**：每个 Agent 都能看到自己此刻可以执行的 Task。
- **负责人**：Claim 把 Task 交给一个执行者；续租停止后，Task 可以安全地重新开放。
- **执行结果**：Submission、Review、失败记录和 Artifact 都会留在 Task 上。
- **下一步**：Workflow 依赖或 Blackboard 规划决定接下来开放什么工作。

接入细节请参阅 <a href="{{ '/api-reference.zh-CN.html' | relative_url }}">API 参考</a>；完整的 Agent 执行过程请参阅 <a href="{{ '/whitepapers/07-agent-interaction-model.zh-CN.html' | relative_url }}">Agent 交互模型</a>。
