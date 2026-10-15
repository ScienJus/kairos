# Install a release package

[简体中文](installation.zh-CN.md)

The first release target is `v0.1.0-alpha.1`, for early evaluation. Until it appears on [GitHub Releases](https://github.com/ScienJus/kairos/releases), use a locally built snapshot as described in the [release checklist](https://github.com/ScienJus/kairos/blob/main/docs/releasing.md). Snapshot builds are candidates, not published releases.

## Download and verify

Select the archive matching your OS and CPU: `darwin` for macOS or `linux`, and `arm64` or `amd64` (`uname -m` reports `arm64`/`aarch64` or `x86_64`). Each archive contains Core, Daemon, the console, examples and third-party notices. Go and Node.js are not needed to run it. Running the examples also needs `curl`, `jq`, `openssl` and Perl/POSIX; model execution separately needs the configured CLI and login.

Download the archive and `checksums.txt` from the same release. Compare its SHA-256 digest with the exact filename in `checksums.txt` using `shasum -a 256 archive.tar.gz` on macOS or `sha256sum archive.tar.gz` on Linux. Release-workflow archives also have [GitHub provenance attestations](https://github.com/ScienJus/kairos/attestations); verify them with `gh attestation verify archive.tar.gz --repo ScienJus/kairos`. A local snapshot has no GitHub attestation.

Extract into a new directory and run `./kairos-server --version` and `./kairos-daemon --version`; both must match the selected version. macOS packages do not currently have Developer ID signing or notarization.

## First local start

From the extracted directory, use a new private data directory:

```sh
mkdir -m 700 data
KAIROS_AUTH_MODE=trusted KAIROS_POSTGRES_DSN='' \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_SQLITE_PATH="$PWD/data/kairos.db" \
KAIROS_ARTIFACT_DIR="$PWD/data/artifacts" \
  ./kairos-server
```

Open `http://127.0.0.1:8080` for the console; health is at `/healthz` and MCP at `/mcp`. This local trial explicitly uses Trusted Mode. For shared use, configure Authenticated Mode and follow the [API configuration and authentication reference](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.md); the alpha does not provide tenant isolation.

Ctrl-C stops Core. Repeat the same command with the same data paths to reopen its persisted state; do not repeat `mkdir` or delete the data. A restart is not a backup or an upgrade test. Keep the database and matching managed Artifact store together when planning recovery.

## Isolated examples

From the extracted directory:

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

These commands create separate temporary data and verify fixture setup without calling a model; they do not execute Tasks. For actual execution, see the packaged [Daemon example](../examples/daemon/README.md) or [Writer/Checker example](../examples/review-loop/README.md). The Daemon requires explicit model/CLI configuration; starting Core alone does not run agents.

## Alpha boundaries

API/schema compatibility is not guaranteed before 1.0; check release notes before changing binaries and retain a recoverable copy of existing data. Real-model validation is currently limited; no production readiness, cross-version upgrade or production backup/restore guarantee is made. After a Daemon crash, lease expiry does not prove the old Harness or external work has stopped; confirm cleanup before starting a replacement. See the [Daemon guide](https://github.com/ScienJus/kairos/blob/main/internal/daemon/README.md) for execution boundaries.
