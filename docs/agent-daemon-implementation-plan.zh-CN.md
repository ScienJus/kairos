# Agent Daemon 实现与验收记录

> 状态：阶段 1–5 已完成。当前能力见 [README](https://github.com/ScienJus/kairos/blob/main/README.zh-CN.md#当前状态)，后续方向见 [Roadmap](https://github.com/ScienJus/kairos/blob/main/ROADMAP.zh-CN.md)，设计边界见 [Agent Daemon 白皮书](whitepapers/agent-daemon.zh-CN.md)。

本文只保留已实现阶段、验收边界和未覆盖范围，不再作为开发计划维护。

## 实现边界

Agent Daemon 是绑定单一 Agent Identity 的长期运行进程。它发现 Workflow Task 和 Blackboard 协调候选，建立 Claim，通过 Adapter 启动 Harness，并将类型化 outcome 收敛到 Core。

首个可用版本包含：

- Claim-bound Executor Credential 与 scope 授权；
- 单次 Dispatch 状态机、独立 heartbeat 和终态核对；
- 共享 slots、Probe、cooldown、budget 和 quarantine；
- 显式启用的本地 Codex Adapter 与隔离 workspace；
- Core/Daemon 双二进制、隔离示例、E2E、SBOM 和发布打包；
- Daemon 实例、Dispatch 快照和关键事件的平台可观测性。

MVP 不包含 Dispatch 持久化、跨进程重连、外部 supervisor、全局公平调度、任务优先级或原子批量 Blackboard 规划。

## 交付阶段

| 阶段 | 交付结果 | 主要证据 |
| --- | --- | --- |
| 1. Executor Credential | Claim Token、principal、scope 授权和双数据库迁移。 | HTTP/MCP、SQLite/PostgreSQL、幂等、失效与竞态测试。 |
| 2. Dispatch 引擎 | Task/Coordination Dispatch、Adapter 契约、heartbeat、停止和终态核对。 | fake Adapter 与真实 Core HTTP/SQLite 测试。 |
| 3. 连续调度 | slots、Probe、轮转、按候选代次的抑制和优雅停机。 | 故障注入、race 测试和可重现的 scheduler 测试。 |
| 4. Codex Adapter | CLI 子进程、受保护环境、MCP 配置、outcome schema 和 workspace 回收。 | macOS 真实模型 smoke，脚本 Harness 覆盖失败与清理。 |
| 5. 集成与发布 | Workflow/Blackboard 隔离示例、双二进制发布、SBOM/Notices 和 E2E。 | `make daemon-e2e`、发布配置和本地归档验收。 |

## 关键验收条件

### 授权

- Executor Token 只能读取绑定 WorkItem 内的必要上下文，并执行 Profile 允许的写入。
- Claim 结束、WorkItem 终态或 scope 不匹配时，读写都被拒绝。
- Identity Token 轮换不破坏已发放的 Active Executor Token；Claim 状态仍是权威失效条件。

### 运行正确性

- 基础设施故障不伪造为 Task Failure；终态不确定时保留 Dispatch 并核对 Core 历史。
- 旧 Harness 未确认终止时不启动替代运行。
- 健康恢复只解除新 Claim 暂停，不清空 cooldown、budget 或 quarantine。
- 候选代次未变时，连续运行不会无限重复领取。

### 交付

- `make build` 同时生成 Core 和 Daemon；发布包覆盖 Linux/macOS amd64/arm64。
- `make daemon-e2e` 使用脚本 Harness 覆盖两种协调模式，不消耗付费模型。
- 优雅停机会尽力停止已知 Harness 并释放 Claim；崩溃后由 Core reaper 回收未续租 Claim。

## 仍需独立验证的范围

- Linux 上的真实 Codex CLI 及更多 Provider/CLI 版本。
- 不同容器、systemd 和 Kubernetes 部署下的进程清理保证。
- Provider 配额、登录、网络代理和反向代理 Host 的完整故障矩阵。
- 实际 Release 发布与升级验证。

这些是加固与部署验证，不改变已实现的 Core/Daemon 契约。
