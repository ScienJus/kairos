# Kairos Human Interaction Model

People should not have to translate a coordination database before they can decide what to do. The Kairos interface interprets the same WorkItems, Tasks, Claims, and results used by agents, then presents the current situation at a human scale.

This is a projection, not a parallel workflow. The interface first explains what is happening and only then offers actions the current person can actually take.

## Interaction Levels: From Objective to One Action

```text
Workspace → WorkItem → Task Detail → one action
```

- **Workspace** selects an objective and separates current work, history, and Human attention.
- **WorkItem** presents intent, constraints, execution structure, and overall lifecycle.
- **Task Detail** presents responsibility, results, history, and currently available actions for one Task.

Moving deeper should remove higher-level management information from attention instead of accumulating everything on one dashboard.

## Workspace and Human Attention

The Workspace separates:

- unfinished WorkItems;
- completed, cancelled, and failed history;
- Tasks, Reviews, and WorkItem acceptances requiring the current Human.

Human attention is a server-computed action projection over domain state, not an independent queue or lifecycle. It contains work the current Human may Claim or already owns, plus pending Reviews and acceptances.

## WorkItem Projection: Reading the Current Situation

A WorkItem page leads with intent and the current execution situation:

- Workflow uses a graph combining Definition nodes, runtime Task instances, and committed decisions;
- Blackboard uses a Task hierarchy for the evolving plan; Relations remain advisory rather than blocking dependencies;
- failure, cancellation, acceptance, and terminal status are WorkItem facts, not frontend guesses derived from Task counts.

The UI may collapse or summarize complex history, but it must not rewrite facts or present an uninstantiated Workflow node as executable work.

## Task Detail: Looking Inside One Task

Viewing and executing a Task are separate permissions:

- the **Detail projection** gives an allowed viewer responsibility, latest outcome, Artifacts, Review/Failure history, and capabilities;
- the **Execution Context** gives an eligible Human or Agent the context required to perform the Task.

The frontend renders actions from backend capabilities instead of reconstructing domain permissions from state, history, or actor IDs. The backend still revalidates every command because capabilities are only a current snapshot.

## Human Actions on the Shared Lifecycle

On the same durable model, a Human may:

- Claim, submit, release, or fail a Task;
- review a Submission and leave revision guidance;
- create, decompose, append, or skip Blackboard Tasks;
- submit or accept Blackboard completion;
- continue a failed Workflow or create a new WorkItem from the original intent;
- cancel a WorkItem that still permits mutation.

Actions need explicit success, conflict, and failure feedback. A network error must not appear as “not authorized” or clear unsubmitted input.

## History and Traceability Beyond the Screen

Claim, Submission, Review, and Failure histories are authoritative Task records. A UI may choose which summaries to show by default, but “not displayed” must never be described as “does not exist.”

WorkItem events support audit and later projections. Business state remains authoritative in the WorkItem, Task, Claim, and associated records.

## Interaction Invariants

- Permission to view does not imply permission to execute.
- UI capabilities are snapshots, not authorization credentials.
- People and agents mutate one lifecycle rather than parallel state machines.
- Current work and history are presented separately, but history remains accessible.
- The backend projects complex domain judgment; the frontend presents it and collects intent.
