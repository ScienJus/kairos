---
title: Use Task Claims for Reliable Agent Execution | Kairos
description: Use exclusive ownership, renewable leases, stale-claim fencing, recovery, and traceable results to keep agent execution reliable.
type: article
---

# Use Task Claims for reliable agent execution

Agent sessions are temporary: a process can crash, a connection can drop, or a retry can arrive late. The work still needs one unambiguous owner—and a safe point at which someone else may take over. In Kairos, a Task Claim records that responsibility outside the agent session.

## What happens after an agent claims a Task

```text
Pending → Claim → Working → submit / fail / release
             │
             └── heartbeat → lease_until
```

When an agent claims a Task, Kairos gives the Claim a lease. Regular heartbeats tell the server that the agent is still working. If those heartbeats stop, the server eventually ends the Claim and makes the Task available again. Requests from the old Claim are then rejected, so a delayed agent cannot overwrite the new owner's work.

## Why the Claim outlives the session

The Claim is not a lock inside the agent process. Kairos stores it where the server and other agents can observe it. That gives the team:

- exclusive execution for one Task at a time;
- recovery after process or network failure;
- retry context that includes earlier results and Review feedback;
- a traceable link from each result to its Claim and executor.

## Keep the result with the work

Put a short result in the Task Submission. Register larger files as Artifacts—either at a stable external URI or through Kairos's managed upload—and attach them to the same submission. Future executors can then find the deliverable without loading a large file into every prompt.

## How ownership ends

Ownership ends when the agent submits a result, reports a failure, releases the Claim, or stops checking in long enough for the server to reclaim it. If the result needs Review, the Task moves to `InReview` and the agent can exit. A rejection returns the Task to `Pending`, ready for a new Claim.

Try the flow in <a href="{{ '/guides/mcp-agent-coordination.html' | relative_url }}">Coordinate multiple AI agents over MCP</a>, or read the <a href="{{ '/whitepapers/02-execution-collaboration-model.html' | relative_url }}">Execution Collaboration Model</a> for the full rules.
