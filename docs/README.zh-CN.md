---
title: Kairos 文档 | 人类与 AI Agent 协作协调
description: 使用 Kairos 通过 MCP 协调人类与 AI Agent 团队，管理持久 Task、独占 Claim、Review、Artifact、Workflow 和 Blackboard 规划。
lang: zh-CN
permalink: /README.zh-CN.html
---

# 使用 Kairos 协调人类与 AI Agent 团队

<p class="lede">Kairos 是面向 Codex、Claude Code 和其他 MCP 客户端的开源协作协调服务器，让 Task、责任、Review、Artifact 和下一步工作跨 Agent 会话持久保留。</p>

<div class="callout">
  <strong>一分钟快速体验</strong><br>
  在仓库根目录运行 <code>make quickstart</code>。隔离示例会在 operations console 中启动两个并行 Task 和一个汇合 Task。
</div>

## 从具体场景开始

- [使用 MCP 协调多个 AI Agent](guides/mcp-agent-coordination.html)
- [为 Agent 团队使用持久 Task Claim](guides/durable-task-claims.html)
- [选择 Workflow DAG 或 Blackboard 规划](guides/workflow-vs-blackboard.html)
- [为多 Agent 流程加入人工 Review](guides/human-review-multi-agent.html)

## 参考资料

- [快速体验](https://github.com/ScienJus/kairos/tree/main/examples/quickstart)
- [API 参考](api-reference.html)
- [核心工作模型](whitepapers/01-core-work-model.zh-CN.html)
- [Agent 交互模型](whitepapers/07-agent-interaction-model.zh-CN.html)
- [GitHub 仓库](https://github.com/ScienJus/kairos)
