---
title: Kairos 文档 | 人类与 AI Agent 协作协调
description: 使用 Kairos 通过 MCP 协调人类与 AI Agent 团队，管理持久 Task、独占 Claim、Review、Artifact、Workflow 和 Blackboard 规划。
lang: zh-CN
permalink: /README.zh-CN.html
type: home
---

<p class="eyebrow">KAIROS / 官方文章</p>

# 使用 Kairos 协调人类与 AI Agent 团队

<p class="lede">Kairos 是面向 Codex、Claude Code 和其他 MCP 客户端的开源协作协调服务器，让 Task、责任、Review、Artifact 和下一步工作跨 Agent 会话持久保留。</p>

<div class="callout">
  <span class="callout-label">从这里开始</span>
  <strong>一分钟看到协作协调循环</strong>
  <p>在仓库根目录运行 <code>make quickstart</code>。隔离示例会在 operations console 中启动两个并行 Task 和一个汇合 Task。</p>
</div>

<figure class="product-shot">
  <img src="{{ '/assets/kairos-workflow.jpg' | relative_url }}" alt="Kairos Workflow 展示两个并行 Task 汇合到发布计划">
  <figcaption>一个 WorkItem、两个并行 Review、一次持久交接。</figcaption>
</figure>

<section class="article-index" aria-labelledby="latest-heading">
  <div class="section-rule"><span>01</span><h2 id="latest-heading">从具体场景开始</h2><span>阅读 / 应用</span></div>

  <div class="article-list">
    <a class="article-row" href="{{ '/guides/mcp-agent-coordination.zh-CN.html' | relative_url }}"><span class="article-number">01</span><span class="article-copy"><span class="article-tag">MCP / 多 Agent</span><strong>使用 MCP 协调多个 AI Agent</strong><span>让 Codex 或 Claude Code 会话共享持久工作队列，避免重复执行。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/durable-task-claims.zh-CN.html' | relative_url }}"><span class="article-number">02</span><span class="article-copy"><span class="article-tag">可靠性 / 执行</span><strong>为 Agent 团队使用持久 Task Claim</strong><span>理解 lease、heartbeat、fencing、重试，以及 Claim 与会话的区别。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/workflow-vs-blackboard.zh-CN.html' | relative_url }}"><span class="article-number">03</span><span class="article-copy"><span class="article-tag">规划 / Workflow</span><strong>选择 Workflow DAG 或 Blackboard 规划</strong><span>根据工作特征，在固定依赖和持续演化的计划之间做选择。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/human-review-multi-agent.zh-CN.html' | relative_url }}"><span class="article-number">04</span><span class="article-copy"><span class="article-tag">Review / 人在回路</span><strong>为多 Agent 流程加入人工 Review</strong><span>让 Review 决策、反馈、失败和交付物都归属于 Task。</span></span><span class="article-arrow">↗</span></a>
  </div>
</section>

<section class="reference-section" aria-labelledby="reference-heading">
  <div class="section-rule"><span>02</span><h2 id="reference-heading">运行模型</h2><span>阅读 / 深入</span></div>

  <div class="reference-list">
    <a href="https://github.com/ScienJus/kairos/tree/main/examples/quickstart"><strong>快速体验</strong><span>运行最小的完整示例。</span></a>
    <a href="{{ '/api-reference.zh-CN.html' | relative_url }}"><strong>API 参考</strong><span>传输、身份验证与执行契约。</span></a>
    <a href="{{ '/whitepapers/01-core-work-model.zh-CN.html' | relative_url }}"><strong>核心工作模型</strong><span>每次协作背后的持久对象。</span></a>
    <a href="{{ '/whitepapers/07-agent-interaction-model.zh-CN.html' | relative_url }}"><strong>Agent 交互模型</strong><span>Workflow 与 Blackboard 共用的执行循环。</span></a>
    <a href="https://github.com/ScienJus/kairos"><strong>GitHub 仓库</strong><span>代码、Issue、Release 和贡献指南。</span></a>
  </div>
</section>
