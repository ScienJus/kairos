# Local Codex Adapter

Stage 4 implements a local, process-backed Adapter for **Codex CLI 0.146.0 or newer** on
Linux and macOS. Preflight checks a stable minimum version and parses the required
execution options with `exec --help`, without launching a model. Newer stable
versions are not rejected solely by version number; unsupported options still
fail preflight. Passing this parser check does not establish full runtime compatibility. Real model execution is
opt-in; automated tests use a fake CLI process with real Core HTTP/MCP and SQLite.

## Run

Prepare a dedicated Codex authentication directory and authenticate it with the
normal Codex login workflow. Keep its credentials separate from Kairos's Identity
Token and do not reuse the Daemon workspace as this directory.

```sh
install -d -m 700 /absolute/path/codex-auth
CODEX_HOME=/absolute/path/codex-auth codex login
make daemon-build
# Set KAIROS_DAEMON_TOKEN securely in the environment. Choose your model explicitly.
./bin/kairos-daemon --adapter codex \
  --core-url http://localhost:8080 \
  --codex-home /absolute/path/codex-auth \
  --codex-model "$MODEL" \
  --workspace-root /absolute/path/daemon-workspaces
```

`--codex-executable` selects the binary. `--codex-home` and `--codex-model` are
required for this Adapter. The default command remains `--adapter unavailable`;
`fake-abandon` remains available for diagnostics. Starting the real Adapter can
incur model usage and change the configured Kairos workload.

The Adapter uses saved Codex authentication, ignores user config and execpolicy
rules, disables inherited project instructions, and establishes a new project
root marker for each attempt. Arbitrary user profiles/custom providers/hooks are
not configured by this MVP. Machine-managed Codex policies still apply.

`Probe` runs `--version`, an `exec --help` parser check with the required flags,
and `login status`. It checks installation, CLI option compatibility and local
login state, not provider quota, model availability, network reachability, or task
fitness. Successful Probe is not a guarantee that execution will succeed.

## Execution and credentials

Each Start creates a fresh `run-*` directory under its Dispatch workspace and
writes a candidate-specific output schema. Credential-specific MCP tools and
initialization instructions describe Kairos operations; the short launch prompt
supplies execution identity, outcome conventions, and the Artifact download endpoint.
No separate Managed Skill is installed or maintained.
Files are private (0600), directories are 0700. The prompt identifies only the
bound candidate/Claim and Core HTTP endpoint; execution context comes from MCP.

Codex runs non-interactively with `workspace-write`, network access enabled, no
approval prompts, ephemeral session history, and a schema-constrained final file.
Network access supports repository work and managed Artifact downloads. This is
not an OS credential-isolation boundary: a same-user process can access that user's
readable files. Use a dedicated runtime user/container for untrusted workloads.

The child environment is allowlisted: PATH/HOME, temporary-directory/locale,
certificate/proxy settings, configured CODEX_HOME, and the freshly generated
KAIROS_EXECUTOR_TOKEN. Kairos's Identity Token and unrelated environment secrets
are not inherited. MCP reads the Executor Token by environment-variable name;
plaintext is never put in argv, prompts, generated config, or Daemon logs.
Raw CLI stdout/stderr are discarded, and output files are size-bounded and must
be regular files, not symlinks. Model-produced files may contain sensitive work
data; they remain in the private workspace for operator inspection.

## Outcomes and cleanup

The Codex-specific envelope has the mode's `task` or `coordination` member plus
`runtime_failure`. Exactly one is non-null. Business results retain the existing
Daemon outcome semantics. For infrastructure problems, the Harness returns
`runtime_failure: {"system": false, "reason": "Chrome exited before page load; browser verification is still required."}` (candidate-specific) or `system: true`
(Harness/Provider-wide); no business failure is fabricated. Missing, malformed,
oversized, or invalid results and normal nonzero exits become runtime failures. A generic
nonzero exit code is candidate-scoped; signal termination instead becomes `lost`.
The Adapter does not guess provider health
from free-form error messages. The generated schema requests a concise reason
(up to 4096 UTF-8 bytes) with the failed operation, observed error and recovery step.
It is retained only in the private outcome file, never in error strings or scheduler
logs; credentials and raw tool output must not be included. Older outcome files
without a reason remain readable. No Core API, persistence or lifecycle contract changes.

Browser availability is separate from CLI preflight: a system Chrome installation
may fail to launch inside the Codex sandbox. Do not disable the sandbox or label
unperformed browser checks as passing. Use a functioning isolated browser environment
to record evidence against the exact reviewed commit, then have the operator resume
the suppressed candidate. Raw CLI output remains discarded.

Start errors occur before a process is created. Once `exec.Start` succeeds, Start
returns a valid RunRef even if cancellation races with that success. Stop sends
one SIGINT to Codex so its shutdown path can terminate its own shell sessions,
including ordinary commands in separate process groups. Repeated Stop calls do
not interrupt that cleanup. After two seconds without confirmed exit, the Adapter
attempts SIGKILL of the root group and reports `lost` when Wait returns; it never
treats that fallback as confirmed shell cleanup. An externally signaled exit or
root-group members still present after auxiliary cleanup also produce `lost`. After
a normal CLI exit, the Adapter kills remaining root-group auxiliary processes and
waits up to two seconds for that group to disappear. Normal CLI exit and graceful
interruption rely on the supported CLI's own session cleanup, tested using the
real CLI with a local simulated Provider. No process-tree supervisor is added:
after forced/abnormal termination, detached processes may require operator cleanup.

Optional `RunForgetter` discards terminal in-memory process metadata after a
Dispatch finishes or before a confirmed-ended run is replaced. If the Dispatch
finishes before Adapter cleanup, the request is remembered: the live run remains
observable until completion, when its record is automatically reclaimed. Repeated
requests are safe; Forget never stops a process or deletes workspace files. This keeps long-running schedulers from
accumulating per-run output indefinitely. A Daemon crash still loses the registry:
Core reaps unrenewed Claims, but no cross-process reconnection is promised.

## Verification and sources

Tests exercise actual child processes, preflight failures, environment separation,
atomic Start, cancellation, exit/invalid-output handling, process-group stopping,
and unique attempt directories. Real HTTP/MCP + SQLite tests cover Blackboard
planning/execution/completion/acceptance, Workflow results/Review/transitions,
decomposition, managed uploads, and committed content reads by both profiles.
They do not call a real model or validate provider-specific behavior.

The opt-in installed-CLI regression uses a scripted loopback Provider without
real credentials. It covers a long-running ordinary `exec_command` across graceful
Stop (including repeated Stop), normal completion, and external SIGKILL. Confirmed
Stop/completion requires the command to be gone; SIGKILL must report `lost`:

```sh
KAIROS_TEST_CODEX_EXECUTABLE=/absolute/path/codex \
  go test ./internal/daemon/codexadapter -run '^TestRealCodexShellLifecycle$' -count=1 -v
```

- [Codex non-interactive execution](https://learn.chatgpt.com/docs/non-interactive-mode)
- [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)

When validating another CLI release, check its actual flags and configuration
even when it passes the minimum-version and option checks. A live-model smoke test is a separate,
explicit operator action after fake-process and Core integration tests pass.

Live-model smoke passed on 2026-09-06: macOS, CLI 0.146.0, ChatGPT authentication,
and `gpt-5.6-sol`, against an isolated temporary Core/SQLite instance. One Task
Dispatch uploaded a managed Artifact; one Blackboard completion Dispatch read its
committed bytes over HTTP and returned `submit_completion`. Both succeeded on their
first attempt with `OutcomeApplied` and `ClaimEnded`; exact content, final completed
WorkItem, and zero retained Adapter runs were verified. The test took about 275 seconds.
The temporary authentication copy was removed. This does not establish Linux,
other models, or all real-provider failure/recovery scenarios as validated.

Codex 0.146.0 can be rejected by providers for models requiring a newer client.
On 2026-09-09, local macOS Codex 0.153.4 completed a minimal authenticated
`gpt-6-astra` request with reasoning effort `high`. This verifies that model/client
combination only, not completion of a production Workflow. The real-CLI simulated
Provider regression also passed normal completion, graceful interruption and forced
termination on 0.153.4; the parser probe passed on both 0.146.0 and 0.153.4. A version/options
preflight error pauses new Claims; install a compatible CLI and allow Probe to
recover. Provider-side model rejection is not detected by this preflight.
