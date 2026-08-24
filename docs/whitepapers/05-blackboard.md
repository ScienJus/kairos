# Kairos Blackboard Mode

> A shared Task Graph formed continuously during execution

## Abstract

Blackboard begins with an explicit WorkItem objective while allowing the Task Graph to be empty or incomplete. People and agents jointly create, select, decompose, extend, and skip Tasks during execution, allowing the plan to evolve with their understanding of the work.

In Blackboard, Task Relation expresses progression guidance. It helps executors understand the work structure while preserving their judgment over execution order and next steps.

## 1. Blackboard Structure

A Blackboard Definition defines a shared collaboration space with a name, description, Agent Instructions, and Suggested Tags. It does not predefine a Task Graph. Every WorkItem is bound to a fixed Definition Version and supplies its own objective, background, constraints, and acceptance criteria within that space:

```text
WorkItem: Implement login
Tasks: []
```

Collaborators create initial Tasks from their current understanding:

```text
[ ] Design the login approach
[ ] Implement login
[ ] Test login
```

New information continues changing the structure during execution:

```text
[x] Design the login approach
[ ] Implement password login
[ ] Implement session management
[ ] Add brute-force protection
[ ] Test login
```

The Task Graph in Blackboard is a shared representation of the current understanding of the work.

Blackboard structural appends are committed against the latest server state. When multiple collaborators concurrently create different Tasks or Relations, their operations are serialized and can all succeed. WorkItem Version is a server-maintained structural revision. Operation ID identifies request retries, while Task Version protects state changes to one Task.

When the Task Graph is empty, the WorkItem itself is exposed as candidate work. A collaborator reads the overall objective and Blackboard Instructions, then creates the first Task. WorkItem Tags support this initial discovery.

Suggested Tags provide an open vocabulary such as `module:*` or `kind:*`. Agents choose concrete tags from actual Task content when creating Tasks. Suggestions are neither permissions nor format constraints.

## 2. Planning and Execution

Blackboard keeps planning active throughout execution:

```text
Observe current work
        ↓
Create, decompose, or extend Tasks
        ↓
Choose and execute a Task
        ↓
Update WorkItem progress through Task lifecycle and results
        ↓
Observe WorkItem again
        ↺
```

Collaborators can:

- create new Tasks;
- decompose a larger Task into clearer deliverable units;
- append child Tasks to an unfinished aggregate Task;
- add suggested relations between Tasks;
- mark a Task that no longer provides value as Skipped when new information appears;
- plan follow-up work from existing results.

Completed Tasks and their results remain available as context for later decisions.

Tasks can form a hierarchy. After claiming a Task that has not produced a result, the executor can decompose it into an initial set of child Tasks. The parent immediately ends its Claim, enters `WaitingChildren`, and no longer produces its own Submission. Its result is aggregated from descendants.

Blackboard does not impose a structured Artifact contract. The dynamically authored Task prompt and acceptance criteria tell the executor what to deliver. Any submitted Artifacts become part of the WorkItem-wide shared Artifact collection.

`WaitingChildren` represents an open aggregation scope. While the WorkItem remains open, collaborators can append child Tasks to it. After every direct child is completed or skipped, the parent recursively completes and closes. Regular execution Tasks, aggregate Tasks, and Task Relations separately represent execution, work decomposition, and suggested order.

## 3. Task Relation

Blackboard uses Task Relation to express the currently suggested progression order:

```text
Design ⇢ Implement ⇢ Test
```

A downstream Task can remain a candidate while its predecessor is unfinished. The executor sees the suggested relation and relevant predecessor results, then decides whether work should begin.

For example, implementation can start before design is fully complete. Collaborators can add a suggested Relation when creating the shared structure. Existing Relations are immutable in the current API: they cannot be updated or deleted.

> Task Relation records shared judgment about how work should proceed.

## 4. Task Discovery and Execution

Blackboard candidate Tasks come from the current shared space:

```text
Pending executable leaf Task
+ no current Claim
+ matches query context
```

Query context can include tags, executor type, and WorkItem scope. For example, an agent can search for Tasks tagged `backend` and `auth`, while a person can use the interface to view Tasks suitable for human execution.

A Task can configure its executor type:

```text
executor:
  agent
  human
  either
```

A person or agent can choose a candidate proactively, and a future Bridge can automate the same role-aware choice. A Claim establishes unique execution responsibility for one concrete actor on the selected Task.

## 5. Autonomy

Blackboard continuously exposes planning autonomy to collaborators:

- decide which current work is worthwhile;
- create missing Tasks;
- decompose or extend work and add suggested relations;
- replan next steps from results;
- decide whether human Review is needed.

An executor can request human Review while submitting a result. A person can also require the next submission to enter Review before the Task formally ends. Review acts on the current Task and does not need preconfiguration in the initial Blackboard structure.

When an executor submits a result for Review, the system creates an immutable Task Submission from the current Claim, links the Review to that Submission, ends the Claim, and moves the Task to `InReview`. Every Submission, Review decision, and feedback record is retained chronologically in shared Task context. There is no Active Claim during Review. The Reviewer acts on the Review record and does not claim another Task. Approval formally ends the Task; rejection returns it to `Pending`, where the original or another executor can claim it again.

Other unfinished, unclaimed Tasks can continue executing. Since Blackboard Task Relations are progression guidance, one Task being in Review does not automatically prevent other Tasks from becoming candidates.

Blackboard autonomy comes from continuous planning and therefore does not need preconfigured optional Task placeholders. Collaborators create only the Tasks that currently provide value, and can later mark a Task as Skipped with a reason if their judgment changes.

## 6. WorkItem Completion

After the current Tasks converge, a collaborator evaluates whether the WorkItem objective requires more work:

```text
Every current Task is Completed or Skipped
                    ↓
        Blackboard completion candidate
         ├── more work needed → create follow-up Tasks
         └── objective met    → submit completion result
                                      ↓
                              acceptance_mode
```

New findings can expand the Task Graph at any point. When the objective is already satisfied, remaining low-value Tasks can be marked Skipped. Task convergence leaves the WorkItem `open`; it does not itself declare completion or start acceptance. A collaborator must submit a durable completion result. Then `acceptance_mode` applies: `none` completes immediately, `agent` exposes an Agent acceptance candidate, and `human` enters human acceptance and is shown in the human-attention queue. An acceptance actor may accept the proposal, while an Agent acceptance actor may instead create more Tasks and return the WorkItem to execution. The same explicit completion submission also applies to an empty Blackboard.

> Blackboard grows a shared plan while people and agents execute the work.
