# Kairos Agent Daemon

> Automatic dispatch and execution control between Kairos and external Agent Harnesses

## Abstract

Kairos Agent Daemon is a long-running process bound to one Agent Identity. It discovers work, acquires
Claims, starts Harnesses through Adapters, and translates typed Harness results into durable Core state.
Core owns collaboration facts and execution responsibility; Adapters own concrete runs; Claims connect
the two.

## 1. Architecture and responsibilities

Each Daemon instance binds one Agent Identity, one Harness Adapter configuration, and a maximum number
of execution slots. It uses an ordinary Agent Identity Token. Core resolves its Actor and Role, and the
Daemon uses that identity for discovery, Claims, heartbeat, and terminal writes. The Claim executor is
that Agent.

Core owns candidate eligibility, Task and Coordination Claims, and the domain rules for Tasks,
WorkItems, Reviews, Failures, Artifacts, Workflow, and Blackboard. The Daemon owns scheduling, lease
renewal, Harness lifecycle, and result finalization. Adapter/Harness configuration supplies models,
Providers, processes, containers, remote services, and workspace isolation.

```text
                     Kairos Core
                 ▲ HTTP      ▲ scoped MCP
                 │           │
           Agent Daemon    Harness
                 │           ▲
                 └─ Adapter ─┘
                    Probe / Start / Observe / Stop
```

The Daemon controls execution through HTTP. The Harness uses MCP for dynamic context, Artifacts, and
authorized Blackboard collaboration. Daemon and Core have independent process lifecycles. External
process startup cannot be exactly-once with a database transaction; Core state correctness relies on
Claims, fencing, and terminal reconciliation.

## 2. Dispatch and lifecycle ownership

A Dispatch associates one Claim with controlled Harness execution:

- Task Dispatch binds a Task Claim and executes a concrete Task.
- Coordination Dispatch binds a WorkItem Coordination Claim for an `empty_blackboard`,
  `blackboard_completion`, or `work_item_acceptance` decision.

Both share one control loop:

```text
discover candidate → generate operation_id and Executor Token → Claim → start heartbeat
→ Adapter starts Harness → dynamic context and execution → outcome → Daemon writes Core
```

The Daemon exclusively owns Claim acquisition, heartbeat, release, and lifecycle operations including
submit, fail, decompose, skip, Workflow transitions, Review requests, and Blackboard completion/acceptance.
Executor Credentials limit direct Harness calls; terminal intents return through outcomes. WorkItem
cancellation remains human-only: the Daemon responds to external cancellation rather than requesting it
for the Harness.

Coordination Dispatch reuses existing Core operations. One create intent produces one Task; batch root
Task and Relation creation is a separate Blackboard capability.

## 3. Executor Credentials

### Issuance and authentication

An Executor Credential is attached to its Claim. Before claiming, the Daemon generates a Token with the
`krs_claim_` prefix and 256 bits of random entropy. Core stores only the complete Token's hash in the
Claim creation transaction; the Adapter's secret channel delivers plaintext to the Harness. Ordinary
Agents claiming for themselves omit this optional Token.

Claim retries reuse the same `operation_id + executor_token`, keeping plaintext credentials out of
idempotency records. Other resource-creating operations also use stable IDs owned by the direct caller:
the Daemon manages its Claims and terminal creation operations; the Harness manages its direct Artifact
and planning writes.

Both Token types use `Authorization: Bearer <token>`. Only Tokens matching the complete `krs_claim_`
format with 32 bytes in canonical unpadded base64url enter Claim lookup; all others enter Identity
lookup. Failed Claim authentication never falls back to Identity lookup. Task and Coordination Claims share the prefix.
Core performs a union over their uniquely indexed `executor_token_hash` columns, derives the Profile
from the matched Claim type, and rejects multiple matches.

### Permissions and lifetime

Both Profiles share `scoped_read`: WorkItem context, Task context, and committed Artifact metadata and
content within the bound WorkItem. `task_executor` additionally has `task_artifact_write` for its
current Task Claim and `blackboard_planning_write` for creating Tasks, Relations, and Child Tasks in
its bound Blackboard WorkItem. `coordination_executor` is read-only and returns write intents to the
Daemon. Other operations are denied by default. HTTP and MCP enforce the same Profile/scope rules
server-side; tool filtering does not replace authorization.

Blackboard planning objects created by a Task Harness are shared facts that survive Task failure or
release. Core always checks resource ownership, mode, state, hierarchy, DAG, capacity, and idempotency.

Role applies to Identity Token discovery and Claim acquisition; `claim_task` rechecks executor and
`allowed_roles`. A successful Claim proves eligibility for that execution. An Executor principal
contains only the Claim Actor, Profile, and scope: no Role, current Identity lookup, or repeated
`allowed_roles` check.

An Executor Token follows its Claim's actual Active lifetime, with no independent TTL, Role snapshot,
or Identity version binding. `lease_until` only makes the Claim eligible for reaping; actual Claim
termination invalidates the Token. Agent Identity Token revocation or rotation does not cascade to
Executor Tokens: even if the Harness cannot be stopped, its capability lasts until the Claim ends.

Executor Token mutations revalidate Claim Active state inside the business transaction. If Claim
termination commits first, the mutation fails; if the mutation commits first, its result remains valid.

## 4. Adapters and outcomes

An Adapter is the Daemon's in-process runtime interface for one Harness type:

```go
type Adapter interface {
    Probe(context.Context) error
    Start(context.Context, StartRequest) (RunRef, error)
    Observe(context.Context, RunRef) (RunObservation, error)
    Stop(context.Context, RunRef, StopReason) error
}
```

- `Probe` checks basic Harness/Provider readiness without creating a run; success does not guarantee
  that a later Start succeeds.
- `Start` injects the Executor Token, MCP endpoint, and execution context, returning a valid RunRef on
  success. On failure it returns an empty RunRef and error only after confirming termination of any
  process it may have started and cleaning up its resources. It cannot return failure with a possible
  run left behind, so the Daemon can safely retry within its budget.
- `Observe` returns a RunObservation snapshot containing RunState; a call error means observation is
  temporarily unavailable, not that the run ended.
- `Stop` is an idempotent, best-effort termination request, not confirmation that the Harness stopped.

An optional RunForgetter drops terminal in-memory Adapter metadata after finalization or before
replacing a confirmed-ended run. If cleanup is still pending, it remembers the request and reclaims
metadata when the run completes. It does not terminate live processes or remove workspace files.

RunRef is a serializable reference without secrets, usable for observation and reconciliation while
the Daemon lives. Credential-specific MCP tools and initialization instructions guide context reads,
authorized operations, and direct write operation IDs. A short launch prompt and output schema define
the managed execution result; no separate Managed Skill is required. The Adapter translates Harness-specific output into a HarnessOutcome, either a
TaskOutcome or a CoordinationDecision.

### TaskOutcome

All Task outcomes apply to both Workflow and Blackboard except `decomposed`, which is Blackboard-only.

| Outcome | Content | Daemon operation |
| --- | --- | --- |
| `completed` | Result, Artifact IDs, Review request, optional Workflow transition | `submit_task` |
| `decomposed` | Child Task specs | `decompose_blackboard_task` |
| `retryable_failure` | Business reason, optional retry prompt | `fail_task(action=reopen)` |
| `terminal_failure` | Business reason | `fail_task(action=fail_work_item)` |
| `abandoned` | Optional release reason | `release_claim` |

`completed.transition` is allowed only for Workflow Tasks. Long logs, patches, and deliverable files
belong in Artifacts; completion refers to existing Artifact IDs.

### CoordinationDecision

| Outcome | Allowed candidate kinds | Daemon operation |
| --- | --- | --- |
| `create_task` | All three kinds | `create_blackboard_task` |
| `submit_completion` | `empty_blackboard`, `blackboard_completion` | `submit_blackboard_completion` |
| `accept_completion` | `work_item_acceptance` | `accept_blackboard_completion` |
| `abandoned` | All three kinds | `release_coordination_claim` |

`create_task` creates the initial Task for an empty Blackboard, a follow-up Task for completion, or a
follow-up Task that reopens the WorkItem for acceptance. `submit_completion` carries the completion
result; `accept_completion` and `abandoned` have no additional fields.

The Dispatch's Claim binding distinguishes the two `abandoned` variants; the Harness does not choose
a Claim type. An outcome incompatible with mode, candidate kind, or schema is an output protocol error.
The Daemon rejects it before calling Core without inferring an alternative intent. Core retains final
domain validation. Section 6 defines suppression after abandonment.

## 5. Run state and terminal convergence

### RunState and DispatchState

RunState describes the Harness: `starting`, `running`, `outcome_ready`, `runtime_failed`,
`stopped`, and `lost`. `outcome_ready` requires an ended Harness with a valid outcome; an ended run
with invalid output is `runtime_failed`. `stopped` confirms an intentional stop; `lost` means the
run cannot be observed or controlled.

DispatchState describes convergence of the complete execution responsibility:

```text
prepared → claimed → starting → running → finalizing → finished
                       ↑          │
                       └── retry ─┘

starting / running / finalizing → stopping
stopping + confirmed Run end + ended Claim → finished
stopping + RunState.lost + ended Claim     → lost
```

No RunState directly releases an execution slot. Only after the Run ends or becomes lost and Core
confirms Claim termination does the Dispatch enter finished/lost and release its slot.
`outcome_ready` only moves it into finalizing. An independent heartbeat guard starts immediately
after Claim acquisition and covers startup, observation, retries, and finalizing.

### Stopping and runtime failures

Each Dispatch has one lifecycle driver at a time, alongside independent heartbeat,
concurrent stop requests, and read-only snapshots. The Scheduler hands its initial acquisition
to Run; internal step and heartbeat methods are not separate public drivers. Snapshots derive
candidate, Core-confirmed Claim, and frozen-intent information rather than duplicating those facts.

Network timeouts, Core unavailability, 5xx, and lost heartbeat responses mean unknown state. The Daemon
retries within the lease safety window, requests Stop at the safe stopping point if renewal still fails,
and reconciles when Core returns. An ended Claim, fencing, owner mismatch, WorkItem cancellation, or
invalid Identity Token stops heartbeat and further terminal mutations immediately, requests Stop,
and moves the Dispatch into stopping.

Observe must confirm termination; a stop timeout or loss of control is treated as RunState.lost.
The Daemon stops heartbeat, attempts Stop, and requests release of the bound Claim when Core is
reachable and its credential remains valid. Until Core confirms Claim termination, an unavailable Core
or uncertain release leaves the Dispatch stopping, holding its slot and isolating the runtime.
No replacement Harness may start under the same Claim while termination of the old run is unconfirmed.

Unavailable models, Provider authentication/quota problems, context overflow, process crashes, invalid
output, and Adapter faults are not directly translated into Submissions, Task Failures, WorkItem
Failures, or Coordination Decisions. Once the old run is confirmed ended, the Daemon may retry within
the same Claim; a runtime_failed retry returns the Dispatch to starting. Exhausted retries or an
intentional stop release Task Claims through `release_claim`, returning the Task to pending, or
Coordination Claims through `release_coordination_claim`, making still-eligible lifecycle candidates
discoverable again. Both paths obey the same terminal-state and slot-release conditions.

### Terminal reconciliation

The Daemon fixes one terminal intent before writing. After a lost response it examines Task/WorkItem
context, Claim end reasons, and related Submission, Failure, decomposition, or Coordination history:

- Claim still Active: retry the same intent.
- Expected end reason and matching history: the original operation committed.
- Another end reason: accept Core state and stop writing.
- Context unavailable: remain pending reconciliation without assuming success.

Resource creation and decomposition retain the same operation_id on replay. Heartbeat continues during
finalizing until Core confirms Claim termination or authoritatively reports credential/ownership loss.

## 6. Scheduling and repeated-Claim suppression

Task and Coordination Dispatches share an instance's execution slots; Claims require a free slot.
`find_work` is a candidate snapshot with an independent limit per kind, not a priority queue.
The Daemon cycles through:

```text
work_item_acceptance → blackboard_completion → task → empty_blackboard → …
```

Each successful Claim advances an in-memory cursor; empty kinds are skipped and within-kind order
follows Core. Free slots use the same cycle, and Claim conflicts trigger rediscovery. This is a local
selection policy, not Task priority or cross-Daemon WorkItem fairness. Systemic Harness/Provider failures
pause new Claims. Probe runs before the first Claim and before resuming after a health pause or Candidate
cooldown; probe failures use bounded backoff and jitter.
Systemic failures from Start, Observe, or Stop pause new admissions. Definitely
unsent acquisitions pass the health gate again; uncertain acquisitions continue
reconciliation with the original operation ID despite a health pause.

Suppression binds a Candidate's identity and current generation, surviving Claim release:

- Exhausted infrastructure retries enter cooldown. Reclaiming requires cooldown expiry, a successful
  Probe, and remaining cross-Claim budget. Exhausting that budget quarantines the generation.
- `abandoned` means this Daemon declines the generation. Release directly quarantines it; abandonment
  is not a successful Dispatch and does not clear existing failure records.

Material changes to business state, execution context, plans, or results create a new Candidate
generation and invalidate old cooldown, budget, and quarantine. Claim creation, heartbeat, release,
and reaping alone do not. Health recovery only lifts the global pause, preserving cooldown,
budgets, and quarantine. Quarantine ends on a new business generation or a new Scheduler instance,
normally a Daemon restart; some recovered candidates therefore require operator intervention.
Within a generation, a non-abandoned business outcome committed to Core clears its suppression.

These records live only in Daemon memory, not Core, and do not prevent another Daemon from claiming.
Claim churn across multiple Daemons or repeated restarts may still trigger Core's Claim-history safety
ceiling and fail the WorkItem.

## 7. Deployment and crash boundaries

Each Scheduler accepts one Run, rejecting concurrent or repeated calls. Cancellation means shutdown,
not a resumable pause. Unknown Claims continue reconciling during operation and the bounded shutdown
window; unresolved Dispatches remain nonterminal at exit. The standalone Dispatch engine retains
its own state and reconciliation capability independently of this Scheduler lifecycle.

The MVP keeps Dispatch state in memory and does not recover old Dispatches. Graceful shutdown attempts
to stop known Harnesses and release Claims. After a Daemon crash its in-process Adapter cannot continue
cleanup, and lost in-memory RunRefs cannot reconnect. The MVP does not promise to terminate surviving
Harnesses, clean workspaces, recover finalizing intent, or reverse external side effects.

After heartbeat stops, Core reaping eventually ends Claims and invalidates Executor Tokens, protecting
only Kairos state. Every Dispatch uses an isolated workspace; old runtimes cannot be reused before
confirmed cleanup, and unknown old workspaces are not reused automatically. systemd cgroups, containers,
or Kubernetes Pods may provide extra process cleanup as a deployment guarantee.

Cross-process recovery requires a durable Dispatch journal, externally stable RunRefs, a runtime
registry or supervisor, and Adapter reattach/observe/stop capabilities. Neither a journal nor serializable
RunRefs alone provide recovery.

Logs and metrics cover Dispatches, Claims, heartbeat safety, Adapter state, duration, outcomes, and
reconciliation, excluding Tokens, Artifact content, full model output, and other private execution data.

## 8. Implementation layers

The local Agent Daemon covers Task and all three Coordination Dispatch kinds, Claim-attached Executor
Credentials, independent heartbeat, Probe/cooldown, in-memory scheduling, and a Codex Adapter, validated
with fake-Adapter state-machine tests and a local E2E example.

A later durable operations layer adds journaling, external runtime supervision, and restart
reconciliation, with active-Claim queries and batch Blackboard planning as needed. Remote Adapters add
remote secret delivery, runtime isolation, cancellation propagation, network partition handling,
RunRef reconciliation, and version negotiation.

## References

- [Temporal Workers](https://docs.temporal.io/workers) and
  [Temporal Activities](https://docs.temporal.io/activities)
- [Prefect Workers](https://docs.prefect.io/v3/concepts/workers)
- [Kubernetes Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
- [GitHub Actions self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/about-self-hosted-runners)
