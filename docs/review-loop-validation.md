# Writer/Checker delivery-loop validation

[简体中文](review-loop-validation.zh-CN.md)

This record owns measured acceptance evidence for `examples/review-loop`. See the [example](../examples/review-loop/README.md) for operation and the [deployment/recovery runbook](guides/deployment-recovery.md) for recovery instructions. Scripted fixtures, model self-reports and operator simulation are kept distinct from a person's Review.

## Live-model delivery

Run on 2026-10-14, Darwin, Codex CLI 0.159.2, model `gpt-6.1-sol`. Core source baseline was `faf6967` (merged PR #70), with the new example and restore-proof script. An isolated Authenticated Core/SQLite served two separate Agent Identities with `writer`/`checker` roles and one slot each. Harnesses used a dedicated Codex login home and Claim-bound credentials.

The goal was a Chinese operational guide grounded in approved read-only repository references and supplied measured restore evidence. No production deployment, `.env` read, desktop-browser operation or credential packaging was part of the run.

1. Writer uploaded an initial `guide`. Checker consumed the exact input and recorded its ID in a JSON `review`, returning `revise`: the copy example followed symlinks by default despite the stated safety policy.
2. A new Draft instance revised that exact guide. Checker returned `ready` for the new guide ID, without selecting a globally newest same-name file.
3. Publication uploaded a fresh Claim-owned guide and entered Human Review. The acceptance operator rejected it because shutdown needed concrete Daemon-first termination/wait/cleanup before Core, rather than an unavailable console pause operation.
4. The same publication Task obtained a new Claim, received the rejection feedback and uploaded a revised Artifact. Operator approval completed the WorkItem.

Human Role decisions were performed by the isolated acceptance controller, not by the user personally in the console. The model roles had no Review-decision permission.

Final state: 5 Task instances, 6 Claims, 6 submitted Artifacts; 4 Writer and 2 Checker Harness runs; Checker verdicts `revise → ready`; publication had two Submissions and `rejected → approved` Reviews; no active Claims or business failures. Workflow `work_item.result` remained empty; delivery lives in Task Submission/Artifact history.

Original final guide SHA-256: `f31e03be36d623dc70a5a24b6f1728adf782edd08b28b3994fab198bcbf92eba`. The repository Chinese runbook retains its accepted operational sections, removes local run-specific IDs and adds navigation. The English companion is edited operational documentation, not a claim of byte identity with the Artifact.

## Deterministic binary regression

`make daemon-e2e` uses scripted Harnesses, real Core/Daemon HTTP/MCP, SQLite and scoped uploads, without model calls. Added cases deliberately cover:

- An exact `await_human` blocker, Human Continue and a replacement whose review still refers to the original input guide.
- Two same-name guide versions, exact review references and Claim-owned publication uploads.
- Human rejection, a new Claim on the same publication Task, retained feedback and both submissions.
- Forced checker Daemon termination, lease expiry without fabricated business failure, operator-confirmed cleanup of the fixture's known orphan, and takeover by a new Checker Identity before recovery/delivery.

The live-model run had no natural failure/crash; these paths were validated separately and are not described as spontaneous model behavior. Core's reaper ends Claims, not processes.

## Independently measured recovery

`verify-sqlite-restore.py` owns a temporary loopback Trusted fixture, submits a managed Artifact and completes its WorkItem, cleanly stops Core, copies the whole database directory and matching store, and restores a separate working copy with the same binary.

Observed: SQLite `quick_check=ok`, retained WorkItem state/result and Artifact ID, and identical downloaded bytes. Content SHA-256: `f4c971f17be63da606673faba8291f31e01796eafe765b2840365e3a2e9056b6`.

The guide's copy example was also independently executed against synthetic ordinary files/WAL/SHM/store, a symlink and a FIFO. Ordinary files passed; the symlink/FIFO were rejected before creating the backup. This was operator verification, not only a Checker assertion.

## Remaining scope

The run does not establish the same real CLI/model on Linux, other Providers, PostgreSQL, online backup, cross-version upgrades, production credential restoration, supervisor/container cleanup guarantees, external-URI/provider-login/workspace recovery, a person using the Review UI, or recovery-time targets on real data sizes. Further validation must record its own platform, CLI/model, dataset and actual operations.
