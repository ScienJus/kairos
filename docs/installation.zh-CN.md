# 安装发布包

[English](installation.md)

首个发布目标为 `v0.1.0-alpha.1`，面向早期试用。在 [GitHub Releases](https://github.com/ScienJus/kairos/releases) 出现该版本前，可按[发布清单](https://github.com/ScienJus/kairos/blob/main/docs/releasing.zh-CN.md)构建本地 Snapshot；Snapshot 是候选包，不是已发布版本。

## 下载与校验

选择匹配系统和 CPU 的 Archive：macOS 为 `darwin`，Linux 为 `linux`；CPU 为 `arm64` 或 `amd64`（`uname -m` 对应输出 `arm64`/`aarch64` 或 `x86_64`）。每个包包含 Core、Daemon、控制台、示例和第三方声明，运行无需 Go 或 Node.js。示例还需要 `curl`、`jq`、`openssl` 和 Perl/POSIX；模型执行另外需要已配置的 CLI 与登录。

从同一个 Release 下载 Archive 和 `checksums.txt`。macOS 用 `shasum -a 256 archive.tar.gz`，Linux 用 `sha256sum archive.tar.gz`，与清单中准确文件名对应的 SHA-256 比较。Release workflow 产物另有 [GitHub 来源证明](https://github.com/ScienJus/kairos/attestations)，可用 `gh attestation verify archive.tar.gz --repo ScienJus/kairos` 验证；本地 Snapshot 没有 GitHub 来源证明。

解压到新目录，执行 `./kairos-server --version` 和 `./kairos-daemon --version`，两者必须匹配所选版本。macOS 发布包目前没有 Developer ID 签名或公证。

## 首次本地启动

在解压目录创建新的私有数据目录：

```sh
mkdir -m 700 data
KAIROS_AUTH_MODE=trusted KAIROS_POSTGRES_DSN='' \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_SQLITE_PATH="$PWD/data/kairos.db" \
KAIROS_ARTIFACT_DIR="$PWD/data/artifacts" \
  ./kairos-server
```

打开 `http://127.0.0.1:8080` 使用控制台；健康检查为 `/healthz`，MCP 为 `/mcp`。此本地试用显式使用 Trusted Mode。共享使用应配置 Authenticated Mode，并遵循 [API 配置与认证参考](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.zh-CN.md)；alpha 不提供租户隔离。

Ctrl-C 停止 Core。再次使用相同命令和数据路径启动，可读取已有持久化状态；无需重复 `mkdir`，不要删除数据。重启不等于备份或升级验证；设计恢复方案时须把数据库与匹配的托管 Artifact store 作为一个恢复集。

## 隔离示例

在解压目录运行：

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

这些命令用独立临时数据验证 fixture 创建，不调用模型，也不执行 Task。实际执行见包内 [Daemon 示例](../examples/daemon/README.zh-CN.md)或 [Writer/Checker 示例](../examples/review-loop/README.zh-CN.md)。Daemon 需要显式配置模型和 CLI；只启动 Core 不会运行 Agent。

## Alpha 边界

1.0 前不保证 API/Schema 兼容；替换二进制前查看发布说明，并保留可恢复的数据副本。真实模型验证范围仍有限，不承诺生产可用、跨版本升级或生产备份恢复。Daemon 崩溃后租约到期，不证明旧 Harness 或外部操作已停止；启动替代执行前须确认清理。执行边界见 [Daemon 指南](https://github.com/ScienJus/kairos/blob/main/internal/daemon/README.md)。
