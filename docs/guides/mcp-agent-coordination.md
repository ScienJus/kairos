---
title: How to Coordinate Multiple AI Agents with MCP | Kairos
description: Connect Codex, Claude Code, and other MCP clients to one durable work queue with Kairos, using exclusive claims to prevent duplicate agent execution.
---

# How to coordinate multiple AI agents with MCP

When two AI agents work from separate chat sessions, they do not automatically share ownership, progress, or deliverables. Kairos adds a durable coordination layer: agents discover eligible Tasks, acquire an exclusive Claim, send heartbeats while working, and submit a result that remains attached to the Task.

## The short version

```text
shared WorkItem → find work → claim Task → heartbeat → submit result
```

The same protocol works for Codex, Claude Code, and any MCP client that can call the Kairos execution tools. Kairos does not run the model or sandbox; it coordinates the work around them.

## Try the parallel example

From a checkout with Go 1.26.6 or later, Node.js, npm, and curl:

```bash
make quickstart
```

Open `http://127.0.0.1:8080`. The example contains two immediately available review Tasks and a join Task that becomes available only after both upstream results are submitted.

Start separate agent sessions with unique identities:

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

Ask each session to use the repository Skill:

```text
Use $kairos-agent to find and complete one available Task.
```

Each session sees the shared WorkItem, but an exclusive Claim means only one session can execute a given Task. When both parallel Tasks finish, Kairos exposes the join Task with their durable results in context.

## What Kairos coordinates

- **Discovery**: agents compare currently eligible work from one WorkItem.
- **Ownership**: a Claim fences competing executors and expires if heartbeats stop.
- **Delivery**: submissions, Reviews, failures, and Artifacts stay with the Task.
- **Continuation**: Workflow dependencies or Blackboard planning determine what can happen next.

See the [API Reference]({{ '/api-reference.html' | relative_url }}) for transport and MCP contracts, or the [Agent Interaction Model]({{ '/whitepapers/07-agent-interaction-model.html' | relative_url }}) for the full execution protocol.
