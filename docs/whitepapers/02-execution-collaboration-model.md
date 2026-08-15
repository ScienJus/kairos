# Kairos Execution Collaboration Model

> Task responsibility, shared context, and collaboration independent of execution method

## Abstract

People and agents can advance one complete WorkItem together through the Tasks they each own. Tasks can be claimed proactively, assigned to a person, or dispatched through an external Bridge. Every method must establish explicit and unique responsibility before execution and persist progress and results in the shared work model.

## 1. Task as the Executor Boundary

A WorkItem is a complete objective advanced by multiple executors. A Task is the execution boundary of one executor.

```text
WorkItem: Implement login
├── Task: Confirm login requirements → Person A
├── Task: Implement login            → Agent B
└── Task: Test login                 → Agent C
```

A Task should describe a complete, coherent, deliverable piece of work. Under normal execution, one executor owns it from start through delivery:

```text
Identify Task
    ↓
Establish responsibility
    ↓
Execute and record progress
    ↓
Submit result
    ↓
Complete Task
```

Therefore:

> One Task corresponds to one complete, coherent execution process.

## 2. Claim

A `Claim` establishes an explicit responsibility relationship between an executor and a Task:

```text
Agent  ─┐
        ├── responsible for ──→ Task
Person ─┘
```

A Claim has two essential properties:

- **Explicitness**: Kairos can determine which executor currently owns execution and delivery of the Task.
- **Uniqueness**: a Task can have only one active Claim at a time.

```text
Task A → Executor 1    valid

Task A → Executor 1
Task A → Executor 2    invalid
```

Unique execution responsibility prevents duplicated work and conflicting results while giving progress and deliverables a clear source.

> A Claim represents exclusive execution responsibility for a Task, independently of how the Task was distributed.

A Claim covers only the period during which the executor is working on the Task. When a submission enters human Review, the current Claim ends. The Task requires no liveness during Review and cannot be claimed by another executor. If Review rejects the result, the Task returns to the candidate set and either the original or another executor creates a new Claim.

When an executor cannot complete a Task, Kairos also ends the Claim and creates an immutable Task Failure:

```text
reopen         → Task returns to Pending
fail_work_item → Task and WorkItem become Failed
```

`reopen` can include a Retry Prompt. Every failure reason and Retry Prompt remains in Task context for the next executor. `fail_work_item` stops new Tasks from being created or claimed and ends other Active Claims as the WorkItem fails.

## 3. Ways to Establish Responsibility

Claim semantics are independent of how work is acquired. A Task executor can be selected in several ways:

| Participation method | How responsibility is established |
| --- | --- |
| Agent chooses proactively | The agent queries candidate Tasks, chooses one, and creates a Claim |
| External dispatch | A Bridge chooses an executor, creates the Claim, and starts the agent |
| Human execution | A person claims the Task or another person assigns it |

All methods share the same conceptual process:

```text
Produce candidate Tasks
        ↓
Choose executor
        ↓
Create Claim
        ↓
Execute Task
```

Proactive agent selection fits the current Kairos boundary, which does not control an Agent Harness. A future Bridge can start Codex, Claude Code, or another harness when a Task becomes executable.

Task organization and executor participation are independent dimensions:

| Task organization | Supported participation methods |
| --- | --- |
| Workflow | Proactive claim, external dispatch, or human assignment |
| Blackboard | Proactive claim, external dispatch, or human assignment |

## 4. Shared Work Context

Context inside an Agent Harness is usually temporary and local. Kairos assigns collaboration information to the work itself:

```text
WorkItem
├── Objective, background, constraints, acceptance criteria
├── Task A
│   ├── Execution progress
│   └── Deliverable result
├── Task B
│   ├── Execution progress
│   └── Deliverable result
└── Task C
    ├── Execution progress
    └── Deliverable result
```

> A Task belongs to a WorkItem, and its progress and results belong in the shared work model.

Every formal submission creates an immutable Task Submission. The Submission links to the Claim that produced it and stores that delivery result. Rework creates a new Submission instead of overwriting an earlier result. A Review links directly to the reviewed Submission, while a Failure links to the failed Claim, making all submissions, feedback, and failure reasons traceable.

Different executors collaborate through the results of their Tasks:

```text
Person A executes Task A: confirm requirements
                 ↓ shared result
Agent B executes Task B: implement
                 ↓ shared result
Agent C executes Task C: test
```

Each executor takes complete responsibility for its own Task and uses previous Task results to understand upstream work. The resulting shared context allows:

- downstream executors to understand completed work;
- parallel executors to understand the latest WorkItem state;
- people to observe each Task’s contribution to the objective;
- deliverables to remain after an Agent Harness exits.

## 5. Workflow and Blackboard

Workflow and Blackboard use the same execution collaboration model. Their difference is concentrated in how candidate Tasks are produced.

| Dimension | Workflow | Blackboard |
| --- | --- | --- |
| Candidate Tasks | Computed from a formal Task Graph | Formed from the shared Task Graph and current context |
| Prerequisite relations | Limit legal candidates | Provide progression guidance |
| Executors | Proactive claim, Bridge dispatch, or human assignment | Proactive claim, Bridge dispatch, or human assignment |
| Execution responsibility | Established through one unique Claim | Established through one unique Claim |
| Progress and results | Persisted on Task and WorkItem | Persisted on Task and WorkItem |

Workflow limits the legal choice space. Blackboard provides a dynamically evolving work structure and advisory relations. Both modes allow people and agents to choose proactively and both can integrate with external dispatch.

## 6. Kairos, Bridge, and Agent Harness

The Kairos collaboration semantics apply to people and agents independently of how an agent is run.

```text
┌──────────────────────────────┐
│         Kairos Core          │
│ WorkItem / Task / Claim      │
│ Shared Context / Result      │
└───────────────┬──────────────┘
                │
          Integration / Bridge
                │
┌───────────────▼──────────────┐
│        Agent Harness         │
│ Codex / Claude Code / Others │
└──────────────────────────────┘
```

Kairos Core represents work, provides candidate Tasks, establishes execution responsibility, and persists shared context. People participate through an interaction layer. An Agent Harness runs the agent, while a Bridge starts a specific harness and returns results.

This collaboration model can be summarized in five principles:

1. One collaborative execution is centered on an explicit Task.
2. One executor is responsible for a Task while it is being executed.
3. How a Claim is established does not change its responsibility semantics.
4. Task progress and results enter the shared work model.
5. Task organization and executor participation are independent.

> People and agents share a WorkItem, while each Task has one responsible executor.
> Kairos coordinates work independently of how that executor participates.
