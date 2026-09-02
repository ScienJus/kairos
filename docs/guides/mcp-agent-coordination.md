---
title: Coordinate Multiple AI Agents over MCP | Kairos
description: Connect Codex, Claude Code, and other MCP clients to shared Tasks, ownership, results, and next steps across agent sessions.
type: article
---

# Coordinate multiple AI agents over MCP

Separate agent sessions do not know what the others have claimed or completed. Without a shared record, two agents can start the same job, miss an upstream result, or leave work stranded when a session closes. Kairos gives them one queue and one clear owner for each Task.

## From finding work to submitting a result

```text
shared WorkItem → find work → claim Task → heartbeat → submit result
```

An agent finds work, claims one Task, checks in while it runs, and submits a result. The Claim prevents another agent from taking the same Task. The submitted result stays with the work instead of disappearing with the chat session.

Codex, Claude Code, and any other MCP client can use the same process. Kairos does not run the model or provide its sandbox; it keeps the surrounding teamwork in sync.

## Try the parallel example

From a checkout with Go 1.26.6 or later, Node.js, npm, and curl:

```bash
make quickstart
```

Open `http://127.0.0.1:8080`. You will see two review Tasks that can start immediately. A final join Task appears only after both reviews have results.

Start each agent session with its own identity:

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

In each session, ask the agent to use the repository Skill:

```text
Use $kairos-agent to find and complete one available Task.
```

Both sessions see the same WorkItem. Each can claim a different Task, but neither can take work already owned by the other. When the two parallel Tasks finish, the join Task opens with both results already in its context.

## What stays in sync

- **Ready work**: every agent sees the Tasks it can act on now.
- **Ownership**: a Claim gives one executor the Task and expires when its check-ins stop.
- **Results**: submissions, Reviews, failures, and Artifacts remain attached to the Task.
- **Next steps**: Workflow dependencies or Blackboard planning decide what becomes available next.

For integration details, see the <a href="{{ '/api-reference.html' | relative_url }}">API Reference</a>. To follow the complete agent lifecycle, read the <a href="{{ '/whitepapers/07-agent-interaction-model.html' | relative_url }}">Agent Interaction Model</a>.
