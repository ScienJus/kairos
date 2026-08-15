# Kairos Human Interaction Model

> A human work interface centered on WorkItem List and Kanban

## Abstract

Kairos presents WorkItem collections through List and Kanban. List provides high-density management, while Kanban provides an overview of the state of complete work. Both views operate on the same data, filters, and action results.

Inside a WorkItem, Tasks are presented according to the coordination mode: Workflow uses a Flow Graph and Blackboard uses a Checklist. People use these interfaces to observe agent work and can also execute Tasks directly.

## 1. Interaction Structure

The Kairos human interface has two levels:

```text
WorkItem Collection
    ├── List
    └── Kanban
          ↓ open WorkItem
WorkItem Detail
    ├── Workflow   → Flow Graph
    └── Blackboard → Checklist
```

List and Kanban both operate on WorkItems. Tasks exist inside WorkItem detail and are operated according to Workflow or Blackboard semantics.

WorkItem detail also provides an append-only event history for tracing Task creation, Claims, submissions, Reviews, failures, and Workflow progression decisions.

## 2. List

List manages large numbers of WorkItems and focuses on:

- search, filtering, and sorting;
- high-density field presentation;
- quickly locating current executors, progress, and update time;
- bulk management;
- opening WorkItem detail.

List answers:

```text
What work exists?
Which work matches the current conditions?
Which specific WorkItem do I need?
```

## 3. Kanban

Kanban organizes the same WorkItems by overall state so people can quickly understand how work is flowing.

It serves four primary purposes:

### 3.1 Shared Work State

Kanban summarizes work that has not started, is progressing, needs attention, or has ended. People can understand overall progress without opening each WorkItem.

### 3.2 Backlog and Exception Discovery

Card distribution across columns reveals work that has not advanced for a long time, excessive work in progress, and items awaiting human action.

### 3.3 Connecting People and Agents

A WorkItem card can show current Task progress, executors, and pending Review prompts. A person can open detail to inspect agent results, handle Review, or claim a Task intended for human execution.

### 3.4 Unified Entry Point

Workflow and Blackboard share one Kanban. Cards represent complete work; the coordination mode determines how the inside of a WorkItem advances.

> Kanban is the shared operational view of work performed by people and agents.

## 4. WorkItem Card

A Kanban card summarizes one complete work item and can include:

- title and objective summary;
- Workflow or Blackboard indicator;
- Task completion progress;
- current executor summary;
- pending Review prompt;
- tags, priority, and update time.

The card contains only enough information to understand work state. Concrete Tasks, dependencies, results, and actions appear in WorkItem detail.

## 5. Workflow Detail

A Workflow WorkItem uses a Flow Graph to present its internal Tasks:

```text
Design
 ├──→ Frontend implementation ─┐
 ├──→ Backend implementation  ─┼→ Integration test
 └──→ Documentation           ─┘
```

Each Task node can show:

- current state;
- executor type and responsible executor;
- required or optional;
- Review configuration and current Review state;
- entry points for progress and results.

In the Flow Graph, people can claim human Tasks, inspect agent submissions, and handle Reviews. When a person executes an upstream Task, they can also decide about downstream optional Tasks.

Flow Graph actions follow Workflow semantics. The system continues enforcing prerequisites, required Tasks, and required Review.

## 6. Blackboard Detail

A Blackboard WorkItem uses a Checklist to present dynamically formed Tasks:

```text
[x] Design the login approach
[ ] Implement login
    [ ] Implement password login      backend, auth
    [ ] Implement session management  backend, auth
    [ ] Add brute-force protection    security
[ ] Test login                        test
```

Checklist supports:

- creating, decomposing, and adjusting Tasks;
- viewing tags and suggested relations;
- claiming Tasks;
- updating progress and results;
- requesting or handling Review;
- marking a Task that no longer provides value as Skipped.

Checklist directly reflects Blackboard’s dynamic planning. Collaborators continuously add to and correct their shared understanding of the WorkItem around one list.

## 7. Semantic Consistency

Interface actions ultimately operate on the unified Work Model and follow the current coordination mode:

- moving a Kanban card must satisfy WorkItem state rules;
- Workflow Flow Graph enforces formal dependencies and Review requirements;
- Blackboard Checklist allows collaborators to adjust Tasks dynamically;
- Claim always establishes unique Task execution responsibility;
- people and agents record progress and results in the same way.

The same interface action can therefore have one consistent form while its validity is determined by Workflow or Blackboard semantics.

## 8. Core Definitions

The Kairos human interaction model can be summarized as:

```text
List       = management view of WorkItems
Kanban     = state and flow view of WorkItems
Flow Graph = execution view of Workflow Tasks
Checklist  = collaboration view of Blackboard Tasks
```

> List helps people find work; Kanban helps them understand its flow; WorkItem details let them act.
