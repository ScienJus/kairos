# Kairos Execution Collaboration Model

Collaboration needs room for many people and agents without making responsibility collective and vague. Kairos therefore separates who is allowed to do a Task from who has actually taken it on. Task configuration describes eligibility; a Claim names the current responsible executor.

The same separation continues after execution. Submissions, Reviews, Failures, and Artifacts record what happened without depending on the executor session that produced them.

## The Execution Boundary

A Task should be one coherent, deliverable unit of work:

```text
choose Task → create Claim → execute → submit or end responsibility
```

A Task should not represent unbounded team work or require several executors to share responsibility simultaneously. Split parallel work or distinct specialties into separate Tasks.

## Eligibility and Responsibility: Being Allowed Is Not Owning

`executor` allows a Human, Agent, or either to execute a Task. `allowed_roles` further restricts Agent roles. These fields define an eligible set; they do not assign responsibility.

A Claim turns eligibility into responsibility:

- a Task has at most one active Claim;
- mutations require the current Claim or an explicit Human management action;
- the Claim ID is a fencing token, so an ended Claim cannot revive or overwrite a later executor's result.

A Human Claim lasts until submission, failure, release, or a management action. An Agent Claim is a renewable lease: heartbeat extends it, and after expiry the Core reaper ends it transactionally before reopening the Task. Time passing alone does not revoke responsibility outside that transaction.

This matters during recovery. If an Agent stops heartbeating, another executor cannot safely assume ownership merely because its local clock passed `lease_until`. Core must first end the old Claim; only then can a new Claim fence the abandoned executor out.

## Submission, Review, and Failure

Each formal delivery creates an immutable Submission linked to the Claim that produced it. Rework creates another Submission instead of overwriting the earlier result.

When Review is required, submission ends the Claim and moves the Task to `in_review`. Approval completes it; rejection returns it to `pending`, where a new Claim owns the revision.

When an executor cannot complete, Kairos creates a Failure and ends the Claim. Whether the work retries, waits for a Human, or ends the WorkItem depends on the coordination mode and explicit failure action. See the [Agent Interaction Model](07-agent-interaction-model.md) and [API Reference](../api-reference.md) for exact behavior.

## Shared Context Belongs to the Work

Shared context belongs to the WorkItem and Task, not to an executor session. It consists of:

1. intent: WorkItem objective, constraints, acceptance criteria, and Task description;
2. coordination: related Tasks, Relations, available decisions, and upstream results;
3. history: Claims, Submissions, Reviews, Failures, and Artifacts.

An executor need not read every unrelated record, but it must receive the upstream facts and feedback required for its current Task.

## Responsibility Models: Three Ways to Take Work

| Participation | How responsibility is established |
| --- | --- |
| Proactive Agent | The Agent discovers a matching candidate and creates a Claim |
| Human execution | A Human Claims a Task whose policy allows Human execution |
| Agent Daemon dispatch | The Daemon Claims with its bound identity, then starts a Harness |

All three use the same Claim, Submission, and invalidation semantics. Distribution does not change execution responsibility.

## Runtime Boundaries: Core, Daemon, and Harness

- **Kairos Core** owns candidate eligibility, Claims, domain lifecycle, and durable context.
- **Agent Daemon** owns scheduling, renewal, Harness lifecycle, and result convergence.
- **Agent Harness** performs concrete work under scoped credentials and has no cross-Task coordination authority.

In short, eligibility says who may execute, a Claim records who is responsible, and Submissions and Artifacts preserve what that executor delivered.
