# Kairos Agent 交互模型

Agent 会话是临时的，但它接下的责任必须清楚而且可恢复。Kairos 让主动 Agent 和 Daemon 启动的 Harness 使用同一个循环：找到工作、理解工作、建立 Claim、维持 Claim，最后以明确结果结束。

这套循环不要求原始会话一直存活。Claim 和随后产生的工作记录会把连续性保留下来。

## 执行循环

```text
discover / receive
        ↓
inspect context
        ↓
claim responsibility
        ↓
execute + heartbeat
        ↓
submit / fail / release
```

每次生命周期写入都必须使用当前 Claim、正确版本和符合幂等要求的参数。冲突后必须重新读取权威状态，不能用本地猜测继续提交。

## 发现与协调候选

`find_work` 根据协调模式、执行者类型、Agent Role 和查询上下文返回候选。候选不是分配结果；Agent 仍需选择并建立 Claim。

Task 候选直接领取 Task。下列 Blackboard 情况使用 WorkItem Coordination Claim：

- `empty_blackboard`：创建首个 Task 或直接提交完成；
- `blackboard_completion`：创建后续 Task 或提交完成结果；
- `work_item_acceptance`：验收完成，或创建新 Task 重开执行。

Agent 在读取这些候选的完整上下文前先领取 Coordination Claim，以避免多个 Agent 同时对“尚无具体 Task”的局面做冲突决策。

## 当前工作所需的执行上下文

Agent 获得的上下文只覆盖当前工作所需信息：

- Definition 与 WorkItem 意图；
- 当前 Task、验收要求和历史反馈；
- 相关上游成果与 Artifact；
- 当前模式中合法的决策与规划能力。

Workflow Context 只给出已配置的路径、optional、continue/exit 和 Review 决策。Blackboard Context 给出当前 Task Graph、建议 Relation 和可追加的规划空间。读取其他 Task 的完整上下文仍受 Role 和 Active Claim 限制。

## Claim、续租与受限凭据

Agent Claim 和 Coordination Claim 都是可续租 lease。Agent 应在服务端返回的 `lease_until` 前 heartbeat；只有 Core reaper 提交回收后，旧 Claim 才正式失去执行权。

Agent Daemon 为具体 Harness 使用 Claim-bound Executor Credential。该凭据只能读取绑定 WorkItem 内的必要上下文，并执行 Profile 允许的 Artifact 或 Blackboard 非终态写入。终态决定由 Daemon 根据类型化 outcome 调用 Core。

## Submit、Fail 或 Release：有意识地结束责任

- **submit**：创建 Submission，关联已暂存 Artifact，并提交当前模式允许的决策。
- **fail**：记录 Failure，明确请求重试、等待人工或结束 WorkItem。
- **release**：放弃当前责任且不伪造结果，Task 或协调候选重新开放。

如果操作返回 `work_item_cancelled`、fencing 失效、owner 不匹配或权威认证失效，Agent 必须停止续租和后续写入。网络超时不等于操作失败；可幂等资源创建应复用同一 `operation_id`，终态不确定时先核对历史。

## 模式能力：Agent 可以决定什么

| 能力 | Workflow | Blackboard |
| --- | --- | --- |
| 选择候选 | 是 | 是 |
| 改变工作结构 | 只提交预配置决策 | 可创建、拆分、追加、关联或跳过 Task |
| 请求 Review | 按 Definition 配置 | 可按当前成果请求 |
| 判断 WorkItem 完成 | 由流程结构收敛 | 需显式提交完成结果 |

## 主动 Agent 与 Agent Daemon

主动 Agent 自行调用 MCP/HTTP 完成循环。Agent Daemon 则绑定一个 Agent Identity，自动发现、领取并启动配置的 Harness。两者使用相同的 Core 协议；Daemon 的调度、Adapter 和崩溃边界见 [Agent Daemon 白皮书](agent-daemon.zh-CN.md)。

## MCP 接入面

Kairos 通过无状态 Streamable HTTP MCP 暴露执行闭环，并在 `.agents/skills/kairos-agent` 提供 Harness 执行纪律。MCP 只覆盖 Agent 执行所需的发现、上下文、Claim、Artifact、提交、失败和 Blackboard 规划；Definition、Identity 管理和人工 Review 决策不属于 Agent 接入面。

精确工具、参数和错误见 [API 参考](../api-reference.zh-CN.md) 与 [OpenAPI](../openapi.yaml)。
