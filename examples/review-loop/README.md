# Writer/Checker delivery loop

[简体中文](README.zh-CN.md)

Run a Writer and Checker as two Agent Identities against one isolated Authenticated Core:

```text
Draft → Check ── revise → Draft
          └── ready → Publish → Human Review
```

The Writer produces a managed `guide` Artifact. The Checker consumes the exact direct input, records `guide_artifact_id` in a managed JSON `review`, and chooses a legal revision or publication path. Publication uploads a new Claim-owned guide; it cannot submit an upstream Artifact ID. Human Review can reject a submission for correction or approve it to complete the WorkItem.

## Run with a real model

Requires Linux/macOS, `curl`, `jq`, `openssl`, Perl/POSIX, built Core/Daemon binaries, and a dedicated logged-in Codex CLI 0.146.0+ home. Choose the model explicitly. The command starts real model runs and consumes quota.

```sh
KAIROS_DAEMON_CODEX_HOME=/absolute/path/dedicated-codex-auth \
KAIROS_DAEMON_MODEL=your-model make review-loop-example
```

The launcher binds `127.0.0.1:18083` by default (`KAIROS_REVIEW_LOOP_ADDR` overrides it), clears an inherited PostgreSQL DSN, and creates private temporary data. It prints the WorkItem URL and the path to `operator.token`, not its value. Use that Human Token in the console's ordinary sign-in form. Do not share the private directory or include its credentials in artifacts.

`KAIROS_REVIEW_LOOP_WORK_ITEM=/absolute/path/work-item.json` selects a goal with approved repository reference paths and evidence. The default goal references public Kairos documentation and has no supplied restore proof; it does not imply any deployment or restore has been performed. Give read-only references explicitly and keep production changes outside this example.

The Daemons have separate `writer` and `checker` roles and one slot each. A failed infrastructure run is bounded by one Harness attempt/Dispatch per candidate generation. Inspect private outcomes/logs before restarting a role; CLI Probe does not establish provider quota or task compatibility. Ctrl-C stops both Daemons before Core; private data remains for inspection.

An installed release package can run `sh examples/review-loop/run.sh` with the same environment and binaries in the archive root.

## Human intervention and Review

- If Checker reports `human_intervention_required`, the Task is Failed while the Workflow may remain Open. Inspect the reason, use **Continue**, and provide a decision or missing evidence. The replacement retains the original input submission.
- A Checker `revise` verdict loops back to a fresh Draft instance. Both the review and the exact guide it refers to remain in history.
- A Human Review rejection reopens the same publication Task. Its next Claim sees feedback and submits a fresh Artifact; the rejected submission remains inspectable.
- Approving publication completes the Workflow. Inspect Task submissions/artifacts: Workflow `work_item.result` remains empty.

Use **Preview** beside a managed Artifact in the console to read the draft, checker feedback, or final guide as Markdown; switch to **Source** for exact text.

Workflow choice groups, recovery, credential scope, and staged/input/history semantics are owned by the [API reference](../../docs/api-reference.md). The [Adapter guide](../../internal/daemon/codexadapter/README.md) owns cleanup limits: after a Daemon crash Core reaps its lease but does not kill orphan Harnesses. Confirm termination/cleanup before replacing an uncertain execution.

## Reproducible verification without a model

```sh
make build
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
make daemon-e2e
```

Smoke only creates fixtures. The binary E2E uses scripted Harnesses and real HTTP/MCP, two Daemons, scoped uploads, and SQLite. It deliberately exercises:

1. An operator-required Checker failure followed by Human Continue.
2. Two same-name guide versions with reviews pointing to the exact input IDs.
3. Human rejection and resubmission of publication, with feedback preserved.
4. A checker Daemon crash, Claim expiry, fixture-owned orphan cleanup and a new execution.

The deterministic fixture forces these cases; a real model run decides whether revision is needed from its actual findings. No test pretends that Core's reaper stopped a process.

A live-model run on macOS exercised revision and publication resubmission. Human Review decisions were simulated by an acceptance controller; Continue and crash recovery were tested separately with scripted Harnesses. This validates the example workflow, not production deployment or backup recovery.
