# Kairos Agent Daemon

> Kairos 与外部 Agent Harness 之间的自动派发与执行控制架构

## 摘要

Kairos Agent Daemon 是绑定单一 Agent Identity 的长期运行进程。它发现工作、取得 Claim，
通过 Adapter 启动 Harness，并将 Harness 的类型化结果转化为 Core 中的持久状态。
Core 管理协作事实与执行责任，Adapter 管理具体运行，Claim 将二者连接起来。

## 1. 架构与职责

每个 Daemon 实例绑定一个 Agent Identity、一套 Harness Adapter 配置和最大 execution slots。
它使用普通的 Agent Identity Token；Core 从中解析 Actor 和 Role，Daemon 以同一身份完成发现、
Claim、heartbeat 和终态写入，Claim executor 记录该 Agent。

Core 负责候选资格、Task 与 Coordination Claim，以及 Task、WorkItem、Review、Failure、
Artifact 和 Workflow/Blackboard 的领域规则。Daemon 负责调度、续租、Harness 生命周期和结果
落地；Adapter/Harness 配置承载模型、Provider、进程、容器、远程服务及 workspace 隔离。

```text
                     Kairos Core
                 ▲ HTTP      ▲ scoped MCP
                 │           │
           Agent Daemon    Harness
                 │           ▲
                 └─ Adapter ─┘
                    Probe / Start / Observe / Stop
```

Daemon 通过 HTTP 控制执行，Harness 通过 MCP 动态读取上下文、管理 Artifact 和执行授权的
Blackboard 协作。Daemon 与 Core 的进程生命周期独立；外部进程启动不能与数据库事务组成
exactly-once，Core 状态正确性依赖 Claim、fencing 和终态 reconcile。

## 2. Dispatch 与生命周期所有权

Dispatch 将一个 Claim 与一次受控的 Harness 执行关联起来，分为两类：

- Task Dispatch：绑定 Task Claim，执行具体 Task。
- Coordination Dispatch：绑定 WorkItem Coordination Claim，处理 `empty_blackboard`、
  `blackboard_completion` 或 `work_item_acceptance` 判断。

两者共享控制循环：

```text
发现候选 → 生成 operation_id 与 Executor Token → Claim → 启动 heartbeat
→ Adapter 启动 Harness → 动态读取与执行 → 返回 outcome → Daemon 写入 Core
```

Daemon 独占 Claim 获取、heartbeat、release，以及 submit、fail、decompose、skip、Workflow
transition、Review 请求和 Blackboard completion/acceptance 等生命周期操作。Harness 直接调用
的能力由 Executor Credential 限定，终态意图通过 outcome 返回。WorkItem cancellation 仍是
Human-only 管理操作，Daemon 响应外部取消，而不替 Harness 发起取消。

Coordination Dispatch 复用现有 Core 操作；一个创建意图产生一个 Task。批量创建根 Task 和
Relation 属于独立 Blackboard 能力。

## 3. Executor Credential

### 签发与认证

Executor Credential 附着于 Claim。Daemon 在领取前生成带 `krs_claim_` 前缀的 256-bit 高熵
随机 Token；Core 在创建 Claim 的同一事务内只保存完整 Token 的 hash，Daemon 再通过 Adapter
的 secret 通道向 Harness 注入明文。普通 Agent 自行 Claim 时省略该可选 Token。

同一次 Claim 重试复用相同的 `operation_id + executor_token`，幂等记录无需保存凭证明文。
其他创建操作的 `operation_id` 由直接调用方生成并稳定复用：Daemon 管理自身的 Claim 和
终态创建操作，Harness 管理直接创建的 Artifact 与规划对象。

两类 Token 都使用 `Authorization: Bearer <token>`。只有完整符合 `krs_claim_` 前缀及
32 字节规范无填充 base64url 编码的 Token 进入 Claim 查询，其他 Token 进入 Identity 查询。
Claim 认证失败不回退 Identity 查询。Task Claim 与 Coordination Claim 共用前缀，
Core 对两表中带唯一索引的 `executor_token_hash` 联合查询，并从命中的 Claim 类型推导
Profile；异常命中多条时拒绝认证。

### 权限与生命周期

两个 Profile 共享 `scoped_read`，可读取绑定 WorkItem 内的 WorkItem context、Task context
及已提交 Artifact 的元数据和内容。`task_executor` 额外拥有当前 Task Claim 的
`task_artifact_write`，以及绑定 Blackboard WorkItem 内的 `blackboard_planning_write`：
创建 Task、Relation 和 Child Task。`coordination_executor` 只读，写入意图交给 Daemon。
其他操作默认拒绝；HTTP 与 MCP 在服务端执行同一 Profile/scope 约束，工具过滤不代替授权。

Task Harness 创建的 Blackboard 规划对象是共享事实，不随当前 Task 失败或 release 回滚。
Core 始终校验资源归属、模式、状态、层级、DAG、容量和幂等约束。

Role 用于 Identity Token 下的发现与 Claim 获取，`claim_task` 会再次校验 executor 和
`allowed_roles`。成功 Claim 证明本次执行资格；Executor principal 只包含 Claim Actor、
Profile 和 scope，不携带 Role、不查询当前 Identity，也不重复校验 `allowed_roles`。

Executor Token 的生命周期与 Claim 的实际 Active 状态一致，无独立 TTL、Role snapshot 或
Identity version 绑定。`lease_until` 只使 Claim 具备被 reaper 回收的资格；Claim 实际结束才
使 Token 失效。撤销或轮换 Agent Identity Token 不会级联撤销 Executor Token：即使 Harness
未能停止，其权限仍持续到 Claim 结束。

Executor Token mutation 必须在业务事务内重新校验 Claim Active。并发结束 Claim 时，Claim
先提交则 mutation 失败，mutation 先提交则结果有效。

## 4. Adapter 与 outcome

Adapter 是 Daemon 进程内针对具体 Harness 的运行时接口：

```go
type Adapter interface {
    Probe(context.Context) error
    Start(context.Context, StartRequest) (RunRef, error)
    Observe(context.Context, RunRef) (RunObservation, error)
    Stop(context.Context, RunRef, StopReason) error
}
```

- `Probe` 不创建运行，只检查 Harness/Provider 基本可用性；成功不保证后续 Start 成功。
- `Start` 注入 Executor Token、MCP 地址和执行上下文，成功返回有效 RunRef。失败必须在
  确认已终止本次可能启动的进程并清理资源后，返回空 RunRef 和 error。不得返回“失败但运行
  可能存在”的结果，因此 Daemon 可以在预算内安全重试。
- `Observe` 返回包含 RunState 的 RunObservation 快照；调用错误只表示暂时无法查询，不证明
  运行已经结束。
- `Stop` 是幂等、尽力而为的终止请求，不保证返回时 Harness 已停止。

Adapter 可选实现 RunForgetter，在 Dispatch 终态或替换已确认结束的运行前释放内存记录；
若 Adapter 尚未完成清理，则记住请求，待运行真正结束后自动回收；它不终止活动进程，也不
删除 workspace 文件。

RunRef 是不含 secret 的可序列化运行引用，可用于进程存活期间的观察和 reconcile。
按凭据返回的 MCP 工具和初始化指令指导上下文读取、授权操作及直接写入的 operation_id；
简短启动 prompt 与 outcome schema 定义托管执行结果，不再维护独立 Managed Skill。
Adapter 将 Harness 专用输出转换成 HarnessOutcome，分为 TaskOutcome 和 CoordinationDecision。

### TaskOutcome

除 `decomposed` 仅限 Blackboard 外，其余 Task outcome 均适用于 Workflow 和 Blackboard。

| Outcome | 内容 | Daemon 调用 |
| --- | --- | --- |
| `completed` | Result、Artifact IDs、Review 请求、可选 Workflow transition | `submit_task` |
| `decomposed` | 子 Task specs | `decompose_blackboard_task` |
| `retryable_failure` | 业务原因、可选 retry prompt | `fail_task(action=reopen)` |
| `terminal_failure` | 业务原因 | `fail_task(action=fail_work_item)` |
| `abandoned` | 可选 release reason | `release_claim` |

`completed.transition` 只允许用于 Workflow Task。长日志、补丁和交付文件存入 Artifact，
完成结果引用已有 Artifact IDs。

### CoordinationDecision

| Outcome | 合法 Candidate kind | Daemon 调用 |
| --- | --- | --- |
| `create_task` | 三类候选均可 | `create_blackboard_task` |
| `submit_completion` | `empty_blackboard`、`blackboard_completion` | `submit_blackboard_completion` |
| `accept_completion` | `work_item_acceptance` | `accept_blackboard_completion` |
| `abandoned` | 三类候选均可 | `release_coordination_claim` |

`create_task` 在空 Blackboard 创建初始 Task，在 completion 候选创建后续 Task，在 acceptance
候选创建后续 Task 并重新打开 WorkItem。`submit_completion` 携带完成结果；
`accept_completion` 和 `abandoned` 无额外字段。

两个 `abandoned` 由 Dispatch 绑定的 Claim 类型区分，Harness 不选择 Claim 类型。
不符合 mode、Candidate kind 或 schema 的 outcome 属于输出协议错误，Daemon 在调用 Core
前拒绝，不猜测替代意图。Core 保留最终领域校验。abandoned 的重复领取抑制见第 6 节。

## 5. 运行状态与终态收敛

### RunState 与 DispatchState

RunState 描述 Harness：`starting`、`running`、`outcome_ready`、`runtime_failed`、
`stopped`、`lost`。`outcome_ready` 要求 Harness 已结束并返回合法 outcome；结束但输出非法
属于 `runtime_failed`。`stopped` 表示确认主动停止，`lost` 表示无法观察或控制运行。

DispatchState 描述整个执行责任的收敛：

```text
prepared → claimed → starting → running → finalizing → finished
                       ↑          │
                       └── retry ─┘

starting / running / finalizing → stopping
stopping + Run 已确认结束 + Claim 已结束 → finished
stopping + RunState.lost + Claim 已结束   → lost
```

任何 RunState 都不能直接释放 execution slot。只有 Run 已结束或成为 lost，且 Core 已确认
Claim 结束，Dispatch 才进入 finished/lost 并释放 slot。`outcome_ready` 只使 Dispatch 进入
finalizing；Claim 成功后启动的独立 heartbeat guard 覆盖启动、观察、重试和 finalizing。

### 停止与运行时失败

每个 Dispatch 同时只有一个生命周期驱动者，独立续租、停止请求和只读快照可并发工作。
Scheduler 的首次领取完成后交接给 Run；内部 step、heartbeat 不作为独立公开驱动入口。
快照从候选、Core 确认的 Claim 和冻结意图派生，不重复维护这些事实。

网络超时、Kairos 不可达、5xx 或 heartbeat 响应丢失表示状态未知。Daemon 在 lease 安全窗口内
重试，到达安全停止点仍不能续租时请求 Stop，并等待 Core 恢复后 reconcile。Claim 已结束、
fencing、owner 不匹配、WorkItem 取消或 Identity Token 失效时，立即停止 heartbeat 和后续
终态 mutation，请求 Stop，进入 stopping。

停止结果必须经 Observe 确认；停止超时或失控按 RunState.lost 处理。Daemon 停止 heartbeat、
尽力请求 Stop，并在 Core 可达且凭证有效时请求释放对应 Claim。Core 不可达或释放结果未知时，
Dispatch 保持 stopping、占用 slot 并隔离运行环境；旧运行未确认结束时，不能在同一 Claim 下
启动替代 Harness。

模型不可用、Provider 鉴权/配额、上下文超限、进程崩溃、非法输出和 Adapter 故障不直接翻译成
Submission、Task Failure、WorkItem Failure 或 CoordinationDecision。确认旧运行已结束后，
Daemon 可在同一 Claim 内有限重试；runtime_failed 的重试使 Dispatch 回到 starting。
重试耗尽或主动停止后，Task Dispatch 以 `release_claim` 将 Task 返回 pending；
Coordination Dispatch 以 `release_coordination_claim` 使仍符合条件的生命周期候选重新可发现。
两者都遵守同一终态和 slot 释放条件。

### 终态 reconcile

Daemon 在写入前固定唯一终态意图。响应丢失后，通过 Task/WorkItem context、Claim 结束原因
和关联的 Submission、Failure、decomposition 或 Coordination 历史判断结果：

- Claim 仍 Active：重试相同意图。
- Claim 按预期原因结束且关联历史相符：原操作已提交。
- Claim 以其他原因结束：接受 Core 状态并停止写入。
- Context 不可读：保持待 reconcile，不假定成功。

创建资源和 decomposition 继续使用相同 operation_id 重放；finalizing 期间继续 heartbeat，
直到 Core 确认 Claim 已结束或权威报告凭证/执行权失效。

## 6. 调度与重复领取抑制

Task 与 Coordination Dispatch 共享实例的 execution slots，仅在有空闲 slot 时 Claim。
`find_work` 是按 kind 独立限制数量的候选快照，不是优先队列。Daemon 按以下循环选择：

```text
work_item_acceptance → blackboard_completion → task → empty_blackboard → …
```

每次成功 Claim 后移动内存游标，跳过空类，同类沿用 Core 返回顺序；多个空闲 slot 按同一
循环填充，Claim 冲突触发重新发现。这是本地选择策略，不建立 Task priority 或跨 Daemon
WorkItem fairness。系统性 Harness/Provider 故障暂停新 Claim；首次 Claim、健康暂停恢复和
cooldown 结束后再次领取前调用 Probe，失败采用有界退避和 jitter。
Start、Observe、Stop 的系统性故障均暂停新领取。明确未发送的领取重试同样经过健康门控；
已有不确定结果的领取继续使用原 operation_id 核对，不因暂停而放弃责任。

抑制状态绑定 Candidate 标识及当前代次，在 Claim 释放后继续保留：

- 基础设施重试耗尽：进入 cooldown；冷却结束、Probe 成功且跨 Claim 预算仍有剩余才重试。
  预算耗尽后 quarantine 当前代次。
- `abandoned`：表示本 Daemon 拒绝当前代次，release 后直接 quarantine，不算成功 Dispatch，
  也不清除已有失败记录。

新业务状态、执行上下文、计划或结果形成新候选代次时，旧 cooldown、预算和 quarantine 失效。
单纯 Claim 创建、heartbeat、release 或 reaper 回收不是新代次。健康恢复只解除全局暂停，
保留 cooldown、预算和 quarantine。quarantine 由业务代次变化或新建 Scheduler（通常为
Daemon 重启）解除，因此部分候选在 Harness 恢复后仍需人工重启。同代次非 abandoned 的
业务 outcome 成功写入 Core 后清除该候选的抑制记录。

这些状态仅保存在 Daemon 内存，不写入 Core，也不阻止其他 Daemon 领取。多个 Daemon 或反复
重启造成的 Claim churn 仍可能触发 Core 的 Claim 历史安全上限并终止 WorkItem。

## 7. 部署与崩溃边界

每个 Scheduler 只接受一次 Run，并拒绝并发或重复调用。取消表示停机，不是可恢复的暂停。
运行期间和有界停机窗口内仍核对未知 Claim；退出时未核对完的 Dispatch 保持非终态。
单次 Dispatch 引擎保留自身状态和核对能力，不受 Scheduler 的一次性生命周期限制。

MVP 使用内存 Dispatch 状态，不恢复旧 Dispatch。优雅停机尽力停止已知 Harness 并释放 Claim；
Daemon 崩溃后，进程内 Adapter 无法继续清理，内存 RunRef 也不能用于重连。MVP 不承诺终止
遗留 Harness、清理 workspace、恢复 finalizing intent 或撤销外部副作用。

heartbeat 停止后，Core reaper 最终结束 Claim 并使 Executor Token 失效，只保护 Kairos 内部
状态。每次 Dispatch 使用隔离 workspace；旧运行环境确认清理前不得复用，来源不明的旧
workspace 也不自动复用。systemd cgroup、容器或 Kubernetes Pod 可以提供额外进程清理，
但这属于部署环境的保证。

跨进程恢复需要持久 Dispatch journal、外部稳定的 RunRef、runtime registry 或 supervisor，
以及 Adapter 的 reattach/observe/stop 能力；journal 或可序列化 RunRef 本身不足以提供恢复。

日志与指标覆盖 Dispatch、Claim、heartbeat、安全窗口、Adapter 状态、耗时、结果和 reconcile；
排除 Token、Artifact 内容、完整模型输出和其他私有执行材料。

## 8. 实现分层

本地 Agent Daemon 覆盖 Task 与三类 Coordination Dispatch、Claim-attached Executor Credential、
独立 heartbeat、Probe/cooldown、内存调度与 Codex Adapter，并以 fake Adapter 状态机测试和
本地 E2E 示例验证。

后续持久运营层增加 journal、外部 runtime supervision 和重启 reconcile，按需扩展 Active
Claim 查询与 Blackboard 批量规划。远程 Adapter 再处理远程 secret 交付、运行隔离、取消传播、
网络分区、RunRef reconcile 和版本协商。

## 参考资料

- [Temporal Workers](https://docs.temporal.io/workers) 与
  [Temporal Activities](https://docs.temporal.io/activities)
- [Prefect Workers](https://docs.prefect.io/v3/concepts/workers)
- [Kubernetes Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
- [GitHub Actions self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/about-self-hosted-runners)
