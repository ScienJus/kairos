# Agent Daemon scheduler

This package implements the single-dispatch engine and continuous scheduler.
For a standalone authenticated Core/Daemon deployment, see the
[Workflow/Blackboard example](../../examples/daemon/README.md). `make daemon-e2e`
runs the opt-in binary suite without model calls. Both release binaries expose `--version`.
Stage 4 adds a [local Codex Adapter](codexadapter/README.md), using credential-specific
MCP instructions and a structured outcome. Model execution is opt-in; the default and fake diagnostic modes
do not invoke a model provider. Real-provider smoke validation remains separate.

## Diagnostic command

```sh
make daemon-build
# Set KAIROS_DAEMON_TOKEN securely in the environment first.
./bin/kairos-daemon --core-url http://localhost:8080 --adapter fake-abandon --slots 1
```

Use a disposable Core/workload for this smoke test: `fake-abandon` really claims
work, returns `abandoned`, releases the Claim, and quarantines that generation.
The default `unavailable` Adapter always fails Probe and never claims work.
The Identity Token is read only from `KAIROS_DAEMON_TOKEN`; it is not a CLI flag
or passed to the Harness. `--help` lists all configuration.

For model-backed work, use `--adapter codex` with an explicit `--codex-home` and
`--codex-model`; see the Adapter guide for supported versions, network policy,
authentication setup, and process cleanup boundaries.

Defaults: 1 slot, discovery every 5s with 50 candidates per kind, idle Probe every
30s, 5m lease, 1s Harness polling, 10s request timeout, 30s stop window, 2 Harness
attempts per Claim, 1m candidate cooldown, 3 unsuccessful Dispatches per generation,
and a 30s shutdown window. MCP defaults to Core URL + `/mcp`. `--tags` accepts
comma-separated canonical tags. `--workspace-root` defaults to `.kairos-daemon`.
Each Dispatch receives a unique private directory before acquisition. Empty
directories are removed when acquisition ends without a Claim or Harness,
including deferred rejection and cancellation; claimed-work
directories are retained, never automatically reused or recursively removed. Inspect them
before manual cleanup, especially after an unresolved or lost run.

## Continuous scheduling

`NewScheduler` accepts a `DiscoveryCore` and runs with `Run(ctx)`. Library callers
using the Codex Adapter must set both Core and MCP endpoints; only the CLI derives
MCP from Core. The Scheduler creates absolute per-Dispatch workspaces:

```go
options := daemon.DefaultSchedulerOptions()
options.Dispatch.CoreURL = "http://localhost:8080"
options.Dispatch.MCPURL = "http://localhost:8080/mcp"
scheduler, err := daemon.NewScheduler(core, adapter, options)
// Check err before running scheduler.Run(ctx).
```

Candidate kinds rotate acceptance → completion → task →
empty Blackboard. Only successful Claims advance the cursor. Conflicts trigger
fresh discovery. Slots include acquisition uncertainty and stopping/reconciliation;
neither a lost Harness nor an unreachable Core frees a slot on its own.

Discovery gives each HTTP request its own `RequestTimeout`, rather than sharing
one deadline across the batch. A later context failure preserves resolved
candidates; cancellation or an authorization failure prevents using that partial
batch. Custom `DiscoveryCore` implementations receive the per-request timeout
explicitly and must honor cancellation.

Probe precedes acquisition, including cooldown recovery. Unhealthy Probe pauses
admissions with exponential backoff (up to 1m plus up to 25% jitter). Adapters mark
system-wide failures with `SystemError` or `RunObservation.SystemFailure`; ordinary
errors affect only the current candidate. System failures pause new admissions
until a successful Probe; existing Dispatches retain their lifecycle guards.
This includes `SystemError` returned by Start, Observe, or Stop. Probe must not
create a Claim or a Harness run. An acquisition known never to have been sent
passes the health gate again on every retry, even when all slots are reserved.
Probe and Core acquisition have separate timeout budgets. An earlier unknown
acquisition bypasses this gate for idempotent reconciliation; a later unsent
retry does not erase that uncertainty or create a new operation ID.
Probes remain serialized, but waiting for the Probe gate honors cancellation and
deadlines. A cancelled waiter does not call the Adapter or change health state;
it cannot hold up shutdown behind other queued probes.
Caller cancellation or deadline expiry during an active Probe also leaves health
and suppression unchanged. A Probe's own `RequestTimeout` expiry or an Adapter
failure still triggers backoff and the normal unhealthy-to-healthy recovery rule.

Unsuccessful claimed Dispatches consume the cross-Claim budget and enter cooldown;
exhaustion quarantines the candidate. `abandoned` immediately quarantines, even
though release succeeded. A non-abandoned applied business outcome clears the
record. Health recovery only lifts the global pause; it never clears cooldown,
failure budgets, or quarantine. A changed business generation or a new Scheduler
instance (normally a process restart) clears the applicable suppression records.
Configuration changes require restart. Recovery of an exhausted candidate may
therefore require an operator restart even after the Harness becomes healthy.

Generation hashes cover Definition and shared WorkItem business context, Task
history (including Retry Prompts and Reviews), relations, and submitted Artifacts.
They omit Claim histories, active Claim pointers, versions and update timestamps,
and normalize working Tasks to pending. Claim/heartbeat/release/reaper churn thus
does not reset a budget. Core discovery is a bounded per-kind snapshot: suppressed
candidates may hide work beyond the limit. There is no global fairness guarantee,
cross-Daemon suppression, or bypass of Core's Claim-history protection.

Cancellation stops admissions and requests termination. During the shutdown
window, Dispatches continue reconciling. After that window their contexts are
cancelled; the existing Dispatch engine may use one additional request timeout
for its bounded cleanup pass. Unresolved Dispatches remain in `Stats().Active`
for diagnosis, without being marked ended. A Scheduler accepts exactly one `Run`
call; concurrent calls and calls after it returns fail with `ErrSchedulerAlreadyRun`,
including when the initial context was already cancelled. Cancellation is shutdown,
not a resumable pause. The standalone Dispatch engine still supports retaining and
reconciling its own state. Process exit loses in-memory state; Core reaps unrenewed
Claims, without promising external process cleanup.

JSON logs include Claim acquisition, Probe results, terminal metadata, duration,
and periodic `SchedulerStats` (active, paused, Claims, heartbeat attempts/failures,
Probe counts/failures, terminal retry totals, suppressed selections, finished/lost).
They exclude credentials, raw Adapter errors, model output, and Artifact contents.

## Entry points

- `NewHTTPClient` takes a Core base URL and the ordinary Agent Identity Token.
  Redirects are rejected; transport errors do not include request bodies or tokens.
- `NewDispatch` validates a candidate and timing options, then generates one
  Claim operation ID, one finalization operation ID, and an Executor Token.
- `Run(ctx)` is the public lifecycle driver, with an independent heartbeat guard.
  Concurrent drivers fail immediately with `ErrDispatchAlreadyRunning`; sequential
  calls can reconcile the same retained Dispatch after a previous Run returns.
  Lifecycle `step` and `heartbeat` are internal, not independent public drivers.
- The Scheduler performs one internal `claimOnce` acquisition before handing the
  Dispatch to Run. Both entry paths share the same exclusive-driver guard.
- `Snapshot` contains only execution metadata. `RequestStop` interrupts the
  current operation and requests cleanup; it does not itself prove termination.

Snapshot is derived on read: candidate data comes from the immutable candidate,
Claim ID/end information from the last Core-confirmed Claim, and outcome kind from
the frozen intent. Execution state and the confirmed `OutcomeApplied` result are
stored separately. Local lease expiry never establishes Claim termination.
Only the shared-state mutex and heartbeat serialization mutex remain: startup
confirmation and the background heartbeat guard can still overlap. Stop requests
and Snapshot reads remain safe concurrently with the single lifecycle driver.
Adapters may implement `RunForgetter` to drop terminal in-memory metadata after
finalization or before replacing a confirmed-ended run. If Adapter cleanup is
still pending after a lost Dispatch finishes, it remembers the request and reclaims
the record once the run completes, without a second call. This hook does not stop
live runs, remove workspaces, or change the four lifecycle operations.

```go
options := daemon.DefaultOptions()
options.CoreURL = "http://localhost:8080"
options.MCPURL = "http://localhost:8080/mcp"
options.Workspace = "/absolute/path/to/existing/private/dispatch-workspace"
core, err := daemon.NewHTTPClient(options.CoreURL, daemon.NewSecret(agentToken), httpClient)
// Check err, then provide an Adapter and a Candidate selected by the caller.
dispatch, err := daemon.NewDispatch(core, adapter, candidate, options)
// Check err before running.
snapshot, err := dispatch.Run(ctx)
```

`Run` cancellation performs one bounded cleanup pass and returns. If
`snapshot.Terminal()` is false, retain the same Dispatch and its occupied slot;
resume it with `Run` and a fresh context when the Core is reachable. Never replace
an unresolved Dispatch with a new Claim. All state is process-local: restarting
the process loses it, and the Core reaper eventually ends unrenewed Claims.

## Execution contract

Claim retries reuse the same operation ID and Executor Token. Before starting a
Harness, a fresh heartbeat confirms the returned Claim: an idempotent Claim
response alone may describe an old execution. Lease safety uses elapsed local
time from the start of the last acknowledged request, not server wall-clock time.
The heartbeat interval is one fifth of the acknowledged Lease, and the safety
margin is one tenth. Request deadlines are capped at the local safety deadline.
The guard wakes at the next renewal or safety deadline; `PollInterval` controls
only Harness observation. Control retries use a separate bounded interval.

The HTTP client distinguishes a failed context preflight (no Claim POST sent)
from an uncertain Claim mutation. A later preflight/authentication failure cannot
erase earlier uncertainty. A Core `409 conflict` from the stable idempotent Claim
POST resolves it: Core would replay an earlier successful operation before
checking candidate eligibility. The Dispatch can then finish without a Claim.
Gateway/transport failures remain uncertain and retain the same operation ID.

The heartbeat guard continues during Start, Observe, retries, and finalization.
The Adapter must honor contexts. Start errors guarantee no remaining run;
ambiguous or empty-success references are treated as lost and never retried.
Observe errors mean unknown state. Persistent observation failure requests Stop
after `StopTimeout` from the first failed response. During this recovery window,
retry waits and Observe requests are capped at its deadline, independently of the
normal `PollInterval`. A successful observation before the deadline restores
normal polling; a late response cannot extend the window. Failure to confirm termination within the stop window marks
the run lost. A stop signal wakes the execution loop immediately. Even when the
confirmation window has already elapsed, the engine attempts Stop before
releasing responsibility. A successful Stop call still requires Observe confirmation.

An `outcome_ready` run has exited. The engine validates and deep-copies its typed
outcome before choosing the Core operation. `DecodeOutcome` provides strict JSON
decoding for concrete Adapters. Invalid output and runtime failures may restart
only after confirmed exit, up to `MaxAttempts` including the initial attempt.
Exhaustion releases the appropriate Claim, without submitting a business Failure.
Payload checks include Role/Tag sets, human-executor constraints, transition
skip/review sets, and Core history-text byte limits. Task-spec Role/Tag values
must have no surrounding whitespace; root and decomposition-child specs reject
untrimmed values so exact-match discovery and Claim authorization remain usable.
Invalid payloads use this
Harness retry path; graph membership, capacity, and changing domain state remain
Core checks. The HTTP client encodes all empty request collections as `[]`,
including nested Task specs and transitions, without changing the frozen intent.

Finalization retries keep the same intent and operation ID. After any uncertain
response, the engine inspects the bound Claim and related business history before
another mutation. Ended Claims are never retried as new work. `OutcomeApplied`
means the original intent was acknowledged or confirmed by history; false is not
proof that no write happened (the Claim may have ended externally or evidence
may no longer suffice). `Outcome` preserves `abandoned` for the scheduler
to quarantine rather than immediately reclaim.
Completed-result reconciliation requires the exact effective Claim end reason:
Blackboard uses `request_review`; Workflow also applies the Task's ReviewPolicy.
In particular, `required` enters Review even when the Harness did not request it.
A missing or invalid Workflow policy does not establish a successful outcome.

Only an ended/lost run **and** a Core-confirmed ended Claim produce a terminal
Dispatch. A rejected acquisition, or stopping before acquisition begins, can end
without a Claim. Unknown Core state retains `stopping`, including when a revoked
Identity Token prevents reading history. This package does not bypass that
authorization boundary or infer Claim expiry from elapsed time.

`Probe` belongs to the Adapter contract and is called by the scheduler before
acquisition. The Executor Token is the only credential passed
to Start. The ordinary Identity Token remains inside the Core client. Secret
formatting and JSON encoding redact values; Adapters must explicitly reveal them
only to their protected credential-injection channel.

## Verification

```sh
go test ./internal/daemon -count=1
go test -race ./internal/daemon -count=3
make go-test
make go-vet
```

Tests use fake Adapters, controllable clocks, `testing/synctest`, and a real
authenticated HTTP server backed by isolated SQLite. A response-dropping transport
commits real SQL mutations before losing replies. Tests cover both modes and all
candidate kinds, outcome legality, Claim and finalization response loss, stable
idempotency, Artifact binding, Workflow transitions, runtime retries, cancellation,
reaping, best-effort stopping, lost runs, and continued heartbeat during slow work.
They do not launch a real Harness or call a model provider.
