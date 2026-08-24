# Kairos Execution Collaboration Model

> Task responsibility, shared context, and collaboration independent of execution method

## Abstract

People and agents can advance one complete WorkItem together through the Tasks they each own. A Task's executor kind restricts who is eligible, and `AllowedRoles` further restricts Agent identities only; a matching actor establishes concrete responsibility by Claiming it. A future external Bridge can automate the same Agent role-aware selection and Claim process. Task lifecycle changes and durable results then express progress of the shared WorkItem.

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
Execute Task
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
- **Recoverability for agents**: an Agent Claim is a renewable lease, so execution responsibility can be recovered after the agent disappears.

```text
Task A → Executor 1    valid

Task A → Executor 1
Task A → Executor 2    invalid
```

Unique execution responsibility prevents duplicated work and conflicting results while giving lifecycle changes and deliverables a clear source.

> A Claim represents exclusive execution responsibility for a Task, independently of how the Task was distributed.

Only Agent Claims use leases. An agent may request a lease duration when claiming and on every heartbeat; the server applies policy bounds and returns the granted `lease_seconds` and `lease_until`. The deadline makes an active Claim eligible for the background reaper; time alone does not change ownership. Before the reaper commits, the current executor may continue operating or renew the Claim, and no other executor may Claim the Working Task. The reaper ends an eligible Claim with `expired` and returns the Task to Pending. Only then may a new executor Claim it; the old Claim ID acts as a fencing token and cannot be revived or used for submission.

Human Claims do not use leases or heartbeat. They remain active until submission, failure, explicit release, or administrative revocation. This keeps infrastructure liveness out of the human interaction model.

A Claim covers only the period during which the executor is working on the Task. When a submission enters human Review, the current Claim ends. The Task requires no liveness during Review and cannot be claimed by another executor. If Review rejects the result, the Task returns to the candidate set and either the original or another executor creates a new Claim.

When an executor cannot complete a Task, Kairos also ends the Claim and creates an immutable Task Failure:

```text
reopen         → Task returns to Pending
fail_work_item → Task and WorkItem become Failed
```

`reopen` can include a Retry Prompt. Every failure reason and Retry Prompt remains in Task context for the next executor. `fail_work_item` stops new Tasks from being created or claimed and ends other Active Claims as the WorkItem fails.

## 3. Ways to Establish Responsibility

Claim semantics are independent of how work is acquired. `Executor` restricts the eligible actor kind, and `AllowedRoles` further restricts eligible Agent identities. Human identities are never filtered by `AllowedRoles`. These constraints select a class of eligible executors; the Claim records the one concrete actor that takes responsibility.

| Participation method | How responsibility is established |
| --- | --- |
| Agent chooses proactively | A matching Agent queries candidate Tasks, chooses one, and creates a Claim |
| Human execution | A person Claims a Task whose executor policy allows human participation |
| External dispatch (planned) | A Bridge chooses a matching Agent identity, establishes its Claim, and starts its harness |

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

Proactive selection fits the current Kairos boundary, which does not control an Agent Harness. A future Bridge can start Codex, Claude Code, or another harness when a Task becomes executable.

Task organization and executor participation are independent dimensions:

| Task organization | Supported participation methods |
| --- | --- |
| Workflow | Role-aware proactive Claim today; external dispatch in the future |
| Blackboard | Role-aware proactive Claim today; external dispatch in the future |

## 4. Shared Work Context

Context inside an Agent Harness is usually temporary and local. Kairos assigns collaboration information to the work itself:

```text
WorkItem
├── Objective, background, constraints, acceptance criteria
├── Task A
│   ├── Lifecycle and responsibility
│   └── Deliverable result
├── Task B
│   ├── Lifecycle and responsibility
│   └── Deliverable result
└── Task C
    ├── Lifecycle and responsibility
    └── Deliverable result
```

> A Task belongs to a WorkItem. Its lifecycle and results express progress of the shared WorkItem.

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
| Executors | Executor kind constrains all claimants; allowed roles constrain Agents only | Executor kind constrains all claimants; allowed roles constrain Agents only |
| Execution responsibility | Established through one unique Claim | Established through one unique Claim |
| WorkItem progress | Expressed by Task lifecycle and durable results | Expressed by Task lifecycle and durable results |

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

Kairos Core represents work, provides candidate Tasks, establishes execution responsibility, and persists shared context. People participate through an interaction layer. An Agent Harness runs the agent; the planned Bridge will start a specific harness and return results.

This collaboration model can be summarized in five principles:

1. One collaborative execution is centered on an explicit Task.
2. One executor is responsible for a Task while it is being executed.
3. How a Claim is established does not change its responsibility semantics.
4. Task lifecycle changes and results express progress of the shared WorkItem.
5. Task organization and executor participation are independent.

> People and agents share a WorkItem, while each Task has one responsible executor.
> Kairos coordinates work independently of how that executor participates.
