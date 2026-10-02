# Kairos Workflow Mode

Workflow is for work whose important steps and dependencies can be stated before execution begins. Its graph is more than a diagram: it decides which runtime Tasks may exist and when they may advance.

To keep that promise stable, every WorkItem pins an immutable Definition version. Later edits can improve future work without quietly changing the rules underneath work already in progress.

## Definition and Runtime Reality

A Workflow Definition contains Task Definitions, Relations, and initial nodes. A WorkItem pins its Definition ID and Version at creation; later versions never change running work.

The Definition is a rule, while runtime Tasks are facts. A UI may project them together, but an unreached Definition node is not a claimable Task. A loop node may produce several runtime Task instances, each retaining its own Claims, Submissions, and history.

A Relation may carry a concise `label` and `agent_guidance` that explains an existing progression choice. Guidance does not create additional conditional branches.

## Workflow Progression: How the Graph Advances

For an ordinary acyclic relation, a node can instantiate only after all of its concrete predecessor instances have ended. Parallel branches progress independently; a join waits for their corresponding instances.

Loop choices compile from graph structure:

- each edge staying in the current loop forms one Continue Group;
- edges leaving the loop form one Exit Group;
- Continue and Exit Groups are mutually exclusive;
- a selected Continue target is kept directly; required Exit targets are kept automatically, while optional Exit targets remain executor decisions.

Every loop needs an exit. The runtime Task Graph links concrete instances and therefore remains an acyclic execution history.

For example, an investigation node may either loop into another investigation pass or exit toward publication. Choosing another pass creates a new runtime Task instance; it does not reopen or overwrite the previous pass. The Definition may contain a cycle while the recorded execution history remains acyclic.

## What a Task Definition Controls

| Configuration | Meaning |
| --- | --- |
| `executor` | Allow Human, Agent, or either |
| `allowed_roles` | Allowed Agent roles; never restrict Humans |
| required / optional | Required paths execute; optional targets may be skipped at configured decision points |
| Review policy | None, executor-decided, or mandatory Human Review |
| Artifact requirements | Deliverables that a Submission must attach |

Workflow bounds Definition size and runtime instances per Definition node and WorkItem. See [OpenAPI](../openapi.yaml) for exact limits and request constraints.

## When a Task Becomes a Candidate

A runtime Task is claimable only when:

```text
state = pending
+ no active Claim
+ WorkItem permits execution
+ executor kind and Agent role match
```

Workflow candidates are not filtered by Blackboard tags. When several are legal, a Human, proactive Agent, or Agent Daemon may choose any and create a Claim.

## Optional Work and Review: Decisions Reserved for Execution

An executor submits its decisions for currently reachable optional targets with the current Task. Kairos applies them through the Definition's relation partitions; the executor does not construct a new graph.

When multiple predecessor paths meet at one optional target, every relevant path must choose skip for it to be skipped. Any keep decision creates the Task.

This prevents one branch from discarding work that another branch still needs. If two analyses converge on an optional verification Task, a skip from only one analysis is not enough to erase the other analysis's keep decision.

Review applies to the current Submission. Submission ends the Claim and enters `in_review`; approval then applies the result and progression decisions, while rejection returns the Task to `pending` under a new Claim.

## Completion and Recovery

A Workflow WorkItem completes when all instantiated Tasks on the selected path have ended and no further Task needs to be created. Unapproved results and unresolved optional decisions do not advance it.

After failure, a Human has two recovery paths:

- **Continue execution** keeps the current WorkItem, successful branches, and history, then creates replacement Task instances for current failed or interrupted work;
- **Start over** creates a new WorkItem from the same Definition version and original intent, carrying only a bounded failure summary and Human instructions.

Neither revives old Claims or offers arbitrary-stage replay. See the [API Reference](../api-reference.md) for retry instructions, instance limits, responses, and conflicts.

## Workflow Invariants

- A WorkItem remains pinned to one Definition version.
- Only graph-authorized Tasks are instantiated and claimed.
- Executors decide only within spaces reserved by the Definition.
- Historical Task instances, Submissions, Reviews, and Failures are never overwritten by progression or recovery.
- See [runtime measurements](../workflow-runtime-performance.md) for storage and query boundaries of graph expansion.
