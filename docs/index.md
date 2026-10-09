---
title: Kairos | Multi-agent coordination for Codex, Claude Code, and human teams
description: Coordinate multiple AI agents without duplicate work, preserve results across sessions, and continue safely after human review.
permalink: /
type: home
---

<p class="eyebrow">KAIROS / COORDINATION FOR AGENT TEAMS</p>

# Coordinate multiple AI agents without duplicate work

<p class="lede">Kairos gives Codex, Claude Code, and human teammates one durable view of the work. Agents take different jobs, results survive closed sessions, and human Review becomes context for whoever continues next.</p>

<div class="callout">
  <span class="callout-label">START HERE</span>
  <strong>Run two agents without giving them the same job</strong>
  <p>Run <code>make quickstart</code> from the repository root. The example opens two tasks in parallel, then unlocks a final task when both results are ready.</p>
</div>

<figure class="product-shot">
  <img src="{{ '/assets/kairos-workflow.jpg' | relative_url }}" alt="Kairos Workflow showing two parallel tasks joining into a release plan">
  <figcaption>Two agents work in parallel. Their results meet in one final handoff.</figcaption>
</figure>

<section class="article-index" aria-labelledby="latest-heading">
  <div class="section-rule"><span>01</span><h2 id="latest-heading">Start with the problem you have</h2><span>GUIDES</span></div>

  <div class="article-list">
    <a class="article-row" href="{{ '/guides/mcp-agent-coordination.html' | relative_url }}"><span class="article-number">01</span><span class="article-copy"><span class="article-tag">CODEX / MULTI-AGENT</span><strong>Coordinate two Codex sessions without duplicate work</strong><span>Give each session different work, preserve both results, and open a final handoff with the shared context.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/durable-task-claims.html' | relative_url }}"><span class="article-number">02</span><span class="article-copy"><span class="article-tag">OWNERSHIP / RECOVERY</span><strong>Use Task Claims for reliable agent execution</strong><span>Keep one clear executor, renew responsibility while work continues, and recover safely after interruption.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/workflow-vs-blackboard.html' | relative_url }}"><span class="article-number">03</span><span class="article-copy"><span class="article-tag">PLANNING</span><strong>Choose a fixed workflow or an evolving plan</strong><span>Use enforced dependencies for known processes and a Blackboard when the work must unfold as you learn.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/human-review-multi-agent.html' | relative_url }}"><span class="article-number">04</span><span class="article-copy"><span class="article-tag">HUMAN REVIEW</span><strong>Add human Review to multi-agent work</strong><span>Preserve decisions, feedback, evidence, and retry context without keeping an agent session open.</span></span><span class="article-arrow">↗</span></a>
  </div>
</section>

<section class="reference-section" aria-labelledby="reference-heading">
  <div class="section-rule"><span>02</span><h2 id="reference-heading">Go deeper</h2><span>REFERENCE</span></div>

  <div class="reference-list">
    <a href="https://github.com/ScienJus/kairos/blob/main/examples/quickstart/README.md"><strong>Quickstart</strong><span>Run a complete local example.</span></a>
    <a href="{{ '/api-reference.html' | relative_url }}"><strong>API Reference</strong><span>Configure the server and integrate through HTTP or MCP.</span></a>
    <a href="{{ '/whitepapers/01-core-work-model.html' | relative_url }}"><strong>Core Work Model</strong><span>Learn how objectives, tasks, and their relationships fit together.</span></a>
    <a href="{{ '/whitepapers/07-agent-interaction-model.html' | relative_url }}"><strong>Agent Interaction Model</strong><span>Follow an agent from discovering work to submitting a result.</span></a>
    <a href="{{ '/whitepapers/agent-daemon.html' | relative_url }}"><strong>Agent Daemon</strong><span>Understand automatic dispatch, Harness execution, and terminal convergence boundaries.</span></a>
    <a href="https://github.com/ScienJus/kairos"><strong>GitHub repository</strong><span>Code, issues, releases, and contribution guide.</span></a>
  </div>
</section>

<p class="status-note">Today you can coordinate work with Workflow or Blackboard, store it in SQLite or PostgreSQL, connect agents over MCP, and follow progress in the operations console. Agent Daemon includes continuous scheduling, an opt-in local Codex Adapter, and <a href="https://github.com/ScienJus/kairos/blob/main/examples/daemon/README.md">isolated execution examples</a>. Release packaging includes Core and Daemon; additional platform/provider validation remains separate. See <a href="https://github.com/ScienJus/kairos#current-status">Current Status</a>.</p>
