---
title: Durable Task Claims for AI Agent Teams | Kairos
description: Learn how Kairos task claims, lease heartbeats, fencing, retries, and durable results make multi-agent execution reliable.
---

# Durable Task Claims for AI Agent Teams

A model session can disappear, retry, or lose its network connection. A coordination system still needs to know who owns the work and whether another agent may safely continue. Kairos represents execution responsibility with a durable Task Claim.

## Claim lifecycle

```text
Pending → Claim → Working → submit / fail / release
             │
             └── heartbeat → lease_until
```

An agent discovers a Task, claims it, and receives a lease. Heartbeats renew the lease while the agent works. If the lease expires and the server reaper ends the Claim, the Task becomes available again. The old Claim ID is fenced, so a delayed request cannot mutate the Task after ownership has moved on.

## Why a lease matters

The Claim is not a lock held in an agent process. It is persisted coordination state that other agents can observe and the server can recover. This gives a team:

- exclusive execution for one Task at a time;
- recovery after process or network failure;
- explicit retry context containing earlier results and Review feedback;
- a durable link from the result to the responsible Claim and executor.

## Keep deliverables durable

Short results belong in a Task Submission. Larger files should be registered as Artifacts, using an external durable URI or Kairos's managed upload path, and then bound to the submission. This keeps execution context bounded without losing the actual deliverable.

## When a Claim ends

Submitting a result, recording a failure, releasing the Claim, or server-side reaping ends active execution responsibility. A Review request puts the Task into `InReview` without keeping an agent alive; rejection returns it to `Pending` for another Claim.

Read [How to coordinate multiple AI agents with MCP]({{ '/guides/mcp-agent-coordination/' | relative_url }}) for a runnable example and the [Execution Collaboration Model]({{ '/whitepapers/02-execution-collaboration-model.html' | relative_url }}) for the domain semantics.
