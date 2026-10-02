# Kairos Core Work Model

Coordination breaks down when the work exists only in conversations: the objective becomes ambiguous, responsibility changes silently, and the next executor has to reconstruct what happened. Kairos gives those facts durable names. A WorkItem holds the objective, a Task describes one deliverable execution, and Relations connect Tasks into a graph.

Workflow and Blackboard organize that graph differently, but they do not create separate work models. The same records carry responsibility, results, review, and history in both modes.

## Definition, WorkItem, and Task

### Definition

A Definition is a reusable, versioned collaboration template. It supplies a name, description, default guidance, and mode-specific configuration. A WorkItem pins one version; later edits never change work already in progress.

### WorkItem

A WorkItem is one concrete collaborative objective. It contains:

- intent: objective, context, constraints, and acceptance criteria;
- mode: Workflow or Blackboard;
- structure: Tasks and Relations;
- work records: Claims, Submissions, Reviews, Failures, Artifacts, and domain events;
- lifecycle: execution, acceptance, or a terminal state.

A terminal WorkItem retains its history. It is not rewritten into a snapshot containing only the final result.

### Task

A Task is the boundary of one coherent, deliverable execution by one responsible executor. It records the objective, description, executor constraints, acceptance requirements, and lifecycle.

A Task may complete directly or be decomposed into child Tasks. After decomposition, the parent becomes an aggregate and produces no Submission of its own; the children collectively express its completion.

## The Task Graph

Tasks and Relations form a Task Graph inside a WorkItem:

```text
WorkItem
├── Task A ──→ Task C
└── Task B ──↗
```

The coordination mode determines the authority of a Relation:

| Mode | Source of graph | Meaning of Relation | How the graph changes |
| --- | --- | --- | --- |
| Workflow | Versioned Definition | Constrains legal progression | The Definition stays fixed; runtime instances and decisions are appended |
| Blackboard | Collaborators' current shared understanding | Suggests progression without hard blocking | Tasks and Relations may be appended during execution |

See [Coordination Semantics](03-coordination-semantics.md) for candidate and completion rules, and the [Workflow](04-workflow.md) and [Blackboard](05-blackboard.md) papers for each mode's detailed contract.

## Records That Survive the Session

A Task stores not only its current state but how that state was reached:

- **Claim**: one executor held responsibility for a period;
- **Submission**: one formal delivery and its result;
- **Review**: a decision and feedback on one Submission;
- **Failure**: why one Claim could not complete;
- **Artifact**: a durable reference to, or managed copy of, a deliverable;
- **Event**: an ordered, append-only domain change.

These records belong to the Task and WorkItem, not to an ephemeral agent session. Later executors can therefore continue from durable facts.

## Lifecycle Boundaries

- Task state and its active Claim must agree; terminal Tasks cannot be mutated.
- A terminal WorkItem ends active Claims and prevents later Task mutations.
- Execution failure, Human cancellation, and Review rejection remain distinct domain facts.
- Retries and recovery never overwrite historical Claims, Submissions, Reviews, Failures, or Artifacts.
- Empty collection fields are encoded as `[]`; only genuinely optional single values use `null`.

## What the Model Protects

A WorkItem is the objective a team advances together; a Task is one deliverable execution owned by one responsible executor. Workflow constrains which predefined path may advance, while Blackboard lets the team append its current plan as understanding changes. Because progress is expressed through durable lifecycle records, no chat or agent session has to remain online for the work to continue.
