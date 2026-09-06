# Agent Daemon 设计决策日志

> 本文只记录 Agent Daemon 设计讨论中已经确认的决策、明确延期的方案和待讨论问题。
> 它是设计工作记录，不是正式白皮书。实现进展见[分阶段规划](../agent-daemon-implementation-plan.zh-CN.md)。

## 状态

- 阶段：Core 执行凭据已验收
- 实现状态：阶段 1 已通过 SQLite 与 PostgreSQL 17.11 实测验收；Daemon 本体尚未开发
- 更新规则：只把双方已经确认的结论标记为“已确认”；未定事项保留在待讨论列表中

## 已确认决策

### D1：先完成设计，再重写文档，最后开发

Agent Daemon 按以下顺序推进：

1. 逐项讨论并确定完整设计；
2. 根据设计决策日志统一重写正式文档；
3. 文档确认后开始实现。

讨论期间不以临时代码验证替代尚未完成的设计决策。

### D2：Agent Daemon 派发 find_work 返回的全部候选

Agent Daemon 不是只派发可执行 Task，而是处理 Kairos `find_work` 返回的全部候选。Application
层使用 `WorkCandidate` 作为响应类型名，但它不作为产品叙述中的核心术语。Dispatch 分为两类：

```text
Dispatch
├── TaskDispatch
│   └── Task Claim
└── CoordinationDispatch
    └── Coordination Claim
        ├── empty_blackboard
        ├── blackboard_completion
        └── work_item_acceptance
```

两类 Dispatch 共享发现、Claim、启动 Harness、heartbeat、终态落地和 fencing
等外层控制过程，但使用不同的 Claim 类型和业务结果协议。

### D3：Agent Daemon 拥有 Dispatch 生命周期

采用 **Agent Daemon-managed lifecycle with Kairos-aware Harness**：

- Agent Daemon 负责发现、Claim、heartbeat、release 及所有会结束 Claim 的操作；
- Harness 理解 Kairos 上下文和业务语义，但不直接取得或结束 Claim；
- Harness 向 Agent Daemon 返回经过类型约束的业务意图；
- Agent Daemon 校验意图后，调用 Kairos Core 的正式操作完成状态变更；
- Kairos Core 继续作为 Task、WorkItem、Claim、Artifact 和 Blackboard 状态的唯一事实来源。

Harness 可以使用以后明确授权的非终态协作能力，但所有 Claim acquire、heartbeat、
release 和 claim-ending action 始终属于 Agent Daemon。

### D4：CoordinationDecision 是 Adapter 契约

`CoordinationDecision` 是 Harness 到 Agent Daemon 的类型化结果，不新增同名 Core API。
不同 Coordination Candidate 允许返回的意图为：

| Candidate | `create_task` | `submit_completion` | `accept_completion` | `abandoned` |
| --- | --- | --- | --- | --- |
| `empty_blackboard` | 允许 | 允许 | 禁止 | 允许 |
| `blackboard_completion` | 允许 | 允许 | 禁止 | 允许 |
| `work_item_acceptance` | 允许 | 禁止 | 允许 | 允许 |

`empty_blackboard` 的 `create_task` 创建初始 Task，`blackboard_completion` 的 `create_task`
创建后续 Task，`work_item_acceptance` 的 `create_task` 创建后续 Task 并由 Core 重新打开
WorkItem。

Harness 不直接调用会消费 Coordination Claim 的操作。Agent Daemon 将意图翻译为现有
Core 操作，并只翻译一次终态结果。Harness 返回非法或空意图时，Agent Daemon 不执行
含糊的状态变更，而是按后续确定的失败策略重试或释放 Claim。

### D5：Agent Daemon 复用现有 Coordination Claim 协议

现有 Coordination Claim 已经可以正常使用，并完整提供候选独占、lease、heartbeat、
release、原子落地和 fencing。Agent Daemon MVP 不为此新增 Agent Daemon 专用 API，也不要求新增
统一的 `apply_coordination_decision` Core 操作。

Agent Daemon 使用现有流程：

```text
find_work
→ claim_work_candidate
→ heartbeat_coordination_claim
→ create_blackboard_task
   / submit_blackboard_completion
   / accept_blackboard_completion
   / release_coordination_claim
```

现有业务操作与 Harness 意图的映射为：

| Harness 意图 | Agent Daemon 调用 |
| --- | --- |
| `CoordinationDecision.create_task` | `create_blackboard_task` |
| `CoordinationDecision.submit_completion` | `submit_blackboard_completion` |
| `CoordinationDecision.accept_completion` | `accept_blackboard_completion` |
| `CoordinationDecision.abandoned` | `release_coordination_claim` |

创建 Task、提交完成和 Agent 验收分别在各自业务事务内结束 Coordination Claim。

### D6：operation_id 由直接调用方管理

哪个组件直接调用要求 `operation_id` 的创建型 Core 操作，哪个组件就负责生成并稳定复用
该操作的 `operation_id`：

- Harness 只向 Agent Daemon 返回类型化意图、由 Agent Daemon 代为执行写操作时，`operation_id` 由
  Agent Daemon 生成，对 Harness 透明；
- Harness 通过 Executor Credential 直接调用 Artifact 等允许的写操作时，`operation_id`
  由 Harness 生成并负责正确重放；
- 同一个逻辑创建操作重试时必须复用相同 ID，参数发生变化时必须使用新 ID。

Agent Daemon 仍负责自己发起操作的幂等控制，但不接管 Harness 直接发起操作的请求标识。没有
`operation_id` 的生命周期操作如何在响应丢失后 reconcile，将在失败与恢复设计中单独确定。

### D7：原子批量 Blackboard 规划不阻塞 Agent Daemon MVP

Agent Daemon MVP 中，CoordinationDecision 的创建意图先映射为单个
`create_blackboard_task`。一次原子创建多个根 Task 和 Relation 是独立的 Blackboard
Core 能力，不是现有 Coordination Claim 正常工作或 Agent Daemon MVP 成立的前提。

以后可以单独设计 Core 扩展，例如批量 Task/Relation 校验、原子创建和幂等重放；该扩展
不与当前 Agent Daemon 设计和开发绑定。

### D8：Task Harness 可以直接执行受 scope 约束的非终态写操作

Task Dispatch 中，Agent Daemon 只独占 Claim 生命周期和会结束 Claim 的业务操作。Harness
可以通过 Executor Credential 直接调用当前执行所需的读取、Artifact 和 Blackboard
非终态协作操作。

Harness 可以直接调用：

- 绑定 WorkItem 内的 `get_work_item_context` 和 `get_task_context`；
- 读取绑定 WorkItem 内已提交 Artifact 的元数据和内容；
- `create_artifact`、`upload_artifact`；
- Blackboard 中的 `create_blackboard_task`、`add_blackboard_relation`、
  `add_blackboard_child_task`。

这些直接写操作使用 Harness 生成并稳定复用的 `operation_id`（操作本身要求时）。Artifact
必须绑定当前 Task Claim；Blackboard 规划操作必须限制在当前 Dispatch 绑定的 WorkItem，
并继续通过 Core 的模式、状态、层级、DAG、容量和幂等校验。

即使当前 Task 后来失败或被释放，Harness 已经成功提交的 Blackboard Task、Relation 或
Child Task 仍然作为共享规划事实保留，不随 Task Claim 回滚。这符合 Blackboard 的开放协作
语义；Task 结果与协作期间产生的计划变更是不同的持久事实。

Executor Credential 的上述能力只有在绑定的 Task Claim 仍然 Active 时有效。Claim 完成、
失败、释放、过期、被 fencing 或 WorkItem 取消后，Harness 不得继续读取或修改该 Dispatch
scope。所有资源引用必须由服务端校验属于绑定的 Task、Claim 或 WorkItem，不能只依靠 MCP
工具列表过滤。

Agent Daemon 继续独占：

- `claim_task`、`heartbeat_claim`、`release_claim`；
- `submit_task`、`fail_task`；
- `decompose_blackboard_task`；
- Workflow transition 和 Review 请求的最终落地；
- 其他会结束当前 Claim 或改变其他 Task 生命周期的操作，例如
  `skip_blackboard_task`。

Harness 通过类型化 `TaskOutcome` 向 Agent Daemon 返回完成、请求 Review、分解、可重试失败、
终止 WorkItem 或放弃当前执行等意图，并在完成结果中引用自己已经创建的 `artifact_ids`。
WorkItem cancellation 是 Human-only 管理操作，不属于 Harness outcome；外部取消 WorkItem 是
Agent Daemon 必须响应的 Core 状态。

### D9：Executor Credential 是 Claim 的附属认证材料

Executor Credential 不建模为独立领域资源，也不使用独立数据表。Task Claim 和
Coordination Claim 分别保存一个可空的 `executor_token_hash`；同一个 Claim 同时最多有
一个有效的 Executor Token。

Executor Token 使用统一的 `krs_claim_` 前缀，后接 256-bit 高熵随机值。HTTP 与 MCP 都通过
现有的 `Authorization: Bearer <token>` 传递凭证，不增加专用 Header。认证层按完整格式选择
解析路径：只有 `krs_claim_` 前缀后接 32 字节规范无填充 base64url 编码的凭证进入 Claim
credential 查询，其余凭证进入现有 Identity credential 查询。Claim 认证失败不回退 Identity
查询。格式仅用于路由，不参与授权判断；hash 覆盖完整 Token。

Task Claim 与 Coordination Claim 不使用不同的 Token 前缀。Core 通过两张 Claim 表上带唯一
索引的 `executor_token_hash` 执行一次 `UNION ALL` 查询，并由命中的 Claim 类型推导
`task_executor` 或 `coordination_executor` Profile。正常结果至多一条；若异常命中多条，认证
必须失败，不能任选一条。

Credential 的 Actor、固定权限 Profile 和资源 scope 均从 Claim 及其关联对象推导：

- Task Claim 派生 `task_executor` Profile，以及绑定的 Task、WorkItem 和 Claim scope；
- Coordination Claim 派生 `coordination_executor` Profile，以及绑定的 WorkItem、
  Candidate kind 和 Claim scope；
- 不提供用户自定义 RBAC 或任意权限列表。

这个模型形成三层互补授权：

```text
Agent Role          决定是否有资格发现并取得 Claim
Credential Profile  决定 Harness 可以调用哪些操作类别
Claim scope         决定这些操作可以作用于哪些资源
```

Executor Token 因而具有类似最小权限 RBAC 的效果，但本质上是由 Claim 派生的固定 scope
capability，而不是通用角色权限系统。两个 Profile 共享 `scoped_read`，可以读取绑定 WorkItem
内的 WorkItem context、Task context 和已提交 Artifact。`task_executor` 额外具有当前 Task
Claim 的 `task_artifact_write`，以及绑定 Blackboard WorkItem 内的
`blackboard_planning_write`；`coordination_executor` 没有直接写能力，只返回类型化判断交给
Agent Daemon 落地。其他操作默认拒绝。

Agent Role 只参与 Agent Identity Token 下的 `find_work` 和 Claim 获取。Core 在 `claim_task`
中重新校验 Task executor 与 `allowed_roles`，防止调用方绕过发现结果直接领取不符合资格的
Task。Claim 创建成功后即证明本次执行资格；Executor Token principal 只包含 Claim Actor、固定
Profile 和 Claim scope，不保存 Role snapshot、不查询当前 Identity，也不再次执行
`allowed_roles` 校验。实现时不能把 Executor Token principal 强行构造成要求 Agent Role 的普通
`Identity`。

Executor Token 的有效性与 Claim 的实际 Active 状态一致，不设置独立 TTL：

```text
token hash 匹配
AND Claim 仍然 Active
AND 操作属于固定 Profile
AND 请求资源属于 Claim 派生的 scope
```

`lease_until` 只表示 Claim 可以被 reaper 回收，不会自行结束 Claim。因此到达
`lease_until` 后、reaper 真正提交回收前，Executor Token 仍然有效；Claim 被完成、失败、
释放、回收、fencing 或因 WorkItem 取消而结束后，Token 立即无法再通过认证。

Executor Token 不绑定签发它时使用的 Agent Identity Token，也不保存 Identity version。
Agent Identity Token 撤销或轮换只影响 Agent Daemon 后续使用该凭证发起的请求，不使已经附着于
Active Claim 的 Executor Token 失效。需要停止 Harness 权限时必须结束对应 Claim；Agent Daemon
无法继续 heartbeat 后，Claim 也会在 lease 到期后由 reaper 回收。Agent Identity Token 撤销
不是 Executor Token 的即时撤销；Harness 未能停止时，Executor Token 仍有效到 Claim 实际结束。

所有 Executor Token mutation 都必须在业务事务内再次校验 Claim 仍然 Active。mutation 与
Claim 结束并发时，以事务提交顺序为准：Claim 先结束则 mutation 失败，mutation 先提交则其
结果有效。这一规则不需要 Agent Identity Token 撤销与 Claim mutation 之间的跨资源原子协议。

Executor Credential 与 Claim 在同一个事务中创建，不提供独立的签发接口。Agent Daemon
在发起 Claim 前生成 256-bit 高熵随机 Token，并在 Claim 请求中携带明文；Core 校验格式后
只把 hash 保存到新 Claim 的 `executor_token_hash`。Claim 成功后该 Token 才获得权限，Agent Daemon
直接通过 Adapter 的受保护环境变量或等价 secret 通道注入 Harness，Core 响应不必重复返回
明文。

同一次 Claim 重试必须复用相同的 `operation_id` 和 Executor Token。这样现有幂等记录只保存
Claim 响应和请求 hash，不会保存 bearer credential 明文。普通 Agent 自行 Claim 时省略该
可选字段，不产生 Executor Credential。

MVP 不提供独立轮换或撤销操作。Agent Daemon 需要停止 Harness 或撤销其权限时结束 Claim；以后
只有出现“保留同一 Claim 但替换 Harness”的明确需求时，才扩展轮换能力。

不保存 `executor_credential_issued_at`。当前没有 Token 年龄约束或运营查询需要该字段；
Credential 创建时间先记录在结构化日志中，未来出现明确的数据行为后再增加专用列。

### D10：Adapter 是 Agent Daemon 内部的最小 Harness 生命周期接口

Agent Daemon 负责一个 Agent Identity 的调度与生命周期控制；Adapter 是某一种具体 Harness
的运行时驱动。MVP 中 Adapter 是 Agent Daemon 进程内的 Go interface，不是独立服务、动态
插件系统或公开的跨语言协议。

Adapter 只需覆盖 Harness 的完整运行生命周期：

```go
type Adapter interface {
    Probe(ctx context.Context) error
    Start(ctx context.Context, request StartRequest) (RunRef, error)
    Observe(ctx context.Context, run RunRef) (RunObservation, error)
    Stop(ctx context.Context, run RunRef, reason StopReason) error
}
```

- `Probe` 在不创建运行的前提下检查 Harness 与 Provider 的基本可用性；
- `Start` 注入 Executor Token、Kairos MCP 地址和 Managed execution Skill，并启动 Harness；
- `Observe` 返回 Harness 当前状态，完成后返回类型化业务结果或运行时错误；
- `Stop` 是幂等、尽力而为的停止操作；
- `RunRef` 是不包含 Credential 的可序列化运行引用，可用于 Agent Daemon 日志和进程存活期间的
  reconcile。可序列化本身不表示 Agent Daemon 重启后能够重新连接运行。

`Start` 对 Agent Daemon 呈现原子语义：成功时返回有效 `RunRef`，Harness 即使已经快速结束也由
后续 `Observe` 报告；失败时只返回空 `RunRef`，Adapter 必须先终止本次调用可能启动的进程并
清理本次创建的资源。`error + 有效 RunRef` 和“返回错误但运行可能存在”都不属于合法结果。
因此 Agent Daemon 收到 `Start` 错误后可以在同一 Claim 内安全重试。

无法提供该原子保证的远程 Adapter 必须在内部使用幂等启动或 supervisor 消化不确定性；在满足
这一契约前不能直接接入 MVP Adapter 接口。

`Probe` 成功只表示可以尝试启动，不保证后续 `Start` 成功。Agent Daemon 在首次 Claim 前，以及
从健康暂停或 Candidate cooldown 恢复前调用它；失败时不取得 Claim，并使用有界退避与 jitter。

MVP 不在基础接口中预设 capabilities、版本协商、JSON stdin/stdout、HTTP 或 gRPC 等扩展。
第一个实现是 Agent Daemon 内部的 Codex Adapter；以后增加其他 Harness 时实现同一最小接口，
出现真实的跨进程或跨语言扩展需求后再设计公开协议。

Adapter 不发现或选择候选，不持有 Agent Identity Token，不执行 Claim、heartbeat、submit、
fail 或 release。Agent Daemon 负责这些 Kairos 控制操作、execution slots 和最终结果翻译；
Adapter 只负责启动、观察、停止 Harness，并把 Harness 专用输出转换成统一结果。

### D11：Adapter 使用快照式观察和类型化 Harness outcome

`StartRequest` 只携带一次 Dispatch 的必要执行信息：Dispatch kind、Agent identity/role、
WorkItem、可选 Task、Claim、Kairos MCP 地址、不可序列化的 Executor Token、Managed execution
Skill 和可选 deadline。Adapter 专用配置保存在 Adapter 实例中，不重复放入每个请求。

`RunRef` 是 Adapter 管理的 opaque ID。`Observe` 返回即时快照，不长期阻塞；统一运行状态为：

```text
starting
running
outcome_ready
runtime_failed
stopped
lost
```

- `Observe` 返回调用错误表示当前无法查询，运行状态仍未知，Agent Daemon 可以重试；
- `runtime_failed` 表示 Adapter 已确认 Harness 异常终止；
- `lost` 表示 Adapter 已确认无法继续观察或控制该运行；
- `outcome_ready` 表示 Harness 已结束并返回合法、类型化的 `HarnessOutcome`；Harness 已结束但
  outcome 非法时返回 `runtime_failed`；
- `Stop` 只发送幂等、尽力而为的终止请求，Agent Daemon 继续观察直到 `stopped`、`lost` 或达到
  后续确定的停止超时。

`RunState.outcome_ready` 与 `DispatchState.finished` 是不同的线性化点。前者使 Dispatch 进入
`finalizing`，Agent Daemon 在此期间继续 heartbeat；只有 Core 确认 claim-ending 操作成功，
或权威确认 Claim 已失效后，Dispatch 才进入 `finished`。

任何 RunState 都不能直接让 Dispatch 进入终态或释放 execution slot。`runtime_failed` 在允许
重试时使 Dispatch 回到 `starting`；`stopped` 使 Dispatch 保持 `stopping` 并等待 Claim 结束；
`lost` 只表示 Harness 已无法观察或控制，Dispatch 同样保持 `stopping`。只有 Run 已确认结束或
成为 `lost`，且 Core 已确认 Claim 结束后，Dispatch 才进入 `finished` 或 `lost` 并释放 slot。

类型化结果分为：

```text
HarnessOutcome
├── TaskOutcome
│   ├── completed
│   ├── decomposed
│   ├── retryable_failure
│   ├── terminal_failure
│   └── abandoned
└── CoordinationDecision
    ├── create_task
    ├── submit_completion
    ├── accept_completion
    └── abandoned
```

Task `completed` 携带 Result、Artifact IDs、是否请求 Review 和可选 Workflow transition；
`decomposed` 携带子 Task specs。每一种结果到 Core 操作的映射固定且唯一：

| Task outcome | Workflow | Blackboard |
| --- | --- | --- |
| `completed` | 允许 | 允许 |
| `decomposed` | 禁止 | 允许 |
| `retryable_failure` | 允许 | 允许 |
| `terminal_failure` | 允许 | 允许 |
| `abandoned` | 允许 | 允许 |

`TaskOutcome.completed.transition` 只允许用于 Workflow Task；Blackboard Task 的完成结果不得
携带该字段。CoordinationDecision 的合法范围遵循 D4 的 Candidate 矩阵。

| 完整 outcome | 字段 | Agent Daemon 调用 |
| --- | --- | --- |
| `TaskOutcome.completed` | Result、Artifact IDs、是否请求 Review、可选 Workflow transition | `submit_task` |
| `TaskOutcome.decomposed` | 子 Task specs | `decompose_blackboard_task` |
| `TaskOutcome.retryable_failure` | failure reason、可选 retry prompt | `fail_task(action=reopen)` |
| `TaskOutcome.terminal_failure` | failure reason | `fail_task(action=fail_work_item)` |
| `TaskOutcome.abandoned` | 可选 release reason | `release_claim` |
| `CoordinationDecision.create_task` | Task spec | `create_blackboard_task` |
| `CoordinationDecision.submit_completion` | completion result | `submit_blackboard_completion` |
| `CoordinationDecision.accept_completion` | 无额外字段 | `accept_blackboard_completion` |
| `CoordinationDecision.abandoned` | 无额外字段 | `release_coordination_claim` |

两个 `abandoned` 由外层结果类型和当前 Dispatch 绑定的 Claim 类型区分，Harness 不传入或选择
Claim 类型。Agent Daemon 内部可以统一处理“释放当前 Claim”，但调用 Core 时必须选择对应的
Task 或 Coordination release 操作。

`abandoned` 表示当前 Agent Daemon 拒绝处理当前 Candidate 代次。Agent Daemon 释放对应 Claim
后，直接在本地 quarantine 该代次，不再重新领取。按 2026-09-06 收窄后的 MVP 契约，Candidate
形成新业务代次或新建 Scheduler（通常为重启）后，旧 quarantine 失效；健康恢复不解除
quarantine，其他 Agent Daemon 不受影响。
`abandoned` 不是成功 Dispatch，也不能清除已有的基础设施失败抑制状态。

Agent Daemon 负责校验结果并调用 Core。`retryable_failure` 和 `terminal_failure` 只表示 Harness
明确报告的业务失败；Adapter 运行时错误、输出协议错误或 `lost` 不是业务 Task Failure，
Adapter 不得自行把它们翻译为 submit、fail 或 release。

Adapter 返回不符合 Dispatch mode 或 Candidate kind 的 outcome 属于输出协议错误。Agent Daemon
必须在调用 Core 前拒绝该结果，不能猜测替代意图，并按基础设施故障策略重试或释放对应 Claim。
Core 仍执行最终的模式、状态和 Claim 校验。

### D12：Harness 基础设施故障不得成为业务失败

模型不可用或不支持、Provider 鉴权/限流/配额问题、Harness 进程崩溃、上下文超限、输出协议
错误、Adapter 启动或观察失败等都属于 Dispatch/Harness 基础设施故障，不表示 Task 本身执行
失败。

这类故障不得产生 Task Submission、Task Failure、WorkItem Failure、decomposition 或
CoordinationDecision。Agent Daemon 可以在确认旧 Harness 已停止后，在同一 Claim 内进行有限的
运行时重试。重试耗尽后：

- Task Dispatch 调用 `release_claim`，让 Task 从 `working` 恢复为 `pending`；
- Coordination Dispatch 调用 `release_coordination_claim`；如果 WorkItem 仍满足原候选条件，
  对应的 `empty_blackboard`、`blackboard_completion` 或 `work_item_acceptance` 将重新可发现。

随后 Agent Daemon 暂停新的 Claim，并按运行时健康策略探测恢复。

释放 Claim 后，Agent Daemon 还必须在内存中记录跨 Dispatch 的 Candidate 失败状态。失败状态
绑定 Task 或 WorkItem 候选标识及其当前候选代次；cooldown 期间调度器跳过该代次，cooldown
结束、`Probe` 成功且跨 Claim 重试预算仍有剩余时才允许重新领取。预算耗尽后该代次在当前
Agent Daemon 内保持 quarantined，直到业务代次变化或新建 Scheduler。健康恢复只解除全局
暂停，不清空 cooldown 或失败预算；非 abandoned 业务 outcome 成功写入后清除该候选记录。

Task 或 WorkItem 的业务状态、执行上下文、计划或结果发生足以形成新候选的变化时，旧
cooldown、预算和 quarantine 自动失效。Claim 创建、heartbeat、release 或 reaper 回收本身不
构成新候选代次，不能因此重置失败预算。只有非 `abandoned` 的业务 outcome 成功写入 Core，
才清除当前代次的抑制记录。具体代次 fingerprint 留给实现确定。

该机制不持久化、不写入 Core，也不阻止其他 Agent Daemon 领取同一 Candidate。Agent Daemon
不会主动把基础设施故障翻译成业务 Failure，但多个 Daemon 或反复重启造成的 Claim churn 仍可能
触发 Core 的 Claim 历史安全上限并终止 WorkItem。

这里的 release 是清理本次执行责任，不是 Task 业务失败。它不会创建 Failure，也不会把
Harness 故障原因写成 Task 执行结果。只有 Harness 返回合法、明确的业务 failure outcome，
并通过 Agent Daemon 策略和 Core 校验后，才可以调用 `fail_task`。

### D13：只有确认旧 Harness 终止后才能在同一 Claim 内重试

Adapter 明确报告 `runtime_failed` 或 `stopped` 时，表示旧 Harness 已经终止。Agent Daemon 可以继续
heartbeat，并在有限重试策略内使用同一个 Claim 和 Executor Token 重新启动 Harness。

Adapter 报告 `lost` 时，Agent Daemon 无法确认旧 Harness 是否仍在运行，因此不得在同一 Claim 下
启动第二个 Harness。Agent Daemon 停止 heartbeat 并尽力停止旧运行；Kairos 可达时请求 release
Task Claim 或 Coordination Claim，使旧 Executor Token 失效。`RunState.lost` 不直接产生终态
`DispatchState.lost`；Dispatch 保持 `stopping` 并占用 slot，直到 Core 确认 Claim 已 release、
被 reaper 回收或以其他方式结束。此后 Dispatch 才进入 `lost`，后续 Dispatch 必须取得新 Claim
并使用新 Token。Kairos 不可达时不能假装 release 成功，只能隔离环境并等待 Core 恢复后
reconcile 或由 reaper 回收 Claim。

`lost` RunRef 所使用的进程、容器或 workspace 在确认清理完成前不得复用。Claim scope 只能
阻止旧 Harness 继续操作 Kairos，不能阻止它修改本地文件或调用外部系统；运行环境隔离和
清理由 Adapter 负责，不能伪装成 Kairos Core 的能力。

### D14：Heartbeat 失败分为暂时未知和权威失效

网络超时、Kairos 暂时不可达、`5xx` 或 heartbeat 响应丢失只表示 Claim 状态暂时未知，
不能直接判定执行权已经丢失。Agent Daemon 在安全窗口内继续观察 Harness 并重试 heartbeat；成功
续租后继续 Dispatch，到达安全停止点仍无法续租时请求停止 Harness。

安全停止点必须为 Adapter 停止保留余量，并使用本地单调时钟从最近一次 Claim/heartbeat
请求开始时间和 Core 返回的 `lease_seconds` 保守计算，而不是依赖 Agent Daemon 与 Kairos 的墙上
时钟完全同步：

```text
safe_deadline =
    request_started_monotonic
    + granted_lease_seconds
    - adapter_stop_grace
```

Core 明确返回 Claim 已结束、owner 不匹配、fencing/conflict、WorkItem 已取消，或 Agent
Identity Token 已失效时，属于权威失效。Agent Daemon 立即停止 heartbeat，不再调用 submit、
fail、release 或 decompose，也不产生 Task Failure；它通过 `Adapter.Stop` 请求终止 Harness，
并进入 `stopping`。只有后续观察确认运行已结束，才能认为 Harness 已停止；停止超时或失去
控制时进入 `RunState.lost`，Dispatch 按 D13 保持 `stopping` 和运行环境隔离，直到 Core 已确认
Claim 结束后才进入终态 `DispatchState.lost`。

Kairos 持续不可达并到达安全停止点时，Agent Daemon 请求停止 Harness，但不能假装 Claim 已
release，也不能启动替代 Harness。Dispatch 保持待 reconcile；Kairos 恢复后，若 Claim 仍 Active 且旧
Harness 已停止则 release，若 Claim 已结束则关闭本地 Dispatch，状态仍未知时继续隔离。

Agent Daemon 在 `finalizing` 阶段继续 heartbeat。只有 Core 确认 claim-ending 操作成功，或权威报告
Claim 已失效，Agent Daemon 才停止续租。

### D15：一个 Agent Daemon 实例绑定一个 Agent Identity

Agent Daemon 是一个 Agent 的托管运行形态。每个实例只配置一个普通的 Agent Identity Token，
Core 通过该 Token 解析 Agent Identity 和 Role；Agent Daemon 使用同一身份执行 `find_work`、
Claim、heartbeat 和最终生命周期操作。Claim executor 直接记录该 Agent，不建立 Agent Daemon
Identity、delegation 或 act-as 协议。

Tag 是 Agent Daemon 调用 `find_work` 时使用的筛选配置，不属于 Identity Credential。需要不同
Role、Token 或 Harness 配置时运行多个 Agent Daemon 实例。`daemon_id` 只用于日志和指标，
不进入领域授权。

### D16：MVP 不提供 Agent Daemon 崩溃后的运行恢复

Agent Daemon MVP 保持轻量，只在内存中保存 Dispatch 状态，不引入持久 journal、runtime
registry、supervisor 或 sidecar。优雅停机时，Agent Daemon 尽力停止已知 Harness，并 release
其 Active Claim；这些动作不构成进程突然退出时的保证。

Agent Daemon 进程崩溃后，进程内 Adapter 无法继续执行清理，内存中的 Dispatch 和 RunRef 也
无法用于重连。MVP 不承诺终止遗留 Harness、清理 workspace、恢复 finalizing intent 或撤销
外部副作用。heartbeat 停止后，Kairos reaper 最终结束 Claim 并使 Executor Token 失效；这只
保护 Kairos 内部状态，不能控制 Kairos 之外的进程和副作用。

重启后的 Agent Daemon 不恢复旧 Dispatch，也不根据不完整的本地信息猜测其终态。每个 Dispatch
使用隔离 workspace，来源不明的旧 workspace 不得自动复用。systemd cgroup、容器或 Kubernetes
Pod 可以在部署层提供进程清理，但不属于 Agent Daemon 协议保证。

以后若需要跨 Agent Daemon 进程恢复，必须同时引入不含 secret 的持久 Dispatch journal、外部
稳定的 RunRef、持久 runtime registry 或 supervisor，以及 Adapter 的 reattach、observe 和
stop 能力。只有 journal 或可序列化 RunRef 不足以提供该保证。

### D17：MVP 调度属于 Agent Daemon 本地策略

Kairos Core 没有 Task priority 概念，`find_work` 返回的是按候选类型分别限制数量的发现快照，
不是全局优先队列或公平调度接口。Agent Daemon 不把 Core 当前的结果拼接顺序解释为领域优先级，
MVP 也不扩展 Core 的排序、分页或调度 API。

Agent Daemon 将每次 `find_work` 结果按 kind 分组，并使用一个内存游标按以下固定顺序循环选择：

```text
work_item_acceptance
→ blackboard_completion
→ task
→ empty_blackboard
→ work_item_acceptance
```

每次成功取得一个 Claim 后，游标移动到下一类；当前类没有候选时直接跳过。多个空闲 execution
slot 按同一循环依次填充。Claim 冲突后重新调用 `find_work`，不继续依赖旧候选快照。同一 kind
内部沿用 Core 返回顺序。

该策略只避免单个 Agent Daemon 因固定类别顺序导致永久饥饿，不承诺完整候选集内的 WorkItem
fairness，也不在多个 Agent Daemon 之间协调公平状态。进程重启后游标从
`work_item_acceptance` 重新开始。以后若出现真实的跨实例公平或业务优先级需求，再独立扩展
Core 语义。

### D18：MVP 健康恢复不重置候选，Scheduler 只运行一次（2026-09-06）

健康恢复仅解除全局暂停，不清空候选的 cooldown、预算或 quarantine。quarantine 由业务
代次变化或新建 Scheduler（通常为进程重启）解除；代价是部分候选恢复后需要人工重启。
不再维护健康 epoch、Dispatch 所属健康 epoch 或相应终态写回条件；保留 healthSerial，防止
旧 Probe 的成功覆盖较新的系统故障。这取代此前健康变化自动清理候选记录的策略。

每个 Scheduler 只接受一次 Run，并发调用或返回后的再次调用均立即报错。取消表示停机，
不承诺在同一 Scheduler 上恢复；运行期间保留未知 Claim 核对和占用 slot，有界停机结束时
未解决的 Dispatch 不得被标记为已完成。独立 Dispatch 引擎仍保留自身状态与核对能力。

### D19：Dispatch 单一状态来源与单驱动接口（2026-09-06）

Snapshot 只作为读视图：候选来自 candidate，Claim ID 和终止信息来自 Core 确认的 claim，
outcome kind 来自冻结 intent；不再存储一份重复的 Snapshot。OutcomeApplied 是独立的
确认结论，不因本轮重构删除，也不降低丢失响应后的精确业务核对承诺。

Run 是公开生命周期驱动入口，拒绝并发驱动，但允许在前次 Run 返回后保留同一 Dispatch
顺序恢复。Scheduler 的内部首次领取与 Run 共用驱动占用标志；step、heartbeat 收为内部
方法，取消 stepMu 和 runMu。启动前确认与后台 guard 的续租调用仍可能重叠，保留续租
串行锁及共享状态锁；RequestStop 和 Snapshot 可并发调用。

## 明确未采用或延期的方案

### 新增 apply_coordination_decision Core API

当前不新增统一的 `apply_coordination_decision`。这个设想曾用于承载原子批量规划和统一
幂等结果，但现有 Coordination Claim 与业务操作已经能够支持 Agent Daemon。若未来确实需要
原子批量 Blackboard 规划，应作为独立 Core 领域能力重新设计，而不是以 Agent Daemon 集成需求
为由直接扩张 Core API。

## 待讨论问题

以下内容尚未定案：

1. Dispatch 状态机，以及 Claim、Harness 与 Kairos 各类失败的重试、停止和 reconcile 规则；
2. 进程存活期间的响应丢失、终态不确定和 fencing 后的恢复；
3. Execution slot、容量和路由配置；
4. 配置模型、部署边界、日志、指标和敏感信息保护；
5. Agent Daemon MVP 的准确范围和后续阶段划分。
