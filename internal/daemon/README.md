# Agent Daemon scheduler

This package implements the Daemon's continuous Scheduler and single-candidate Dispatch engine. The [Agent Daemon whitepaper](../../docs/whitepapers/agent-daemon.md) owns the product model; this file records the maintainer-facing runtime contract. For deployment, use the [Daemon example](../../examples/daemon/README.md). The local model-backed implementation is documented in the [Codex Adapter guide](codexadapter/README.md).

## Run

Build and run a non-model diagnostic against a disposable Core:

```sh
make daemon-build
# Set KAIROS_DAEMON_TOKEN securely first.
./bin/kairos-daemon \
  --core-url http://localhost:8080 \
  --adapter fake-decline \
  --slots 1
```

`fake-decline` claims real work, returns `candidate_declined`, releases the Claim, and quarantines that candidate generation. The default `unavailable` Adapter fails Probe without claiming work. Use `--adapter codex` only for explicit model-backed execution.

The ordinary Agent Identity Token is read only from `KAIROS_DAEMON_TOKEN`. It stays inside the Core client and is never passed to the Harness. Each claimed Dispatch instead receives a scoped Executor Token. Both release binaries support `--version`; `--help` is the authoritative configuration list.

Important defaults:

| Setting | Default |
| --- | --- |
| Slots | 1 |
| Discovery | every 5s, 50 candidates per kind |
| Idle Probe | 30s |
| Claim lease | 5m |
| Harness observation | 1s |
| HTTP request timeout | 10s |
| Stop / shutdown window | 30s |
| Harness attempts per Claim | 2 |
| Candidate cooldown | 1m |
| Unsuccessful Dispatch budget | 3 per generation |

`--workspace-root` defaults to `.kairos-daemon`. Every Dispatch receives a unique private directory. Empty pre-Claim directories are removed; directories associated with a Claim or Harness are retained for diagnosis and never automatically reused or recursively removed.

## Scheduler contract

`NewScheduler` accepts a `DiscoveryCore`; `Run(ctx)` starts one continuous scheduling lifetime. A Scheduler accepts exactly one `Run` call. Cancellation initiates shutdown and is not a resumable pause.

Candidate kinds rotate in this order: acceptance, completion, task, empty Blackboard. Only successful Claims advance the cursor. Conflicts trigger discovery again. A slot remains occupied while acquisition is uncertain or a Dispatch is unresolved.

Probe runs before acquisition and after health backoff. A system-wide Adapter failure pauses new admissions until a later Probe succeeds; existing Dispatches continue their lifecycle guards. Probe must not create a Claim or Harness run. An uncertain acquisition is reconciled with its existing operation ID rather than gated as new work.

Candidate suppression is process-local:

- an unsuccessful claimed Dispatch consumes the generation's budget and enters cooldown;
- `candidate_declined` quarantines the generation immediately;
- a successful business outcome clears its record;
- a changed business generation or Scheduler restart clears applicable suppression;
- health recovery does not clear cooldown, budgets, or quarantine.

Generation hashes include Definition, shared WorkItem context, Task history and relations, and submitted Artifacts. They exclude Claim churn, timestamps, versions, and the transient `working` state. Discovery is bounded, so suppression can hide candidates beyond the current result limit; no global or cross-Daemon fairness is promised.

During shutdown the Scheduler stops admission and asks active Dispatches to terminate. Unresolved Dispatches remain visible in `Stats().Active`; process exit loses their in-memory state, after which Core eventually reaps unrenewed Claims.

Library callers must set both endpoints explicitly:

```go
options := daemon.DefaultSchedulerOptions()
options.Dispatch.CoreURL = "http://localhost:8080"
options.Dispatch.MCPURL = "http://localhost:8080/mcp"
scheduler, err := daemon.NewScheduler(core, adapter, options)
// Check err before scheduler.Run(ctx).
```

## Dispatch contract

`NewDispatch` binds one immutable candidate to one Claim operation ID, one finalization operation ID, one Executor Token, and one private workspace. `Run(ctx)` is the only public lifecycle driver. Concurrent calls fail; a later sequential call may reconcile the same unresolved Dispatch.

The lifecycle is:

```text
acquire Claim → confirm lease → start Harness → observe → apply outcome → end Claim
```

The heartbeat guard remains active during Start, Observe, retries, and finalization. Lease safety uses elapsed local time from the last acknowledged request; local expiry alone never proves that Core ended the Claim.

Mutation retries reuse the same operation ID. If a response is uncertain, the Dispatch reads the bound Claim and business history before retrying. A failed request preflight is distinguished from a request that may have reached Core. Never replace an unresolved Dispatch with a new Claim.

Adapter behavior:

- Start errors guarantee that no run remains.
- Observe errors mean the run state is unknown.
- Stop success still requires later Observe confirmation.
- invalid output or runtime failure may retry only after confirmed process exit, up to `MaxAttempts`;
- an `outcome_ready` run has exited and its typed outcome is frozen before Core mutation.

The engine validates mode-specific outcomes, Role and Tag sets, human-executor constraints, transition data, and history-text byte limits. Graph membership, capacity, and current domain state remain Core checks. Empty request collections are encoded as `[]`.

A Dispatch is terminal only when its run is ended or lost and Core confirms the Claim ended. `OutcomeApplied` means the frozen intent was acknowledged or recovered from history; `false` does not prove no write occurred. Unknown Core state remains `stopping`.

Minimal direct use:

```go
options := daemon.DefaultOptions()
options.CoreURL = "http://localhost:8080"
options.MCPURL = "http://localhost:8080/mcp"
options.Workspace = "/absolute/private/dispatch-workspace"
core, err := daemon.NewHTTPClient(options.CoreURL, daemon.NewSecret(agentToken), httpClient)
dispatch, err := daemon.NewDispatch(core, adapter, candidate, options)
snapshot, err := dispatch.Run(ctx)
```

Check every returned error. If `snapshot.Terminal()` is false, retain the same Dispatch and occupied slot, then call `Run` again with a fresh context when recovery is possible.

## Public surfaces

- `NewHTTPClient`: authenticated Core client; rejects redirects and redacts secrets.
- `NewDispatch`: validates options and creates stable execution identifiers.
- `Run`: drives and reconciles one Dispatch lifecycle.
- `RequestStop`: requests cleanup but does not prove termination.
- `Snapshot`: returns execution metadata derived from immutable candidate data and Core-confirmed state.
- `Stats`: exposes bounded Scheduler counters and active Dispatches.
- `RunForgetter`: optional Adapter hook for dropping terminal in-memory run metadata; it never stops a run or deletes workspaces.

## Observation

Each CLI process registers a Daemon instance and reports health, active Dispatches, and typed lifecycle events. Reports are independent of Claim coordination. A report becomes `stale` after 45 seconds; this neither changes a Claim nor proves process exit.

Reporter failures use bounded retry and redacted logs. Shutdown reports `stopping`, then attempts one bounded `stopped` report. Batch or body limits can leave events unsent. Exact HTTP contracts and retention rules live in the [API Reference](../../docs/api-reference.md).

JSON logs contain Claim acquisition, Probe results, terminal metadata, duration, and aggregate Scheduler stats. They exclude credentials, raw Adapter errors, model output, Artifact contents, and response bodies.

## Verification

```sh
go test ./internal/daemon -count=1
go test -race ./internal/daemon -count=3
make daemon-e2e
make go-test
make go-vet
```

Automated coverage uses fake Adapters, controllable clocks, real authenticated HTTP with isolated SQLite, and response-dropping transports. It covers both coordination modes, outcome validation, response loss, stable idempotency, retries, cancellation, reaping, lost runs, and heartbeats during slow work. Real-provider smoke tests are separate and opt-in.
