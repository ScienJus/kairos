# 安装发布包

[English](installation.md)

从 [GitHub Releases](https://github.com/ScienJus/kairos/releases) 下载匹配系统（`darwin`/`linux`）与 CPU（`arm64`/`amd64`）的 Archive，以及同版本 `checksums.txt`。首个版本发布前，可按[发布清单](https://github.com/ScienJus/kairos/blob/main/docs/releasing.zh-CN.md)构建 Snapshot。

每个包包含 Core、Daemon、控制台和示例，运行无需 Go 或 Node.js。macOS 用 `shasum -a 256 archive.tar.gz`，Linux 用 `sha256sum archive.tar.gz`，与清单中准确文件名的 SHA-256 比较。公开 workflow 产物还可用 `gh attestation verify archive.tar.gz --repo ScienJus/kairos` 验证来源；本地 Snapshot 没有 GitHub 来源证明。

在新目录解压，核对两个版本均匹配下载版本，再启动本地试用：

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

打开 `http://127.0.0.1:8080` 使用控制台，`/healthz` 为健康检查。Ctrl-C 停止 Core；重启时复用相同数据路径，无需重复 `mkdir`。此本地试用使用 Trusted Mode，共享使用需配置 [Authenticated Mode](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.zh-CN.md)。

在解压目录验证示例初始化，不调用模型：

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

示例依赖 `curl`、`jq`、`openssl` 和 Perl/POSIX。初始化不执行 Task；模型执行另外需要显式配置 CLI、模型和登录，详见包内 [Daemon](../examples/daemon/README.zh-CN.md)和 [Writer/Checker](../examples/review-loop/README.zh-CN.md) 示例。

首个目标为早期试用版 `v0.1.0-alpha.1`。兼容性和验证边界，包括 macOS 包尚未签名/公证，统一见[发布说明](https://github.com/ScienJus/kairos/blob/main/docs/release-notes.zh-CN.md)。
