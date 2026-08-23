# Kairos 快速体验

[English](README.md)

这个示例包含两个可以立即并行执行的 Task，以及一个仅在两个上游结果都提交后才会开放的汇合 Task。所有执行者共享同一个持久 WorkItem，独占 Claim 会阻止重复执行。

## 运行示例

环境要求：Go 1.26.6 或更高版本、Node.js 20 LTS 或更高的 LTS 版本、npm 和 curl。

在仓库根目录执行：

```bash
make quickstart
```

命令会构建 Kairos，在 `http://127.0.0.1:8080` 启动服务，并向隔离环境写入示例 Workflow 和 WorkItem。打开该地址即可在 operations console 中观察执行过程。按 Ctrl-C 后，临时数据库和 Artifact 目录会自动删除。

如果 8080 端口已被占用，可以指定其他地址：

```bash
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 make quickstart
```

## 接入 Agent

仓库已经包含 Codex MCP 配置和 Kairos Skill。在仓库目录下分别打开一个或多个终端，为每个会话配置不同的 Actor ID 和 `contributor` Role：

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

第二个会话使用 `quickstart-agent-2`。分别向会话发出指令：

```text
Use $kairos-agent to find and complete one available Task.
```

两个会话可以并行 Claim 两个初始 Task。第三次尝试在出现新候选项之前不会发现可执行 Task。两个初始结果都提交后，Kairos 会开放汇合 Task，并在它的执行上下文中提供两个上游结果。

只使用一个会话也可以，它会按顺序完成这些 Task。
