# Kairos 快速体验

[English](README.md)

这个示例包含两个可以立即并行执行的 Task，以及一个仅在两个上游结果都提交后才会开放的汇合 Task。所有执行者共享同一个持久 WorkItem，独占 Claim 会阻止重复执行。

## 运行示例

使用包含本示例的发布包时，启动仅需 curl 和包内服务端。在解压目录运行：

```sh
sh examples/quickstart/run.sh
```

较早的 `v0.1.0-alpha.1` Archive 不包含 quickstart，请按[手动安装流程](../../docs/installation.zh-CN.md)使用。

源码构建要求 Go 1.26.10 或更高版本、Node.js 22.22.2+（22.x）、24.15.0+（24.x）或 26+、npm 和 curl。

在仓库根目录执行：

```bash
make quickstart
```

`make quickstart` 先构建 Kairos，发布包命令直接使用预编译二进制。两种方式均在 `http://127.0.0.1:8080` 以 Trusted Mode 启动服务，忽略继承的 PostgreSQL DSN，并向隔离环境写入示例 Workflow 和 WorkItem。打开该地址即可在 operations console 中观察执行过程。按 Ctrl-C 后，临时数据库和 Artifact 目录会自动删除。

如果 8080 端口已被占用，可以指定其他地址：

```bash
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 make quickstart
# 发布包解压目录中使用：
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 sh examples/quickstart/run.sh
```

## 接入 Agent

源码仓库与新发布包均包含 `.codex/config.toml` 和 `.agents/skills/kairos-agent`。使用脚本打印的 Codex 命令，其中已经包含实际 MCP URL。默认端口的命令如下，请从该目录分别打开终端：

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex -c 'mcp_servers.kairos.url="http://127.0.0.1:8080/mcp"'
```

在 Codex 中信任该目录，以加载项目级 MCP 配置。更换监听端口后，使用脚本打印的 URL 替换 8080。若 Agent 在其他项目工作，按[独立接入说明](../../docs/installation.zh-CN.md#接入-agent)操作；仅安装 Skill 不会配置 MCP。实际执行 Agent 需要 Codex CLI 已登录。

第二个会话使用 `quickstart-agent-2`。分别向会话发出指令：

```text
Use $kairos-agent to find and complete one available Task.
```

两个会话可以并行 Claim 两个初始 Task。第三次尝试在出现新候选项之前不会发现可执行 Task。两个初始结果都提交后，Kairos 会开放汇合 Task，并在它的执行上下文中提供两个上游结果。

只使用一个会话也可以，它会按顺序完成这些 Task。

`workflow.json` 的 `max_task_instances_per_node: 10` 表示每个定义节点各自最多产生 10 个 Task 实例，流程不再设置实例总数上限。

体验恢复时，可让一个分支失败，再在控制台选择**继续执行**，新尝试会与已成功的分支汇合。恢复及从头执行的详细规则见 [Workflow 恢复](../../docs/api-reference.zh-CN.md#workflow-恢复)。
