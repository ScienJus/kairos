---
title: Kairos Documentation | Human and AI Agent Coordination
description: Learn how Kairos coordinates human and AI agent teams with durable tasks, exclusive claims, reviews, artifacts, workflows, and blackboard planning over MCP.
permalink: /
type: home
---

<p class="eyebrow">KAIROS / FIELD NOTES</p>

# The coordination layer for human and AI agent teams

<p class="lede">Kairos is an open-source coordination server for Codex, Claude Code, and other MCP clients. It keeps tasks, ownership, reviews, artifacts, and next steps durable across agent sessions.</p>

<div class="callout">
  <span class="callout-label">START HERE</span>
  <strong>See the coordination loop in action</strong>
  <p>From the repository root, run <code>make quickstart</code>. The isolated example starts two parallel Tasks and a join Task in the operations console.</p>
</div>

<figure class="product-shot">
  <img src="{{ '/assets/kairos-workflow.jpg' | relative_url }}" alt="Kairos Workflow showing two parallel tasks joining into a release plan">
  <figcaption>One WorkItem, two parallel reviews, one durable handoff.</figcaption>
</figure>

<section class="article-index" aria-labelledby="latest-heading">
  <div class="section-rule"><span>01</span><h2 id="latest-heading">Start with a scenario</h2><span>READ / APPLY</span></div>

  <div class="article-list">
    <a class="article-row" href="{{ '/guides/mcp-agent-coordination.html' | relative_url }}"><span class="article-number">01</span><span class="article-copy"><span class="article-tag">MCP / MULTI-AGENT</span><strong>Coordinate multiple AI agents with MCP</strong><span>Connect Codex or Claude Code sessions to one durable work queue and avoid duplicate execution.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/durable-task-claims.html' | relative_url }}"><span class="article-number">02</span><span class="article-copy"><span class="article-tag">RELIABILITY / EXECUTION</span><strong>Use durable Task Claims for agent teams</strong><span>Understand leases, heartbeats, fencing, retries, and why a Claim is different from a chat session.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/workflow-vs-blackboard.html' | relative_url }}"><span class="article-number">03</span><span class="article-copy"><span class="article-tag">PLANNING / WORKFLOW</span><strong>Workflow DAG vs Blackboard planning</strong><span>Choose between fixed dependencies and an evolving plan based on the work in front of you.</span></span><span class="article-arrow">↗</span></a>
    <a class="article-row" href="{{ '/guides/human-review-multi-agent.html' | relative_url }}"><span class="article-number">04</span><span class="article-copy"><span class="article-tag">REVIEW / HUMAN-IN-THE-LOOP</span><strong>Add human review to multi-agent workflows</strong><span>Keep review decisions, feedback, failures, and deliverables attached to the Task.</span></span><span class="article-arrow">↗</span></a>
  </div>
</section>

<section class="reference-section" aria-labelledby="reference-heading">
  <div class="section-rule"><span>02</span><h2 id="reference-heading">The operating model</h2><span>READ / DEEPEN</span></div>

  <div class="reference-list">
    <a href="https://github.com/ScienJus/kairos/tree/main/examples/quickstart"><strong>Quickstart</strong><span>Run the smallest complete example.</span></a>
    <a href="{{ '/api-reference.html' | relative_url }}"><strong>API Reference</strong><span>Transport, authentication, and execution contracts.</span></a>
    <a href="{{ '/whitepapers/01-core-work-model.html' | relative_url }}"><strong>Core Work Model</strong><span>The durable objects behind every collaboration.</span></a>
    <a href="{{ '/whitepapers/07-agent-interaction-model.html' | relative_url }}"><strong>Agent Interaction Model</strong><span>One execution loop for Workflow and Blackboard.</span></a>
    <a href="https://github.com/ScienJus/kairos"><strong>GitHub repository</strong><span>Code, issues, releases, and contribution guide.</span></a>
  </div>
</section>

Kairos currently includes Workflow and Blackboard runtime semantics, SQLite and PostgreSQL persistence, HTTP and MCP execution surfaces, an operations console, and a repository-level Codex Skill. Automatic Bridge dispatch and some console workflows remain planned; see [Project Status](https://github.com/ScienJus/kairos#project-status).
