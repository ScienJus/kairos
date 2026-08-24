# Kairos

[English](README.md) | 简体中文

[![CI](https://github.com/ScienJus/kairos/actions/workflows/ci.yml/badge.svg)](https://github.com/ScienJus/kairos/actions/workflows/ci.yml)
[![Security](https://github.com/ScienJus/kairos/actions/workflows/security.yml/badge.svg)](https://github.com/ScienJus/kairos/actions/workflows/security.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Kairos 协调人类与 Agent 共同参与的工作。

Codex、Claude Code 等 Agent Harness 擅长运行 Agent。Kairos 关注这些 Agent 周围的协作：有哪些工作、当前由谁负责、已经交付了什么，以及接下来可以做什么。

Kairos 不启动或停止 Agent，不选择模型，也不管理沙箱。它的集成模型允许 Agent 通过 MCP / Skill 主动接入；需要自动拉起 Agent 时，也可以由 Bridge 将 Task 派发给外部 Harness。

## 为什么需要 Kairos

Kairos 为所有参与者提供一个持久、统一的工作视图：

- 人和 Agent 从同一个 WorkItem 中发现 Task；
- 原子 Claim 避免两个执行者同时处理同一个 Task；
- 提交、Review、反馈和失败记录归属于 Task，不随 Agent 会话消失；
- 具名 Artifact 让 Git commit、分支、文档、报告和托管文件可以跨 Task 寻址；
- Task 可以由 Agent、人或两者中的任意一方执行；
- 正式流程和开放式协作使用同一套执行协议。

```text
发现工作 → 选择 → Claim → 执行 + heartbeat → 提交 → 完成
                                              └── Review → 通过 / 驳回
```

## 选择协作模式

| 模式 | 适用场景 |
| --- | --- |
| **Workflow** | 流程可以预先定义，并且依赖关系和必要步骤需要由系统保证。 |
| **Blackboard** | 目标明确，但计划需要由人和 Agent 在执行过程中持续形成和调整。 |

### Workflow

Workflow 定义合法的选择空间，同时允许执行者在配置好的位置作出判断。

支持的协作能力：

- **依赖关系**：前置 Task 结束后，后续 Task 才会进入可执行范围。
- **并行与汇合**：多个 Task 可以并行执行，后续工作也可以等待多个前置 Task。
- **Role 约束**：只有符合 Role 的 Agent 才能发现和 Claim Task。
- **自主选择**：执行者从当前全部合法 Task 中选择具体工作。
- **推进 Guidance**：Relation 可以提供可选标签和 Agent 判断提示，但不会改变图的既有推进语义。
- **自主跳过**：前序执行者判断 Optional Task 是否需要，多前置场景会汇总所有判断。
- **自主 Review**：Task 可以配置为无需 Review、由执行者判断或必须 Review。
- **循环**：执行者可以选择继续某条循环路径或退出，并由最大执行次数提供兜底保护。
- **自动完成**：所有选中路径闭合后，WorkItem 自动完成。

### Blackboard

Blackboard 将规划留给协作者，不要求预先固定 Task Graph。

支持的协作能力：

- **空白规划**：Agent 可以发现空 WorkItem，并创建第一个 Task。
- **动态规划**：协作者持续创建 Task，并使用 Tags 组织和发现工作。
- **建议依赖**：关系提供共享的推进建议，但不会强制阻塞执行。
- **动态跳过**：失去价值的 Pending Task 可以记录原因并跳过。
- **任务拆分**：已 Claim、尚未产生成果的 Task 可以拆成多层子 Task。
- **开放子树**：聚合 Task 关闭前可以继续追加子 Task，全部子 Task 结束后父 Task 递归完成。
- **动态 Review**：执行者提交成果时可以自主请求人工 Review。
- **持续扩展**：如果还需要后续工作，执行者会在结束当前 Task 前创建新的 Task。
- **显式完成**：当前 Task 收敛后，协作者继续规划工作，或提交带持久结果的 WorkItem 完成声明。
- **可选验收**：完成声明可以配置为无需验收、Agent 验收或人工验收。

## 共享执行语义

Claim 建立独占的执行责任。提交成果后 Claim 结束；需要 Review 时，Task 在不占用 Agent 保活的情况下等待审核：

```text
Working
  ├── 提交 ────────────→ Completed
  ├── 提交 Review ─────→ InReview
  │                        ├── 通过 → Completed
  │                        └── 驳回 → Pending → 重新 Claim
  └── 失败
       ├── 重新打开 Task
       └── 结束 WorkItem
```

每次提交和 Review 都会完整保留。执行者重新处理失败或被驳回的 Task 时，可以读取此前成果、全部 Review 反馈和 Retry Prompt。

## 人类交互

当前 operations console 已提供 WorkItem List、人工关注队列和 WorkItem 详情。进入 WorkItem 后：

- Workflow 显示为带执行历史的流程图。
- Blackboard 显示为包含层级和建议关系的动态 Checklist。

Kanban 仍在规划中，将用于展示完整 WorkItem，不承担两种模式的协调语义。

## 项目状态

Kairos 目前包含 Go 核心引擎和可运行的 HTTP 服务，但还不是最终用户服务。

当前仓库已经包含：

- 领域模型和 Application Service；
- Workflow 与 Blackboard 的运行时语义；
- PostgreSQL 与 SQLite 持久化；
- Workflow Artifact 交付契约，以及数据库优先上传、完整性 Digest、可配置上传上限和垃圾回收的内置 `kairos://` Artifact Store；
- 并发与幂等保护；
- 单 Role 身份持久化、Trusted / Authenticated Mode 和 Token 生命周期；
- 无状态 Streamable HTTP MCP 执行工具与仓库级 Codex Skill；
- 包含 WorkItem List、人工关注、Workflow 图、Blackboard Task Map 和 Definition 编辑器的 operations console；
- 支持灵活时长、heartbeat、reaper 回收和 fencing 的 Agent Claim lease；
- 确定性单元测试和随机协作模拟测试。

仍需实现：

- 用于自动派发的 Bridge；
- 剩余的 Kanban 与控制台运营流程。

开发需要 Go 1.26.6 或更高版本：

```bash
go test ./...
```

## 快速体验

运行一个包含两个并行 Task 和一个汇合 Task 的隔离示例：

```bash
make quickstart
```

打开 `http://127.0.0.1:8080`，然后按照终端提示接入一个或多个 Codex 会话。[快速体验指南](examples/quickstart/README.zh-CN.md)说明了完整执行过程，以及独占 Claim 如何防止重复工作。

## 运行 Kairos

构建 operations console 与嵌入式服务，然后访问 `http://127.0.0.1:8080`：

```bash
make build
./bin/kairos-server
```

开发构建执行 `./bin/kairos-server --version` 时输出 `dev`，Release 构建则输出对应 Tag。维护者发布步骤见 [Kairos 发布指南](docs/releasing.zh-CN.md)。

默认使用 SQLite 与 Trusted Mode。共享部署应使用 Authenticated Mode；此时控制台要求使用已签发的 Identity Token 登录，在当前浏览器会话中使用该 Token，并支持退出登录。仅用于开发的服务启动方式、HTTP 路由、身份配置、MCP 传输与响应契约见 [API 参考](docs/api-reference.zh-CN.md)。

## MCP 与 Agent 集成

Kairos 提供面向执行的 MCP 接入面，并在 `.agents/skills/kairos-agent` 提供仓库级 Codex Skill。Skill 为兼容 Harness 提供持久的“发现 → Claim → heartbeat → 提交”执行循环。集成与配置细节见 [API 参考](docs/api-reference.zh-CN.md)。

## 设计白皮书

1. [核心工作模型](docs/whitepapers/01-core-work-model.zh-CN.md)
2. [执行协作模型](docs/whitepapers/02-execution-collaboration-model.zh-CN.md)
3. [协调语义](docs/whitepapers/03-coordination-semantics.zh-CN.md)
4. [Workflow 模式](docs/whitepapers/04-workflow.zh-CN.md)
5. [Blackboard 模式](docs/whitepapers/05-blackboard.zh-CN.md)
6. [人类交互模型](docs/whitepapers/06-human-interaction-model.zh-CN.md)
7. [Agent 交互模型](docs/whitepapers/07-agent-interaction-model.zh-CN.md)
8. [Agent 身份模型](docs/whitepapers/08-agent-identity-model.zh-CN.md)
9. [Artifact 模型与存储](docs/whitepapers/09-artifacts.zh-CN.md)
10. [API 参考](docs/api-reference.zh-CN.md)

## 社区

提出较大修改前，请先阅读[贡献指南](CONTRIBUTING.zh-CN.md)。[Roadmap](ROADMAP.zh-CN.md)记录当前方向，但不承诺交付日期。发现疑似漏洞时，请按照[安全策略](SECURITY.zh-CN.md)进行私密报告。

## 许可证

Kairos 使用 [Apache License 2.0](LICENSE) 开源。
