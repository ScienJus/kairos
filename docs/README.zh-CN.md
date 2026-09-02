---
title: Kairos | 人类与 AI Agent 团队协作
description: 让 Codex、Claude Code 和人类成员跨会话共享工作、明确责任、审核结果并可靠交接。
lang: zh-CN
permalink: /README.zh-CN.html
type: home
---

<p class="eyebrow">KAIROS / AGENT 团队协作</p>

# 让人和 AI Agent 围绕同一份工作协同推进

<p class="lede">Kairos 让 Codex、Claude Code 和人类成员看到同一份工作进度。Agent 可以找到当前该做的 Task，避免重复领取，留下可审核的结果，并把未完成的工作可靠地交给下一个会话。</p>

<div class="callout">
  <span class="callout-label">从这里开始</span>
  <strong>让两个 Agent 并行工作，而不是重复劳动</strong>
  <p>在仓库根目录运行 <code>make quickstart</code>。示例会开放两个并行 Task，并在两份结果都准备好后解锁最后的汇总 Task。</p>
</div>

<figure class="product-shot">
  <img src="{{ '/assets/kairos-workflow.jpg' | relative_url }}" alt="Kairos Workflow 展示两个并行 Task 汇合到发布计划">
  <figcaption>两个 Agent 并行完成工作，结果在最后一次交接中汇合。</figcaption>
</figure>

<section class="article-index" aria-labelledby="latest-heading">
  <div class="section-rule"><span>01</span><h2 id="latest-heading">从你遇到的问题开始</h2><span>场景指南</span></div>

  <div class="article-list">
    <a class="article-row" href="{{ '/guides/mcp-agent-coordination.zh-CN.html' | relative_url }}"><span class="article-number">01</span><span class="article-copy"><span class="article-tag">MCP / 多 AGENT</span><strong>使用 MCP 让多个 AI Agent 协同推进工作</strong><span>让不同会话共享可执行的 Task、明确负责人、接续上游结果并找到下一步。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/durable-task-claims.zh-CN.html' | relative_url }}"><span class="article-number">02</span><span class="article-copy"><span class="article-tag">执行责任 / 中断恢复</span><strong>用 Task Claim 保障 Agent 执行可靠性</strong><span>明确唯一执行者，在工作期间续期责任，并在会话中断后安全恢复。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/workflow-vs-blackboard.zh-CN.html' | relative_url }}"><span class="article-number">03</span><span class="article-copy"><span class="article-tag">工作规划</span><strong>选择固定流程，还是边做边调整计划</strong><span>已知步骤用 Workflow；需要随着调查逐步拆解时，用 Blackboard。</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/human-review-multi-agent.zh-CN.html' | relative_url }}"><span class="article-number">04</span><span class="article-copy"><span class="article-tag">人工审核</span><strong>在多 Agent 协作中加入人工 Review</strong><span>保留审核决定、反馈、证据和重试上下文，同时无需让 Agent 会话持续在线。</span></span><span class="article-arrow">↗</span></a>
  </div>
</section>

<section class="reference-section" aria-labelledby="reference-heading">
  <div class="section-rule"><span>02</span><h2 id="reference-heading">进一步了解</h2><span>参考文档</span></div>

  <div class="reference-list">
    <a href="https://github.com/ScienJus/kairos/tree/main/examples/quickstart"><strong>快速体验</strong><span>在本地运行一个完整示例。</span></a>
    <a href="{{ '/api-reference.zh-CN.html' | relative_url }}"><strong>API 参考</strong><span>配置服务，并通过 HTTP 或 MCP 接入。</span></a>
    <a href="{{ '/whitepapers/01-core-work-model.zh-CN.html' | relative_url }}"><strong>核心工作模型</strong><span>了解工作目标、Task 及其关系如何组合在一起。</span></a>
    <a href="{{ '/whitepapers/07-agent-interaction-model.zh-CN.html' | relative_url }}"><strong>Agent 交互模型</strong><span>了解 Agent 从发现工作到提交结果的完整过程。</span></a>
    <a href="https://github.com/ScienJus/kairos"><strong>GitHub 仓库</strong><span>代码、Issue、Release 和贡献指南。</span></a>
  </div>
</section>

<p class="status-note">目前，Kairos 已支持 Workflow 与 Blackboard 两种协作方式，可使用 SQLite 或 PostgreSQL 保存数据，通过 MCP 连接 Agent，并在操作控制台中查看进度。自动 Bridge 派发和部分控制台流程仍在规划中，详见 <a href="https://github.com/ScienJus/kairos#project-status">项目状态</a>。</p>
