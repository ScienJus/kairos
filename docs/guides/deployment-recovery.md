# Deploying and recovering Kairos

[简体中文](deployment-recovery.zh-CN.md)

This runbook targets source baseline `faf6967` (PR #70). The [review-loop validation record](../review-loop-validation.md) distinguishes live-model delivery, operator simulation and measured recovery checks. Its verified recovery scope is stopped-Core SQLite with the matching local managed Artifact store on Darwin. PostgreSQL, online backup, production credential restoration, systemd/Kubernetes deployment and cross-version upgrades remain unverified.

## Start with explicit paths and credentials

Use a reviewed Core binary, a dedicated runtime user and absolute data paths. Check version, checksums and provenance according to the [release guide](../releasing.md). Restore with the same binary version first: startup may apply embedded database migrations.

```sh
umask 077
install -d -m 700 /srv/kairos/data /srv/kairos/data/artifacts
export KAIROS_SQLITE_PATH=/srv/kairos/data/kairos.db
export KAIROS_ARTIFACT_DIR=/srv/kairos/data/artifacts
export KAIROS_LISTEN_ADDR=127.0.0.1:8080
export KAIROS_POSTGRES_DSN=''
export KAIROS_AUTH_MODE=authenticated
# Inject KAIROS_ADMIN_TOKEN through your protected secret-management facility.
/opt/kairos/kairos-server
```

Adjust these example paths and use the intended runtime owner. Do not blindly change permissions on an existing deployment. Authenticated mode requires a valid Admin Token; exact credential rules and proxy requirements belong to the [API reference](../api-reference.md). Trusted mode belongs only inside an explicit trust boundary. A nonempty PostgreSQL DSN takes precedence over SQLite.

Database files and existing WAL/SHM sidecars use mode 0600. The managed Artifact root must be dedicated, real, non-root and mode 0700; managed files use 0600. Correct startup errors without weakening these checks.

```sh
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/auth/config
```

Health and authentication configuration are public checks, not recovery proof. Use a protected authenticated client to check session identity, an expected WorkItem and submitted Artifact content. Keep authorization headers and real credentials out of commands, logs and reports.

## Stop writers before stopping Core

1. Arrange the maintenance window and stop admitting new work through your actual upstream controls. Kairos's console does not provide remote Daemon pause/stop operations. Identify all Daemons, other writers and their supervisors.
2. Send SIGTERM or SIGINT to all Daemons, then wait for each original process to exit. Keep Core available while Daemon cleanup releases Claims. Check process identity, supervisor state, exit status and private logs; prevent automatic restarts. A `stale` telemetry state is not proof of termination.
3. Confirm known Harness cleanup and outstanding external operations. The Codex Adapter tries graceful interruption before killing its root process group, but cannot guarantee removal of detached processes. A crashed Daemon loses its run registry; Core's lease reaper does not stop orphan processes. Operators must resolve uncertain runs before proceeding.
4. Only after writers have stopped, signal Core and confirm a clean exit. Core allows 10 seconds for HTTP shutdown. A timeout, unreachable endpoint or forced kill is not a clean shutdown.
5. Keep all database/store writers stopped through copying and verification. After an abnormal exit, preserve the original full file set and diagnose it; never delete WAL to make a backup appear clean.

Use your existing process manager or checked PIDs; signal commands alone do not prove completion. The [Chinese runbook](deployment-recovery.zh-CN.md#3-有序停机) includes concrete signal/wait examples and their limits.

## Back up one consistent recovery set

After confirmed shutdown, copy the complete database directory and its matching managed Artifact store into a new, private recovery set. Include the main SQLite file and every existing WAL/SHM/journal sidecar. Do not infer from one clean shutdown that copying the main file alone is generally sufficient. If the store is elsewhere, copy it in the same no-write window and retain the original path mapping.

The source and destination must be controlled paths. Reject symbolic links and special files before copying, explicitly avoid recursively following links, and compare the complete copied inventory, permissions, sizes and digests with the unchanged source. The [tested Python example](deployment-recovery.zh-CN.md#4-停服备份数据库与内容作为一个恢复集) applies this policy. It assumes a quiescent, access-controlled tree; it is not an adversarial filesystem snapshot. A failed copy or manifest check leaves an incomplete set: inspect it and repeat into a new destination rather than trusting the directory's existence.

Protect directories with mode 0700 and database/content files with mode 0600. Keep binary version, backup time, clean-shutdown confirmation and secret/configuration references in the recovery manifest. Protect secret backups separately; database credential hashes cannot recover plaintext Tokens.

A complete recovery set must distinguish:

- **SQLite and managed content:** Artifact metadata and upload state are in the database; `kairos://` bytes are in the store. Neither half is sufficient alone. Do not rename blobs or reconstruct Artifact IDs manually.
- **External Artifact URIs:** database/store backup does not copy their remote bytes, access rights or retention. Arrange their own recovery and match it to this set. Never send Kairos credentials to external hosts.
- **Configuration and credentials:** retain recoverable Admin/Identity credential provisioning, deployment/TLS/proxy configuration and any secret-bearing DSN separately. Preserve the whole database, including stable identity rows.
- **Workspace and provider login:** they are separate from the managed store and may contain sensitive data. Back them up only under an explicit policy, separately from published evidence and release archives. A restored database does not reconnect an old Harness or revive an ended Claim.

## Restore into an isolated working copy

Keep the original recovery set read-only. Copy it into a fresh private directory, validate its manifest, and use the original binary version with explicit restored SQLite/store paths, an empty PostgreSQL DSN and an independent loopback port. Do not connect production Daemons or automation: restored pending work could otherwise trigger external effects.

Run SQLite `PRAGMA quick_check` on the working copy, not the unique backup; opening SQLite may operate sidecars. Then start isolated Core and verify:

1. Health, configured authentication mode and expected session identity.
2. Known WorkItem state/result and Submission/Review associations.
3. Exact submitted Artifact IDs and authenticated downloaded content/digests.

A health check or SQLite check alone is insufficient. Test ordinary production authentication with protected credentials separately; a Trusted fixture does not establish authenticated identity recovery. Expired Claims and background retention work must be assessed against actual restored business state.

After verification, stop the isolated Core and record the result. Any real switch requires another clean no-write window, an independent snapshot of the current state, matching database/store/configuration and a reviewed rollback plan. Restore matching recovery sets; do not mix database and store versions. Cross-version migration and rollback require separate validation.

## Reproduce the measured fixture

```sh
python3 examples/review-loop/verify-sqlite-restore.py \
  --server /absolute/path/kairos-server --output /private/restore-evidence.json
```

This script owns a temporary loopback Trusted service. It creates a completed WorkItem and submitted managed Artifact, stops Core, copies the full stopped source directory, restores a new working copy, checks SQLite integrity, WorkItem state, exact Artifact identity and bytes, then stops services and removes temporary data. It never selects an existing production database.

The measured Darwin fixture returned `quick_check=ok`, a completed WorkItem, preserved Artifact ID and identical content with SHA-256 `f4c971f17be63da606673faba8291f31e01796eafe765b2840365e3a2e9056b6`. The copy example also accepted regular file sets and rejected a symlink/FIFO before creating a backup. These results do not establish online backups, PostgreSQL, deployment-supervisor guarantees, production credential/external-store/workspace recovery, recovery-time targets, or cross-version upgrades.

Implementation references: [API reference](../api-reference.md), `cmd/kairos-server/main.go`, `internal/repository/repository.go`, `internal/artifactstore/local.go`, the [Adapter guide](../../internal/daemon/codexadapter/README.md), and `examples/review-loop/verify-sqlite-restore.py`.
