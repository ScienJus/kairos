# Kairos Agent Daemon

Core 能判断哪些工作可用、谁拥有执行责任，但它有意不启动模型，也不监管本地进程。Agent Daemon 填补这段运行时空白，同时不把协调权威移出 Core。

Daemon 是绑定单一 Agent Identity 的长期运行进程。它发现候选、建立 Claim、通过 Adapter 启动 Harness，再把类型化结果变成 Core 中的持久事实。这里存在一条刻意划出的三方边界：Core 管理协作，Daemon 管理调度和运行收敛，Harness 只完成 scope 内的具体工作。

## 架构边界

```text
Kairos Core
  候选 / Claim / 生命周期 / 持久上下文
            ↑ Identity Token
Agent Daemon
  Scheduler / Dispatch / heartbeat / reconcile
            ↓ Executor Token
Adapter → Harness / Provider / process / workspace
```

每个 Daemon 实例绑定：

- 一个 Agent Identity 及 Role；
- 一个 Adapter 配置；
- 一个共享 execution slots 上限。

Core 不启动模型、不管理 workspace，也不通过遥测推断 Claim 状态。Daemon 不重新定义 Task、Review、Failure 或 Workflow/Blackboard 规则。

## 一个候选对应一个 Dispatch

Dispatch 是一个候选从领取到终态核对的完整运行记录：

```text
Dispatch
├── Task Dispatch → Task Claim
└── Coordination Dispatch → Coordination Claim
      ├── empty_blackboard
      ├── blackboard_completion
      └── work_item_acceptance
```

一个 Dispatch 固定候选身份、Claim、Harness 运行和唯一终态意图。一旦开始 finalizing，响应丢失也不能改为另一种业务结果。

领取响应不确定时，Dispatch 保留原 `operation_id` 继续核对，不创建第二个 Claim。Claim 成功后，独立 heartbeat guard 覆盖 Harness 启动、执行、重试和 finalizing。

## 只给 Harness 必要权限

Daemon 在创建 Claim 时同时生成高熵 Executor Token，Core 仅保存 hash。Token 绑定：

- Claim 与 Actor；
- `task_executor` 或 `coordination_executor` Profile；
- 当前 WorkItem scope。

Harness 可以读取 scope 内的必要上下文和已提交 Artifact。Task Harness 还可以创建 Artifact 与允许的 Blackboard 非终态规划对象。Harness 不能直接提交、失败、释放 Claim 或完成 WorkItem；Daemon 使用 Identity Token 翻译类型化 outcome。

Claim 结束、WorkItem 终态或 scope 不匹配时，Executor Token 立即失效。

## Adapter 边界

Adapter 是 Daemon 进程内的最小 Harness 运行时边界：

```go
type Adapter interface {
    Probe(context.Context) error
    Start(context.Context, StartRequest) (RunRef, error)
    Observe(context.Context, RunRef) (RunObservation, error)
    Stop(context.Context, RunRef, StopReason) error
}
```

- `Probe` 只检查基本可用性，不保证下一次 `Start` 成功。
- `Start` 只能返回有效 RunRef，或在确认本次运行不存在后返回错误。
- `Observe` 返回快照；查询错误不证明运行已结束。
- `Stop` 是幂等、尽力而为的请求，必须再通过 `Observe` 确认终止。

RunRef 不包含 secret，但 MVP 只在当前进程存活期间使用它。Adapter 可在运行终态后释放内存记录，不得把“忘记”当作“终止”。

## 把 Harness Outcome 变成业务事实

Task Harness 可返回：

| Outcome | Daemon 行为 |
| --- | --- |
| `completed` | `submit_task`，携带 result、Artifact IDs、Review 请求和合法 Workflow 决策 |
| `decomposed` | 仅 Blackboard；`decompose_blackboard_task` |
| `retryable_failure` | `fail_task(action=retry)` |
| `human_intervention_required` | 仅 Workflow；`fail_task(action=await_human)` |
| `work_item_failure` | `fail_task(action=fail_work_item)` |
| `candidate_declined` | `release_claim` |

Coordination Harness 可返回：

| Decision | 合法候选 | Daemon 行为 |
| --- | --- | --- |
| `create_task` | 三种候选 | `create_blackboard_task` |
| `submit_completion` | empty / completion | `submit_blackboard_completion` |
| `accept_completion` | acceptance | `accept_blackboard_completion` |
| `candidate_declined` | 三种候选 | `release_coordination_claim` |

Daemon 在调用 Core 前校验 schema、mode 与 candidate kind，不猜测非法输出的替代意图。长日志、补丁和文件应作为 Artifact，不塞入结果字段。

Provider 鉴权、配额、进程崩溃、非法输出和 Adapter 故障是运行时失败，不直接伪造为 Submission、Task Failure 或 WorkItem Failure。

## 运行收敛：为什么进程退出还不够

RunState 描述 Harness：`starting`、`running`、`outcome_ready`、`runtime_failed`、`stopped`、`lost`。DispatchState 描述整个执行责任：

```text
prepared → claimed → starting → running → finalizing → finished
                       ↑          │
                       └─ retry ──┘

starting / running / finalizing → stopping → finished | lost
```

RunState 结束不会直接释放 slot。只有 Harness 已终止或确认 lost，且 Core 已确认 Claim 结束，Dispatch 才能进入终态。

网络超时、5xx 或 heartbeat 响应丢失只表示状态未知。Daemon 在 lease 安全窗口内重试；无法续租时请求停止并等待 Core 恢复后核对。如果 Core 权威确认 Claim 已结束、fencing 失效、owner 不匹配或 WorkItem 取消，Daemon 立即停止续租和后续写入。

响应丢失后，Daemon 通过 Claim 结束原因以及 Submission、Failure、decomposition 或 WorkItem 历史核对唯一终态意图；证据不足时保持 finalizing，不假定成功或失败。

例如，Core 可能已经提交 Submission 并结束 Claim，但响应在网络中丢失。直接换一个意图重试可能造成重复工作；把超时当成失败，又会把 Core 已接受的结果误报为缺失。冻结的终态意图和持久历史让 Daemon 能确认已提交结果，而不必猜测。

## 调度与抑制：不依赖全局队列

Task 与 Coordination Dispatch 共享 slots。Daemon 在候选类型之间轮转，但 `find_work` 返回的顺序不是全局优先级，本地轮转也不建立跨 Daemon 公平性。

系统性 Adapter/Provider 故障会暂停新 Claim，并通过有界退避的 `Probe` 检查恢复。对单个候选代次：

- 基础设施重试耗尽后进入 cooldown，跨 Claim 预算耗尽后 quarantine；
- `candidate_declined` 释放 Claim 并直接 quarantine 当前代次；
- 新业务状态、计划或结果形成新代次，旧抑制记录失效；
- 健康恢复只解除全局暂停，不清空 cooldown、budget 或 quarantine。

抑制状态只存于 Daemon 内存，不阻止其他 Daemon 领取。

## 崩溃恢复：能恢复什么、不能恢复什么

MVP 使用内存 Dispatch，不在进程重启后恢复旧运行。优雅停机会尽力停止已知 Harness 并释放 Claim；崩溃后，Core reaper 最终回收未续租 Claim 并使 Executor Token 失效。

这只保护 Kairos 内部状态，不保证终止遗留进程、清理 workspace 或撤销外部副作用。每个 Dispatch 应使用隔离 workspace；不可信工作还需要独立 OS 用户、cgroup、容器或 Kubernetes Pod 提供额外保证。

真正的跨进程恢复需要持久 Dispatch journal、外部 runtime supervisor 和 Adapter reattach/observe/stop 能力；只持久化 RunRef 不足以安全恢复。

## 平台观测：观察但不控制工作

Daemon 使用 Agent Identity Token 向 Core 尽力上报实例快照和关键事件。遥测失败不改变调度、Claim、heartbeat 或 outcome 处理；平台上的状态只是运行上报，业务事实仍以 Core 记录为准。

实例连接状态、快照/事件契约、保留周期和控制台语义见[平台可观测性设计](../daemon-observability-design.zh-CN.md)。

## Agent Daemon 不变量

- Core 是候选、Claim 和业务终态的唯一权威。
- Harness 只使用 Claim scope 内的短期凭据。
- 运行时故障不伪造为业务失败。
- 旧 Harness 未确认终止时，不在同一 Claim 内启动替代运行。
- 只有 Harness 与 Claim 都收敛，Dispatch 才释放 slot。
- 遥测、日志和内存调度状态不参与 Core 业务正确性。
