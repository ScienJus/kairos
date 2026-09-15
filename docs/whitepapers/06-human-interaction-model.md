# Kairos Human Interaction Model

> How people find work that needs them, understand progress, and act without reading an agent transcript

## Abstract

People should not have to inspect every agent transcript to understand what is happening. Kairos provides one workspace for seeing active WorkItems, finding decisions that need a person, and opening the exact Task where action is required.

Workflow appears as a flow graph; Blackboard appears as a Task hierarchy that can keep evolving. From those views, people can inspect agent activity, take human Tasks, submit results, handle Reviews, and help plan the work when the selected mode allows it.

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

The workspace groups ongoing work separately from completed, cancelled and failed WorkItems. It provides a direct path from a WorkItem's title, objective, and status into its coordination detail. A failed Workflow is recoverable: Human Continue returns the same WorkItem to Open when recovery is eligible, while Start over creates a new WorkItem and leaves the source Failed. This grouping does not make Failed an irreversible lifecycle state.

The current Needs Human projection aggregates:

- pending Reviews;
- unclaimed Pending Tasks with `executor=human`;
- Working Tasks actively claimed by the requesting Human, including `executor=either` Tasks;
- WorkItem completion proposals awaiting human acceptance.

This is an action-oriented projection of the same durable work model, not a separate queue with independent lifecycle semantics. Unclaimed `executor=either` Tasks remain outside this projection; they appear once the requesting Human claims them. Another actor’s Working Tasks are excluded. Ownership is selected before pagination, and a release removes the active-claim entry (an unclaimed Human Task remains eligible).

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
- terminally cancelling an active WorkItem with a durable reason.

These actions directly update the shared Task Graph and therefore the observable progress of the WorkItem. Suggested Relations remain available in the durable model, HTTP API, MCP tools, and execution context, but the current console neither displays nor creates them.

The console exposes empty-Blackboard planning, converged completion, and WorkItem acceptance decisions only to Human identities. Agents perform those decisions through MCP so they acquire and maintain the required Coordination Claim before reasoning begins.

Cancellation is deliberately a WorkItem-level management action. The detail page exposes it only to Human identities while the WorkItem is active or awaiting acceptance, confirms the reason, and then shows the recorded actor, time, and reason in the read-only terminal view.

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

> The workspace answers “Where am I needed?” WorkItem detail explains the overall progress, and Task detail provides the next action.

Open Workflows also list unreplaced Failed Tasks in Human Attention, regardless of executor requirement. Continue execution on the WorkItem creates new attempts for all current failed Tasks; old Tasks remain history and leave the actionable list. Failed Workflow WorkItems offer Continue execution and Start over (new WorkItem), without arbitrary-stage selection. Start over carries current failure reasons and the submitted Human instructions; the console prompts Humans to list external outcomes to reuse because scoped executors cannot read source history.
