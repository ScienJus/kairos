# Install a release package

[简体中文](installation.zh-CN.md)

Download the archive for your OS (`darwin`/`linux`) and CPU (`arm64`/`amd64`) from [GitHub Releases](https://github.com/ScienJus/kairos/releases), together with `checksums.txt`. Until the first release is published, build a snapshot using the [release checklist](https://github.com/ScienJus/kairos/blob/main/docs/releasing.md).

Each package contains Core, Daemon, the console and examples; running it needs no Go or Node.js. Compare `shasum -a 256 archive.tar.gz` (macOS) or `sha256sum archive.tar.gz` (Linux) with the exact filename in `checksums.txt`. Published workflow assets also support `gh attestation verify archive.tar.gz --repo ScienJus/kairos`; local snapshots have no GitHub attestation.

Extract into a new directory, check both versions match the download, and start a local trial:

```sh
tar -xzf archive.tar.gz
./kairos-server --version
./kairos-daemon --version
mkdir -m 700 data
KAIROS_AUTH_MODE=trusted KAIROS_POSTGRES_DSN='' \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_SQLITE_PATH="$PWD/data/kairos.db" \
KAIROS_ARTIFACT_DIR="$PWD/data/artifacts" \
  ./kairos-server
```

Open `http://127.0.0.1:8080` for the console; `/healthz` checks health. Ctrl-C stops Core. Reuse the same data paths on restart without repeating `mkdir`. This local trial uses Trusted Mode; shared use requires [Authenticated Mode configuration](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.md).

From the extracted directory, verify example setup without calling a model:

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

Examples need `curl`, `jq`, `openssl` and Perl/POSIX. Setup does not execute Tasks; model execution additionally needs an explicitly configured CLI/model and login. See the packaged [Daemon](../examples/daemon/README.md) and [Writer/Checker](../examples/review-loop/README.md) examples.

The first target is `v0.1.0-alpha.1` for early evaluation. Compatibility and validation limits, including unsigned/unnotarized macOS packages, are documented in the [release notes](https://github.com/ScienJus/kairos/blob/main/docs/release-notes.md).
