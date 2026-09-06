# Agent Daemon example

[简体中文](README.zh-CN.md)

This example starts an **isolated authenticated Core with SQLite**, creates Workflow
and Blackboard WorkItems, then starts a single-slot Codex Daemon. The native MCP
[quickstart](https://github.com/ScienJus/kairos/tree/main/examples/quickstart) remains available separately.

Requirements: Linux/macOS, Go/Node for source builds, curl, jq, openssl, Perl (core POSIX module), Codex CLI
0.146.x, and a dedicated authenticated Codex home. Release archives contain both
`kairos-server` and `kairos-daemon`; Codex itself and model authentication are not bundled.

```sh
# Authenticate the dedicated directory first using the normal Codex login workflow.
KAIROS_DAEMON_CODEX_HOME=/absolute/path/codex-auth \
KAIROS_DAEMON_MODEL=your-model make daemon-example
```

This explicitly starts real model work and consumes usage. From an extracted release,
use the same variables with `sh examples/daemon/run.sh` instead of Make.
The Workflow uploads a managed text Artifact. The Blackboard plans one Task, executes
it, submits completion, and accepts the result. Every Harness uses only its Executor
Token; the Daemon owns Claims and finalization. No repository access is needed.
The example uses one attempt and one unsuccessful Dispatch per candidate generation.

The default address is `127.0.0.1:18082` (`KAIROS_DAEMON_EXAMPLE_ADDR` overrides it).
An inherited PostgreSQL DSN is cleared. Credentials are neither printed nor passed
in command arguments. Core data and workspaces stay in a private temporary directory
whose path is printed; Ctrl-C stops Daemon before Core. Core runs in a separate
process group, so terminal SIGINT/SIGTERM cannot bypass this ordering; repeated
signals during cleanup do not interrupt the waits. Inspect then remove that
directory manually. Do not point another service at its database while it runs.

## No-model verification

```sh
make daemon-e2e
```

This builds and runs real Core/Daemon binaries with a scripted fake Codex process,
not a model. It tests both example fixtures plus cancellation, failed-run suppression,
and crash/lease expiry. It also signals the example's entire foreground process
group, checking Core availability during Daemon cleanup and released Claims afterward.
Ordinary `make go-test` skips this opt-in binary suite; CI
runs it explicitly. `KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh` only
validates example setup and exits without starting a Harness.

Operational boundaries and model/version validation are in the
[Adapter guide](../../internal/daemon/codexadapter/README.md) and
[scheduler guide](../../internal/daemon/README.md). Probe checks local installation
and login, not model quota; health recovery does not erase quarantine. Candidate
generation changes or restart clear quarantine. Agent Identity Token revocation
does not immediately revoke active Executor capabilities; their Claim lifecycle
controls validity. Stop is best-effort; a Daemon crash leaves Core to reap Claims,
not external processes. Use OS/container isolation for untrusted workloads.
