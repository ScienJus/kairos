# 安装发布包

[English](installation.md)

从 [GitHub Releases](https://github.com/ScienJus/kairos/releases) 下载匹配系统（`darwin`/`linux`）与 CPU（`arm64`/`amd64`）的 Archive，以及同版本 `checksums.txt`。未发布版本可按[发布清单](releasing.zh-CN.md)构建 Snapshot。

每个包包含 Core、Daemon、控制台和示例，运行无需 Go 或 Node.js。macOS 用 `shasum -a 256 archive.tar.gz`，Linux 用 `sha256sum archive.tar.gz`，与清单中准确文件名的 SHA-256 比较。公开 workflow 产物还可用 `gh attestation verify archive.tar.gz --repo ScienJus/kairos` 验证来源；本地 Snapshot 没有 GitHub 来源证明。

在新目录解压，核对两个版本均匹配下载版本：

```sh
tar -xzf archive.tar.gz
./kairos-server --version
./kairos-daemon --version
```

## 运行包内示例

从本次修订构建的包已包含 quickstart、Skill 和本地 Codex MCP 配置。在解压目录运行：

```sh
sh examples/quickstart/run.sh
```

示例需要 curl，会在回环地址启动 Core，并创建两个并行 Task 和一个汇合 Task。启动示例无需 Go、Node.js，也不调用模型。使用脚本打印的命令连接 Codex，两个会话的操作见包内[快速体验指南](../examples/quickstart/README.zh-CN.md)。Ctrl-C 停止 Core，并删除临时示例数据。

已发布的 `v0.1.0-alpha.1` Archive 不包含 quickstart、Skill 和 MCP 配置。使用该版本时，按下文手动启动，在控制台创建协作方案，再按[接入 Agent](#接入-agent)配置执行者。`make quickstart` 用于源码仓库，不是发布包命令。

## 使用持久数据手动启动 Core

先停止占用 8080 端口的示例，再在解压目录执行：

```sh
mkdir -m 700 data
KAIROS_AUTH_MODE=trusted KAIROS_POSTGRES_DSN='' \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_SQLITE_PATH="$PWD/data/kairos.db" \
KAIROS_ARTIFACT_DIR="$PWD/data/artifacts" \
  ./kairos-server
```

打开 `http://127.0.0.1:8080` 使用控制台，`/healthz` 为健康检查。Ctrl-C 停止 Core；重启时复用相同数据路径，无需重复 `mkdir`。此本地试用使用 Trusted Mode，共享使用需配置 [Authenticated Mode](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.zh-CN.md)。

空控制台中点击**创建工作**，首次引导会带你创建步骤明确的 **Workflow**，或打开供 Agent 边做边规划的 **Blackboard 资料架**。发布 Definition 后，点击**使用这个 Workflow/Blackboard**创建 WorkItem。旧版控制台没有引导时，先从顶部导航进入 Workflow 或 Blackboard。Agent Task 会等待执行者接入，配置方式见下文。

在解压目录验证示例初始化，不调用模型：

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

示例依赖 `curl`、`jq`、`openssl` 和 Perl/POSIX。初始化不执行 Task；模型执行另外需要显式配置 CLI、模型和登录，详见包内 [Daemon](../examples/daemon/README.zh-CN.md)和 [Writer/Checker](../examples/review-loop/README.zh-CN.md) 示例。

兼容性和验证边界，包括 macOS 包尚未签名/公证，统一见[发布说明](release-notes.zh-CN.md)。

## 接入 Agent

在 Agent 实际工作的项目目录中安装 Kairos Agent Skill。安装器需要 Node.js 和 npm：

```sh
npx skills add ScienJus/kairos --skill kairos-agent
```

按提示选择目标 Agent 和项目级安装范围。源码仓库，以及包含 `.agents/skills/kairos-agent` 的新发布包，已为从该目录打开的会话提供 Skill。

Skill 提供执行指令，Core 启动和 MCP 配置需要分别完成。在其他项目中使用 Agent 无需克隆 Kairos 仓库。

### Codex：本地 Trusted Mode

在 Agent 实际工作的项目中创建 `.codex/config.toml`（目录不存在时先创建 `.codex/`），加入以下条目。已有文件时保留其他设置；已有 `kairos` 条目时直接修改，避免重复声明：

```toml
[mcp_servers.kairos]
url = "http://127.0.0.1:8080/mcp"
env_http_headers = { "X-Kairos-Actor-Id" = "KAIROS_ACTOR_ID", "X-Kairos-Actor-Kind" = "KAIROS_ACTOR_KIND", "X-Kairos-Actor-Role" = "KAIROS_ACTOR_ROLE" }
enabled = true
```

使用其他端口或主机时替换 URL。从该项目目录启动 Codex CLI：

```sh
KAIROS_ACTOR_ID=my-project-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

按提示信任项目，让 Codex 加载项目级配置。并发执行者使用不同 Actor ID；Role 必须匹配 Task 允许的角色（quickstart 使用 `contributor`）。这些 Trusted Mode ID 无需签发 Token。环境变量必须传入实际承载 MCP 连接的进程；在终端设置变量不会更新已经运行的桌面应用。配置范围见 [Codex MCP 文档](https://developers.openai.com/codex/mcp)。

### Authenticated Mode 与其他客户端

使用 Authenticated 服务时，由管理员在控制台 Token 管理页签发角色匹配的 **Agent** Identity Token。将 Trusted Mode 条目替换为：

```toml
[mcp_servers.kairos]
url = "https://YOUR_KAIROS_HOST/mcp"
bearer_token_env_var = "KAIROS_IDENTITY_TOKEN"
enabled = true
```

将签发的 Token 设置到 Codex 进程的 `KAIROS_IDENTITY_TOKEN` 环境变量后启动 Codex。不要将 Token 提交到配置文件，也不要同时发送 Trusted 身份请求头和 Bearer 认证。身份生命周期以 [API 参考](api-reference.zh-CN.md#身份与认证)为准。

其他客户端使用 **Streamable HTTP**、相同的 `/mcp` URL 和传输身份：本地 Trusted Mode 发送 `X-Kairos-Actor-Id`、`X-Kairos-Actor-Kind: agent`、`X-Kairos-Actor-Role`；Authenticated Mode 发送 `Authorization: Bearer <Agent Identity Token>`。环境变量展开与 MCP 重新加载方式遵循客户端说明。仅安装 Skill 不会安装这些配置。

### 验证连接

在 Codex 中使用 `/mcp` 确认 `kairos` 已连接，再发出指令：

```text
Use $kairos-agent to find and complete one available Task.
```

Agent 应调用 `find_work`。候选列表为空表示连接成功，但当前身份没有可执行工作；到控制台检查 WorkItem 和 Task 角色。工具缺失表示客户端配置尚未生效，`401` 表示认证缺失或无效。重试前检查端点、身份、项目信任和客户端重新加载。完整的双 Agent 示例见[快速体验](../examples/quickstart/README.zh-CN.md)。
