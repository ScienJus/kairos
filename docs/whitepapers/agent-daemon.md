# Kairos Agent Daemon

Core can say which work is available and who owns it, but it deliberately does not start models or supervise local processes. Agent Daemon fills that runtime gap without moving coordination authority out of Core.

The Daemon is a long-running process bound to one Agent identity. It discovers candidates, creates Claims, starts Harnesses through an Adapter, and turns typed outcomes into durable Core facts. This leaves a deliberate three-way boundary: Core owns coordination, Daemon owns scheduling and runtime convergence, and a Harness performs only the scoped concrete work.

## The Architecture Boundary

```text
Kairos Core
  candidates / Claims / lifecycle / durable context
            ↑ Identity Token
Agent Daemon
  Scheduler / Dispatch / heartbeat / reconcile
            ↓ Executor Token
Adapter → Harness / Provider / process / workspace
```

Each Daemon instance binds:

- one Agent identity and role;
- one Adapter configuration;
- one shared execution-slot limit.

Core does not start models, manage workspaces, or infer Claim state from telemetry. Daemon does not redefine Task, Review, Failure, or Workflow/Blackboard rules.

## One Candidate, One Dispatch

A Dispatch tracks one candidate from Claim through terminal reconciliation:

```text
Dispatch
├── Task Dispatch → Task Claim
└── Coordination Dispatch → Coordination Claim
      ├── empty_blackboard
      ├── blackboard_completion
      └── work_item_acceptance
```

It fixes candidate identity, Claim, Harness run, and one terminal intent. Once finalizing begins, a lost response cannot change the intended business outcome.

If Claim response is uncertain, the Dispatch keeps the original `operation_id` and reconciles instead of creating a second Claim. After Claim success, an independent heartbeat guard spans Harness start, execution, retry, and finalization.

## Giving the Harness Only What It Needs

When creating a Claim, Daemon generates a high-entropy Executor Token and Core stores only its hash. The Token binds:

- Claim and Actor;
- `task_executor` or `coordination_executor` profile;
- current WorkItem scope.

A Harness may read required scoped context and committed Artifacts. A Task Harness may also create Artifacts and allowed nonterminal Blackboard planning objects. It cannot directly submit, fail, release, or complete work; Daemon translates the typed outcome with its Identity Token.

The Executor Token fails when the Claim ends, the WorkItem is terminal, or scope does not match.

## The Adapter Boundary

The Adapter is Daemon's minimal Harness runtime boundary:

```go
type Adapter interface {
    Probe(context.Context) error
    Start(context.Context, StartRequest) (RunRef, error)
    Observe(context.Context, RunRef) (RunObservation, error)
    Stop(context.Context, RunRef, StopReason) error
}
```

- `Probe` checks basic availability but cannot guarantee the next `Start`.
- `Start` returns a valid RunRef or an error only after proving that this attempted run does not remain active.
- `Observe` returns a snapshot; query failure does not prove termination.
- `Stop` is idempotent and best effort; `Observe` must later confirm termination.

RunRef contains no secret, but the MVP uses it only while the current process survives. Forgetting an Adapter record never counts as stopping a run.

## Turning Harness Outcomes into Business Facts

A Task Harness may return:

| Outcome | Daemon action |
| --- | --- |
| `completed` | `submit_task` with result, Artifact IDs, Review request, and legal Workflow decision |
| `decomposed` | Blackboard only; `decompose_blackboard_task` |
| `retryable_failure` | `fail_task(action=retry)` |
| `human_intervention_required` | Workflow only; `fail_task(action=await_human)` |
| `work_item_failure` | `fail_task(action=fail_work_item)` |
| `candidate_declined` | `release_claim` |

A Coordination Harness may return:

| Decision | Legal candidate | Daemon action |
| --- | --- | --- |
| `create_task` | all three | `create_blackboard_task` |
| `submit_completion` | empty / completion | `submit_blackboard_completion` |
| `accept_completion` | acceptance | `accept_blackboard_completion` |
| `candidate_declined` | all three | `release_coordination_claim` |

Daemon validates schema, mode, and candidate kind before calling Core. Large logs, patches, and files belong in Artifacts rather than result fields.

Provider authentication, quota, process crashes, invalid output, and Adapter failures are runtime failures. They never directly masquerade as a Submission, Task Failure, or WorkItem Failure.

## Runtime Convergence: Why Process Exit Is Not Enough

RunState describes the Harness: `starting`, `running`, `outcome_ready`, `runtime_failed`, `stopped`, or `lost`. DispatchState describes the whole responsibility:

```text
prepared → claimed → starting → running → finalizing → finished
                       ↑          │
                       └─ retry ──┘

starting / running / finalizing → stopping → finished | lost
```

A terminal RunState does not release a slot. The Dispatch becomes terminal only after the Harness ended or became lost and Core confirmed the Claim ended.

Timeouts, 5xx responses, and lost heartbeat responses mean unknown state. Daemon retries inside the lease safety window, then requests stop and waits to reconcile when Core returns. If Core authoritatively reports an ended Claim, fencing loss, owner mismatch, or WorkItem cancellation, Daemon stops renewal and later writes immediately.

After a lost response, Daemon checks Claim end reason and associated Submission, Failure, decomposition, or WorkItem history against its frozen terminal intent. Without enough evidence it remains finalizing instead of assuming success or failure.

For example, Core may commit a Submission and end the Claim while the response is lost on the network. Blindly retrying with a new intent could duplicate work; treating the timeout as failure could report a result that Core already accepted as missing. The frozen intent and durable history let Daemon recognize the committed outcome without guessing.

## Scheduling and Suppression Without a Global Queue

Task and Coordination Dispatches share slots. Daemon rotates candidate kinds, but `find_work` order is not global priority and local rotation does not establish cross-Daemon fairness.

Systemic Adapter or Provider failure pauses new Claims and uses bounded-backoff `Probe` calls for recovery. For one candidate generation:

- exhausted infrastructure retries enter cooldown, then quarantine after the cross-Claim budget is exhausted;
- `candidate_declined` releases and directly quarantines that generation;
- new business state, plan, or result creates a new generation and invalidates old suppression;
- health recovery removes only the global pause, preserving cooldown, budget, and quarantine.

Suppression remains in Daemon memory and never prevents another Daemon from claiming.

## Crash Recovery: What Can and Cannot Be Recovered

The MVP keeps Dispatch state in memory and does not recover old runs after restart. Graceful shutdown tries to stop known Harnesses and release Claims. After a crash, the Core reaper eventually recovers unrenewed Claims and invalidates Executor Tokens.

That protects Kairos state only. It does not guarantee termination of orphan processes, workspace cleanup, or reversal of external side effects. Each Dispatch needs an isolated workspace; untrusted work also needs an OS user, cgroup, container, or Kubernetes Pod boundary.

Cross-process recovery requires a durable Dispatch journal, external runtime supervision, and Adapter reattach/observe/stop capabilities. Persisting RunRef alone is insufficient.

## Platform Observation Without Control

Daemon best-effort reports instance snapshots and meaningful events to Core with its Agent Identity Token. Telemetry failure never changes scheduling, Claims, heartbeats, or outcome handling. Reported status is operational observation; Core business records remain authoritative.

See the [platform observability design (Chinese)](../daemon-observability-design.zh-CN.md) for connectivity, report/event, retention, and console semantics.

## Agent Daemon Invariants

- Core is the sole authority for candidates, Claims, and business outcomes.
- A Harness uses only short-lived credentials inside one Claim scope.
- Runtime failures never masquerade as business failures.
- No replacement run starts in one Claim until the old Harness is confirmed ended.
- A Dispatch releases its slot only after both Harness and Claim converge.
- Telemetry, logs, and in-memory scheduling state never participate in Core correctness.
