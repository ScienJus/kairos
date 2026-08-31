---
title: Kairos Documentation | Human and AI Agent Coordination
description: Learn how Kairos coordinates human and AI agent teams with durable tasks, exclusive claims, reviews, artifacts, workflows, and blackboard planning over MCP.
permalink: /
---

# Coordinate human and AI agent teams with Kairos

<p class="lede">Kairos is an open-source coordination server for Codex, Claude Code, and other MCP clients. It keeps tasks, ownership, reviews, artifacts, and next steps durable across agent sessions.</p>

<div class="callout">
  <strong>Try it in under a minute</strong><br>
  From the repository root, run <code>make quickstart</code>. The isolated example starts two parallel Tasks and a join Task in the operations console.
</div>

## Start with a scenario

<div class="guide-grid">
  <a class="guide-card" href="{{ '/guides/mcp-agent-coordination/' | relative_url }}">
    <h2>Coordinate multiple AI agents with MCP</h2>
    <p>Connect Codex or Claude Code sessions to one durable work queue and avoid duplicate execution.</p>
  </a>
  <a class="guide-card" href="{{ '/guides/durable-task-claims/' | relative_url }}">
    <h2>Use durable task claims for agent teams</h2>
    <p>Understand leases, heartbeats, fencing, retries, and why a Claim is different from a chat session.</p>
  </a>
  <a class="guide-card" href="{{ '/guides/workflow-vs-blackboard/' | relative_url }}">
    <h2>Workflow DAG vs blackboard planning</h2>
    <p>Choose a coordination mode based on whether the plan is known in advance or must evolve during execution.</p>
  </a>
  <a class="guide-card" href="{{ '/guides/human-review-multi-agent/' | relative_url }}">
    <h2>Add human review to multi-agent workflows</h2>
    <p>Keep review decisions, feedback, failures, and deliverables attached to the Task instead of an ephemeral run.</p>
  </a>
</div>

## Reference material

- [Quickstart](https://github.com/ScienJus/kairos/tree/main/examples/quickstart)
- [API Reference]({{ '/api-reference.html' | relative_url }})
- [Core Work Model]({{ '/whitepapers/01-core-work-model.html' | relative_url }})
- [Agent Interaction Model]({{ '/whitepapers/07-agent-interaction-model.html' | relative_url }})
- [GitHub repository](https://github.com/ScienJus/kairos)

Kairos currently includes Workflow and Blackboard runtime semantics, SQLite and PostgreSQL persistence, HTTP and MCP execution surfaces, an operations console, and a repository-level Codex Skill. Automatic Bridge dispatch and some console workflows remain planned; see [Project Status](https://github.com/ScienJus/kairos#project-status).
