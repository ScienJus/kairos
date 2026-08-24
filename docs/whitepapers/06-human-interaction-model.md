# Kairos Human Interaction Model

> A human operations console organized around the workspace, attention, and WorkItem detail

## Abstract

Kairos gives people one operational workspace for observing complete work, handling items that need human attention, and opening a WorkItem to act on its Tasks. The interface follows the work model directly instead of imposing a generic collection-view metaphor on it.

Inside a WorkItem, Workflow uses a flow graph and Blackboard uses a hierarchical Task workspace. People can inspect agent activity, Claim eligible human Tasks, submit results, handle Reviews, and participate in planning where the coordination mode allows it.

## 1. Interaction Structure

The operations console has three connected surfaces:

```text
Workspace
├── All Work
└── Needs Human
        ↓ open WorkItem
WorkItem Detail
├── Workflow   → Flow Graph
└── Blackboard → Task Hierarchy
        ↓ open Task
Task Detail and Actions
```

The workspace summarizes complete WorkItems. Coordination and execution remain inside each WorkItem, where Tasks are presented according to Workflow or Blackboard semantics.

## 2. Workspace

The workspace separates work that is active from work that has reached a terminal state. It provides a direct path from a WorkItem's title, objective, and status into its coordination detail.

The current Needs Human projection aggregates:

- pending Reviews;
- unclaimed Pending Tasks with `executor=human`;
- WorkItem completion proposals awaiting human acceptance.

This is an action-oriented projection of the same durable work model, not a separate queue with independent lifecycle semantics. It does not currently include `executor=either` Tasks, even though a person may Claim those Tasks through their detail surface.

## 3. WorkItem Progress

Kairos does not maintain a standalone mutable `Task.Progress` field. A WorkItem's progress is expressed by the state and durable records of its Tasks:

```text
Task creation and decomposition
        ↓
Claim and active responsibility
        ↓
Submission, Review, Failure, or Skip
        ↓
Task Graph and WorkItem state advance
```

Claiming, submitting, reviewing, failing, skipping, decomposing, and creating follow-up Tasks all change the observable progress of the owning WorkItem. Results and Artifacts preserve what each completed execution contributed.

WorkItem detail therefore combines the WorkItem objective and status with its Task structure and opens per-Task responsibility, result, Review, failure, Artifact, and action details.

## 4. Workflow Detail

A Workflow WorkItem uses a flow graph to present formal structure and runtime history. The graph combines the immutable Definition with concrete Task instances so unreached nodes remain distinguishable from executable work.

A graph node shows current lifecycle state, executor type, allowed Agent roles, and the number of runtime instances. Selecting a concrete Task opens its responsibility summary, acceptance criteria, latest submitted result, Artifacts, complete Review and Failure histories, and currently available actions.

People can Claim eligible human Tasks, inspect agent submissions, handle Reviews, and make configured progression decisions when submitting their own work. Every action remains subject to Workflow prerequisites and Review rules.

## 5. Blackboard Detail

A Blackboard WorkItem uses a hierarchical Task workspace to present the plan currently shared by collaborators. It shows parent-child hierarchy, tags, description, and lifecycle state. Selecting a Task opens its detail and available actions.

The current interaction surface supports:

- creating Tasks and adding child Tasks;
- decomposing a claimed Task into an aggregate with children;
- Claiming and submitting Tasks;
- requesting or handling Review;
- skipping a Pending Task with a durable reason;
- submitting or accepting WorkItem completion when current Tasks converge.

These actions directly update the shared Task Graph and therefore the observable progress of the WorkItem. Suggested Relations remain available in the durable model, HTTP API, MCP tools, and execution context, but the current console neither displays nor creates them.

## 6. History

Kairos durably appends WorkItem events for Task creation, Claims, submissions, Reviews, failures, and Workflow progression decisions. `GET /api/v1/tasks/{id}` already returns normalized Claim, Submission, Review, Failure, and Transition Decision history for the selected Task. The current console renders only the responsibility summary, latest submitted result, complete Review history, and complete Failure history; it does not yet render Claim history, every Submission, or Transition Decisions.

A complete WorkItem-wide event timeline in the operations console is planned. Until that surface exists, the persisted event stream is an internal audit record rather than a user-visible capability.

## 7. Semantic Consistency

Every interface action operates on the unified work model and follows the current coordination mode:

- Workflow keeps formal dependencies and Review requirements authoritative;
- Blackboard lets collaborators expand and reorganize the shared plan through its supported planning operations;
- executor type determines eligible actor kinds and, for Agents only, allowed roles further narrow eligibility;
- Claim identifies the one concrete actor responsible during execution;
- Task lifecycle changes and durable results express progress of the owning WorkItem.

## 8. Core Definitions

```text
Workspace       = operational overview of complete WorkItems
Needs Human     = selected human-attention signals from the current projection
WorkItem Detail = coordination state and progress of one objective
Task Detail     = responsibility, history, and actions for one execution unit
```

> The workspace helps people find what needs attention; WorkItem detail shows how the work is advancing; Task detail lets them act.
