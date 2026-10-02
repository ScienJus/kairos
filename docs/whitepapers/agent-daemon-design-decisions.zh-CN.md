# Agent Daemon 设计决策摘要

> 状态：历史决策记录。当前规范性边界见 [Agent Daemon 白皮书](agent-daemon.zh-CN.md)，精确 API 以 [OpenAPI](../openapi.yaml) 为准。

本文只保留理解当前实现所需的已确认决策。完整讨论过程可从 Git 历史查看，不再在当前文档中维护过期的待讨论列表。

## 职责与契约

| 决策 | 结论 | 主要原因 |
| --- | --- | --- |
| D2 候选范围 | Daemon 处理 `find_work` 返回的 Task 和 Blackboard 协调候选。 | 自动执行不应只覆盖已存在 Task 的情况。 |
| D3 生命周期归属 | Daemon 持有 Dispatch 生命周期；Harness 只返回类型化结果。 | 续租、停止、重试和 Core 终态核对必须由一个边界统一管理。 |
| D4 协调结果 | Blackboard 生命周期选择用 `CoordinationDecision` 表达。 | Harness 表达意图，Daemon 再调用已有 Core 操作。 |
| D5 协调 Claim | 复用现有 Coordination Claim，不新增 Daemon 专用领取协议。 | 现有协议已提供独占、lease、heartbeat 和 fencing。 |
| D6 幂等责任 | 直接调用 Core 的一方管理 `operation_id`。 | Harness 不应了解 Core 的传输重试细节。 |
| D7 批量规划 | MVP 不增加原子批量 Blackboard 规划 API。 | 它是独立的 Core 领域能力，不应由 Daemon 集成需求偷渡。 |

## 凭据与 Harness 边界

| 决策 | 结论 | 主要原因 |
| --- | --- | --- |
| D8 受限写入 | Task Harness 可以在 Claim scope 内读取上下文、创建 Artifact 和执行允许的 Blackboard 非终态写入。 | 让 Harness 完成工作，同时不赋予跨 Task 权限。 |
| D9 Executor Credential | Daemon 在领取时生成 Claim-bound Token，Core 只存 hash；Claim 结束后立即失效。 | 将具体 Harness 与长期 Identity Token 隔离。 |
| D10 Adapter 契约 | Adapter 只提供 `Probe` / `Start` / `Observe` / `Stop`；终态记录可由可选 `RunForgetter` 回收。 | 保留最小可替换运行时边界。 |
| D11 观察模型 | Adapter 返回快照式运行状态和类型化 outcome，不共享可变状态。 | 降低并发耦合，并让响应丢失可核对。 |
| D12 故障分类 | Provider、进程、网络和协议故障不得伪造为 Task 业务失败。 | 基础设施问题不应污染持久工作记录。 |
| D13 运行时重试 | 只有确认旧 Harness 已终止，才能在同一 Claim 中重试。 | 避免两个 Harness 并发使用同一执行权。 |

## 不确定、调度与崩溃边界

| 决策 | 结论 | 主要原因 |
| --- | --- | --- |
| D14 Heartbeat 失败 | 区分“暂时不确定”与 Core 权威确认的执行权失效。 | 网络错误不等于 Claim 已丢失。 |
| D15 Identity 绑定 | 一个 Daemon 实例绑定一个 Agent Identity，可运行多个 slots。 | 使发现、Role 和 Claim 归属始终一致。 |
| D16 崩溃恢复 | MVP 不持久化 Dispatch，不在重启后重连旧 Harness。 | 无外部 supervisor 时无法安全证明旧运行状态。 |
| D17 调度层级 | 候选抑制、轮转和健康暂停都是 Daemon 本地策略。 | Core 只提供候选与唯一 Claim，不承担全局队列调度。 |
| D18 健康恢复 | Probe 恢复只解除新领取暂停，不清空 candidate cooldown、budget 或 quarantine。 | 不让短暂恢复触发重复领取风暴。 |
| D19 单一状态源 | Dispatch 只有一份可变运行状态，`Run` 是唯一对外驱动入口。 | 避免 step、heartbeat 和 scheduler 各自维护互相冲突的状态。 |

## 几个看似更简单的方案为什么没有采用

- **让 Harness 直接完成全部生命周期写入**：这会把长期 Identity Token、幂等重试和不确定响应核对分散到每一种 Harness，无法保证不同 Adapter 具有相同的收敛行为。
- **让 Core 直接启动模型或进程**：这会把领域协调与 Provider、沙箱、进程管理和本地 workspace 耦合在一起，破坏 Core 的可部署边界。
- **只持久化 RunRef 以支持崩溃恢复**：RunRef 不能证明旧进程仍是同一个运行，也不能提供重新观察和安全停止能力；没有外部 supervisor 时，恢复只是未经证实的猜测。
- **把 Daemon 遥测当作 Claim 真相**：上报可以延迟、丢失或乱序。若它参与责任判断，单纯的观测故障就会改变业务状态。

## 非当前契约

- 统一的 `apply_coordination_decision` Core API 或原子批量 Blackboard 规划。
- 跨进程 Dispatch 恢复、旧 Harness 重连或孤儿进程终止保证。
- Core 管理的全局优先级、公平调度或路由队列。
- 通过平台遥测推断 Claim 归属或业务成功。

这些能力如需引入，必须作为独立的 Core、运行时或部署契约重新设计。
