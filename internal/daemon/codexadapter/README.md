# Local Codex Adapter

This package runs a Kairos Dispatch through a local Codex CLI process. It supports Codex CLI 0.146.0 or newer on macOS and Linux, subject to required option parsing. Model execution is always explicit and can consume provider quota or modify the selected Kairos workload.

The [Daemon package guide](../README.md) owns scheduling and Dispatch semantics. This file covers only Codex-specific setup, isolation, outcomes, and process cleanup.

## Run

Use a dedicated authentication directory. Do not reuse the Daemon workspace or Kairos Identity Token storage.

```sh
install -d -m 700 /absolute/path/codex-auth
CODEX_HOME=/absolute/path/codex-auth codex login

make daemon-build
# Set KAIROS_DAEMON_TOKEN securely and choose a model explicitly.
./bin/kairos-daemon \
  --adapter codex \
  --core-url http://localhost:8080 \
  --codex-home /absolute/path/codex-auth \
  --codex-model "$MODEL" \
  --workspace-root /absolute/path/daemon-workspaces
```

`--codex-home` and `--codex-model` are required; `--codex-executable` selects the binary. The Daemon still defaults to the non-running `unavailable` Adapter.

Probe checks:

- `codex --version` meets the minimum;
- `codex exec --help` exposes required flags;
- the selected `CODEX_HOME` has a valid local login.

Probe does not validate provider quota, model availability, network reachability, browser launch, or task fitness. Passing it is not a runtime compatibility guarantee.

## Execution boundary

Every Start creates a private `run-*` directory beneath the Dispatch workspace and writes a candidate-specific output schema. A short prompt supplies the execution identity and outcome convention; the scoped MCP server supplies work context and operations. No separate managed Skill is installed.

Codex runs non-interactively with:

- `workspace-write` sandboxing and network access;
- no approval prompts;
- ephemeral session history;
- a schema-constrained final output file;
- ignored user config, execpolicy rules, project instructions, profiles, providers, and hooks.

Machine-managed Codex policies still apply. This is not an operating-system isolation boundary: another process under the same user can read that user's accessible files. Run untrusted workloads in a dedicated user or container.

Directories use mode 0700 and files 0600. The child environment is allowlisted to required path, home, temporary, locale, certificate/proxy, `CODEX_HOME`, and `KAIROS_EXECUTOR_TOKEN` values. The ordinary Kairos Identity Token and unrelated environment secrets are not inherited.

The Executor Token is referenced by environment-variable name in generated MCP configuration. Its value is never placed in argv, prompts, generated config, or logs. Raw CLI stdout and stderr are discarded. Output must be a size-bounded regular file, not a symlink. Workspace files may still contain sensitive task data and remain available for operator inspection.

## Outcomes

The output envelope contains exactly one of the mode-specific `task` / `coordination` member or `runtime_failure`.

Business outcomes retain the Daemon's normal typed semantics. Infrastructure failures use:

```json
{"runtime_failure":{"system":false,"reason":"concise diagnostic"}}
```

`system: true` marks a Harness- or provider-wide problem; `false` affects only the candidate. The Adapter does not infer provider health from free-form messages. Missing, malformed, oversized, or invalid results and ordinary nonzero exits are runtime failures; signal termination is `lost`.

The optional diagnostic `reason` is limited to 4096 UTF-8 bytes and should contain the failed operation, observed error, and recovery step. It stays in the private outcome file and is not copied into Core business state, Scheduler logs, or returned errors. It must not contain credentials or raw tool output.

Browser capability is separate from CLI Probe. If Chrome cannot launch inside the sandbox, record that as missing validation; do not disable the sandbox or report an unperformed browser check as passing.

## Process cleanup

Start errors occur before process creation. Once `exec.Start` succeeds, Start returns a valid RunRef even if cancellation races with it.

Stop sends one SIGINT and lets Codex clean up its own command sessions. If exit is not confirmed within two seconds, the Adapter attempts SIGKILL on the root process group and reports `lost`; it does not claim that detached processes were removed. After normal exit, remaining root-group helpers are killed and checked for up to two seconds.

No process-tree supervisor or cross-process reconnection is provided. Forced or abnormal termination can require operator cleanup. A Daemon crash loses the in-memory run registry; Core only reaps the unrenewed Claim.

`RunForgetter` removes terminal in-memory metadata once cleanup finishes. It never stops live processes or deletes workspace files, and repeated calls are safe.

## Verification

Package tests cover child processes, Probe failures, environment separation, atomic Start, cancellation, result validation, process-group stopping, and unique attempt directories. Integration tests use real Core HTTP/MCP and SQLite with a fake CLI; they do not call a live model.

An opt-in installed-CLI regression uses a loopback provider to verify ordinary command completion, graceful Stop, repeated Stop, and forced termination:

```sh
KAIROS_TEST_CODEX_EXECUTABLE=/absolute/path/codex \
  go test ./internal/daemon/codexadapter \
  -run '^TestRealCodexShellLifecycle$' -count=1 -v
```

Live-model smoke testing is a separate operator action. Validate the exact CLI, model, platform, sandbox, and reviewed commit; one successful combination does not establish support for others.

References:

- [Codex non-interactive execution](https://learn.chatgpt.com/docs/non-interactive-mode)
- [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)
