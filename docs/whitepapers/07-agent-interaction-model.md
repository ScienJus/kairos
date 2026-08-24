# Kairos Agent Interaction Model

> One interaction model for agents participating in Workflow and Blackboard

## Abstract

Kairos provides agents with one Task interaction process. An agent can proactively discover and choose work today; a future Bridge can start it with a Task selected for its role. Both paths establish responsibility around one Task, load work context, execute, and submit durable results whose lifecycle effects contribute to WorkItem progress.

Workflow and Blackboard share the execution process while exposing different planning capabilities to agents. Workflow lets an agent make decisions at configured points; Blackboard lets an agent continuously adjust the Task Graph.

## 1. Interaction Process

An agent enters execution in one of two ways:

```text
Proactive: discover candidates → choose Task ─┐
                                              ├→ create Claim → execute Task
Bridge dispatch: receive Task ────────────────┘
```

The complete process is:

```text
discover / receive
        ↓
inspect
        ↓
claim
        ↓
execute
        ↓
heartbeat while executing
        ↓
submit result
```

Before execution, the agent reads necessary context and confirms the Task. The agent creates a leased Claim before work begins, establishing unique execution responsibility; a future Bridge will establish the same Claim for the selected Agent identity. During execution, the agent renews that lease with heartbeat calls and may request a different duration for each interval. The Claim and Task state show active work, while submissions, Reviews, failures, decisions, and Artifacts durably describe its contribution to WorkItem progress. Reaching `lease_until` makes the Claim eligible for reaping but does not revoke it: the current agent may still renew or submit until the reaper commits. After reaping, the agent must stop and cannot revive or submit through the old Claim.

## 2. Discovering Work

An agent discovers only Tasks that permit agent execution:

```text
executor = agent | either
+ role matched
```

The coordination mode determines where candidate Tasks come from:

| Mode | Candidate Tasks |
| --- | --- |
| Workflow | Required Tasks whose prerequisites are satisfied, plus optional Tasks that were retained; role and graph state decide eligibility, not tags |
| Blackboard | Tasks matching tags and query context |

Candidate results provide enough information to compare work, including the WorkItem summary, Task objective, coordination mode, tags, and current eligibility reason. An agent can load full context before creating a Claim.

An empty Blackboard exposes its WorkItem directly as a candidate. The agent reads the objective and global instructions, creates the first Task, and then returns to regular Task discovery.

## 3. Task Context

An agent receives five categories of information while executing a Task:

```text
Definition Context
    Description, Agent Instructions, Suggested Tags

WorkItem Context
    Objective, background, constraints, acceptance criteria

Task Context
    Task description, delivery requirements, lifecycle, and prior records

Related Results
    Results and Artifacts from related Tasks

Coordination Context
    Current mode, Task Relations, available decisions
```

Definition Context applies to every WorkItem in the same collaboration space. Workflow Coordination Context contains formal prerequisites, optional decisions, and Review configuration. Blackboard context contains suggested relations, tags, and the current shared work state.

An agent can load more history and results on demand. Default context should prioritize information directly relevant to the current Task.

Workflow Context supplies controlled upstream runtime Task summaries ordered by distance, including durable results, currently legal Choice Groups, direct targets, optional Relation labels and agent guidance, and optional Tasks that can be decided in this progression. Relation guidance helps the agent interpret existing progression choices but does not create a new conditional branch. The agent submits the Task IDs it wants to skip, and Kairos partitions relations and unfolds paths according to the Workflow Definition. Blackboard Context supplies the current shared Tasks and suggested relations. Full Task context remains restricted to the target Task's role and active Claim.

## 4. Execution and Submission

During execution, heartbeat renews the Claim but does not write a separate mutable progress note. WorkItem progress changes through durable Task operations such as Claim, decomposition, submission, Review, failure, Skip, and follow-up Task creation.

When submitting a Task, the agent records completed work, discovered issues relevant to the delivery, produced results, and Artifacts. Kairos creates an immutable Submission under the Task and makes it part of shared WorkItem context. A later submission after rework creates a new Submission instead of overwriting the previous result.

If a submission requires human Review, the current Claim ends with the submission, and the Task does not require agent liveness while `InReview`. Rejection returns the Task to the candidate set for continuation under a new Claim.

```text
Task
├── Claim and lifecycle history
├── Submission 1
│   ├── Result
│   └── Artifacts
└── Submission 2
    ├── Result
    └── Artifacts
```

A submission can also carry progression decisions allowed by the current coordination mode. Kairos updates the Task Graph according to those decisions and mode rules.

Every mutation request can include a caller-generated Operation ID. When the same identity retries the same request, Kairos returns the original result. Reusing an Operation ID for a different request returns a conflict.

When an agent cannot complete a Task, it can submit a failure reason and either reopen the Task or fail the entire WorkItem. Reopening can add a Retry Prompt. Failure history and prompts enter the full Task context read by future executors.

## 5. Workflow Capabilities

In Workflow, an agent executes predefined Tasks and makes decisions where configuration allows:

- choose work from multiple candidate Tasks;
- decide whether optional Tasks connected to the current Task should be retained or skipped;
- decide whether to request human Review under `executor_decides`.

The agent submits optional Task decisions with the current Task. Kairos aggregates decisions from multiple predecessors; if any executor retains the Task, it enters the candidate set.

Workflow continues guaranteeing the formal Task Graph, required Tasks, and required Review.

## 6. Blackboard Capabilities

In Blackboard, an agent participates in both execution and planning. It can:

- create new Tasks;
- decompose existing Tasks;
- create Tasks with discovery tags;
- add suggested Task Relations;
- mark Tasks that no longer provide value as Skipped;
- request human Review based on current results;
- decide whether the WorkItem objective has been satisfied.

These changes enter the shared Task Graph. Later people and agents see the latest work structure and results.

## 7. Planned Bridge

A Bridge connects Kairos to a particular Agent Harness:

```text
Kairos Candidate Task
         ↓
       Bridge
         ↓
Codex / Claude Code / Other Harness
```

A future Bridge can choose a Task for a matching Agent role, start the agent, provide context, and return lifecycle operations and results. Proactive agent participation and Bridge dispatch use the same Task, Claim, and submission semantics.

The Kairos agent interaction model is therefore independent of a specific Harness and of how an agent begins execution.

## 8. MCP and Skill Surface

Kairos exposes the proactive execution loop through a stateless Streamable HTTP MCP endpoint. Each HTTP request independently resolves the actor through Trusted or Authenticated Mode, so identity does not depend on an MCP session and is never accepted as a tool argument.

The MCP surface contains work discovery, Task context, terminal-capable WorkItem context, Claim creation and heartbeat, external Artifact registration, Base64 managed Artifact upload, submission, failure, Claim release, and Blackboard Task creation. `claim_task` and `heartbeat_claim` accept an optional requested `lease_seconds`; the server returns the granted duration and `lease_until`. In Blackboard task context, the top-level `task` is the current task; `blackboard.tasks` intentionally excludes it and exposes `blackboard.current_task_id` for correlation. Responses use compact `snake_case` execution views instead of exposing the full persistence model. Definition and Identity administration and human Review decisions stay outside the Agent surface. A repository-level Codex Skill supplies the execution and heartbeat loop plus idempotency discipline to compatible harnesses, while `.codex/config.toml` connects Codex to the local project server.

> One execution protocol, two coordination modes.
