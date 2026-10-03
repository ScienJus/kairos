# Kairos Blackboard Mode

Some work begins with a clear objective but no honest way to predict the full path. Forcing it into a fixed Workflow only hides uncertainty in vague steps. Blackboard keeps the objective durable while allowing the plan to emerge from evidence.

A WorkItem may therefore start with no Tasks at all. People and agents add Tasks, hierarchy, and advisory Relations as they learn, but they must still make an explicit, reviewable decision when they believe the objective is complete.

## A Plan That Can Grow

A Blackboard Definition supplies default description, Agent guidance, suggested tags, and acceptance policy, but not a complete Task Graph.

WorkItem Version is a server-maintained structural revision. Concurrent collaborators may append distinct Tasks or Relations and have the operations serialize successfully; Task lifecycle changes still use Task Version. Resource creation uses `operation_id` to replay safely after a lost response.

## How Collaborators Change the Plan

Collaborators may:

- create a top-level Task;
- decompose a claimed Task with no result into initial children;
- append children while an aggregate remains open;
- append advisory Relations between Tasks;
- skip an obsolete, unclaimed Task.

Decomposition ends the parent Claim and moves the parent to `waiting_children`. The parent produces no Submission; its children collectively express completion.

Tasks and Relations created by a Harness through Executor Credentials become shared facts once committed. They do not roll back if the originating Claim later fails or releases.

## Relations and Candidates: Advice, Not Hidden Dependencies

A Blackboard Relation suggests progression. It helps an executor interpret context but does not block a later Task while a predecessor is unfinished. Existing Relations are immutable; changed plans are expressed through appended structure and Skip reasons.

An ordinary Task is a candidate when:

```text
state = pending
+ no active Claim
+ WorkItem permits execution
+ executor kind, Agent role, and queried tags match
```

When the Blackboard is empty, converged, or awaiting Agent acceptance, the WorkItem itself produces a coordination candidate. An Agent creates a Coordination Claim before reading full context and deciding to create work, submit completion, or accept it.

## Reviewing an Individual Result

An executor may request Human Review at submission, and a Human may require the current Task's next Submission to enter Review.

Submission ends the Claim and enters `in_review`. Approval completes the Task; rejection returns it to `pending`, retaining every Submission, Review, and feedback record.

## Deciding That the Objective Is Complete

All Tasks being completed or skipped means only that the current plan converged; the WorkItem stays `open`. A collaborator must either create follow-up work or submit a durable WorkItem completion result.

Imagine an investigation whose original Tasks are all complete, but the final evidence reveals that a rollout check is still needed. Automatic completion would close the WorkItem too early. Blackboard instead presents a coordination decision: add that check, or state why the existing result is sufficient and submit completion.

Only then does `acceptance_mode` apply:

| Mode | Result |
| --- | --- |
| `none` | Complete immediately |
| `agent` | Produce an Agent acceptance candidate |
| `human` | Enter Human acceptance |

An acceptance actor may accept completion. An Agent acceptance actor may instead create a Task, discard the proposal, and reopen execution.

## Blackboard Invariants

- Committed Tasks, Relations, Submissions, Reviews, and Artifacts are never overwritten by a new plan.
- Relations remain advisory rather than hard blocking conditions.
- Coordination Claims protect analysis of an empty graph, completion, and Agent acceptance.
- The WorkItem completes only after an explicit result passes its acceptance policy.
