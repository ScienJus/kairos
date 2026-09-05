# Single-dispatch engine

This package implements Stage 2 of Agent Daemon integration. It runs an explicitly
selected Task or Blackboard coordination candidate using the existing Core HTTP
API and an injected Adapter. There is no daemon command, continuous discovery,
slot allocator, cross-Claim retry policy, or real model Adapter yet.

## Entry points

- `NewHTTPClient` takes a Core base URL and the ordinary Agent Identity Token.
  Redirects are rejected; transport errors do not include request bodies or tokens.
- `NewDispatch` validates a candidate and timing options, then generates one
  Claim operation ID, one finalization operation ID, and an Executor Token.
- `Run(ctx)` drives the Dispatch with an independent heartbeat guard. `Step` and
  `Heartbeat` are also available for deterministic tests or a future scheduler.
- `Snapshot` contains only execution metadata. `RequestStop` interrupts the
  current operation and requests cleanup; it does not itself prove termination.

```go
options := daemon.DefaultOptions()
options.MCPURL = "http://localhost:8080/mcp"
core, err := daemon.NewHTTPClient(coreURL, daemon.NewSecret(agentToken), httpClient)
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
may no longer suffice). `Outcome` preserves `abandoned` for the Stage 3 scheduler
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

`Probe` belongs to the Adapter contract; the continuous pre-Claim health policy
will be implemented in Stage 3. The Executor Token is the only credential passed
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
