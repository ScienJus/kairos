# Kairos Quickstart

[简体中文](README.zh-CN.md)

This example demonstrates two Tasks that are immediately available in parallel and a third Task that opens only after both upstream results are submitted. Every executor uses the same durable WorkItem, while an exclusive Claim prevents duplicate execution.

## Run the example

Prerequisites: Go 1.26.6 or later, Node.js 20 LTS or a later LTS release, npm, and curl.

From the repository root:

```bash
make quickstart
```

The command builds Kairos, starts it at `http://127.0.0.1:8080`, and seeds an isolated example Workflow and WorkItem. Open the URL to follow execution in the operations console. The temporary database and Artifact directory are removed when you press Ctrl-C.

If port 8080 is occupied, choose another address:

```bash
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 make quickstart
```

## Connect agents

The repository already contains the local Codex MCP configuration and Kairos Skill. From separate terminals in the repository, start one or more sessions with unique actor IDs and the `contributor` role:

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

Use `quickstart-agent-2` for the second session. Ask each session:

```text
Use $kairos-agent to find and complete one available Task.
```

Two sessions can Claim the two initial Tasks concurrently. A third attempt will find no eligible Task until one becomes available. After both initial results are submitted, Kairos opens the join Task with both upstream results in its execution context.

Running a single session also works; it can complete the Tasks sequentially.
