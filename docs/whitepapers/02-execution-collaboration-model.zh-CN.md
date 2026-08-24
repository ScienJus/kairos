# Kairos 执行协作模型

> Task 责任、共享上下文与执行方式无关的协作设计

## 摘要

人和 Agent 可以围绕同一个 WorkItem，通过各自负责的 Task 协同推进完整工作。Task 的执行者类型限定谁有资格参与，`AllowedRoles` 只进一步限制 Agent Identity；符合条件的 Actor 通过 Claim 建立具体责任。未来的外部 Bridge 也可以自动完成同一套面向 Agent Role 的选择和 Claim。Task 生命周期变化与持久成果共同表达共享 WorkItem 的进展。

## 1. Task 是执行者的执行边界

WorkItem 是多个执行者共同推进的完整目标，Task 是单个执行者的执行边界。

```text
WorkItem：实现登录功能
├── Task：确认登录需求    → 人 A
├── Task：实现登录功能    → Agent B
└── Task：测试登录功能    → Agent C
```

一个 Task 应当描述一段完整、连贯且可交付的工作。正常情况下，它由一个执行者从开始执行到交付完成：

```text
确定 Task
    ↓
建立执行责任
    ↓
执行 Task
    ↓
提交成果
    ↓
完成 Task
```

因此：

> 一个 Task 对应一次完整、连贯的执行过程。

## 2. Claim

`Claim` 建立执行者与 Task 之间明确的责任关系：

```text
Agent ─┐
       ├──负责执行──→ Task
人 ────┘
```

Claim 具有两个基本性质：

- **明确性**：Kairos 能够确定当前由哪个执行者对 Task 的执行和交付负责；
- **唯一性**：同一个 Task 在同一时间只能存在一个有效 Claim。
- **Agent 可恢复性**：Agent Claim 是可续期的 lease，Agent 失联后执行责任可以被回收。

```text
Task A → 执行者 1    合法

Task A → 执行者 1
Task A → 执行者 2    不合法
```

唯一的执行责任可以避免重复工作与结果冲突，同时让生命周期变化和成果具有清晰的来源。

> Claim 表示 Task 的独占执行责任，与任务采用何种分发方式无关。

只有 Agent Claim 使用 lease。Agent 可以在领取 Task 和每次 heartbeat 时请求 lease 时长；服务端按策略限制并返回实际批准的 `lease_seconds` 与 `lease_until`。到达该时间只表示 Active Claim 可以被后台 reaper 回收，时间本身不会改变执行权。reaper 提交回收事务前，当前执行者仍可继续操作或续租，其他执行者不能 Claim 这个 Working Task。reaper 会以 `expired` 结束可回收 Claim 并将 Task 恢复为 Pending；只有此后新的执行者才能领取，旧 Claim ID 作为 fencing token，不能复活或继续提交结果。

Human Claim 不使用 lease 或 heartbeat。它持续有效，直到提交、失败、主动释放或管理员撤销，从而避免将基础设施存活机制暴露给人类交互。

Claim 只覆盖执行者正在处理 Task 的阶段。执行者提交 Task 进入人工 Review 时，当前 Claim 结束；等待 Review 期间 Task 不需要保活，也不能被新的执行者领取。Review 驳回后 Task 重新进入候选集合，由原执行者或其他执行者建立新的 Claim。

执行者无法完成 Task 时也会结束当前 Claim，并创建不可变的 Task Failure：

```text
reopen         → Task 回到 Pending
fail_work_item → Task 与 WorkItem 进入 Failed
```

`reopen` 可以携带 Retry Prompt。全部失败原因与 Retry Prompt 都保留在 Task 上下文中，新的执行者在后续 Claim 中继续处理。`fail_work_item` 停止产生和领取新 Task，其他 Active Claim 随 WorkItem 失败而结束。

## 3. 责任建立方式

Claim 与任务获取方式相互独立。`Executor` 限定允许参与的 Actor 类型，`AllowedRoles` 进一步限定允许参与的 Agent Identity；Human Identity 永远不受 `AllowedRoles` 筛选。它们选择的是一组有资格的执行者，Claim 才记录其中实际承担责任的具体 Actor。

| 参与方式 | 责任建立过程 |
| --- | --- |
| Agent 主动选择 | 符合 Role 的 Agent 查询候选 Task，自主选择并建立 Claim |
| 人工执行 | 人 Claim 执行者策略允许人工参与的 Task |
| 外部系统派发（规划中） | Bridge 选择符合 Role 的 Agent Identity，为其建立 Claim 并启动 Harness |

这些方式共享同一个概念过程：

```text
产生候选 Task
      ↓
确定执行者
      ↓
建立 Claim
      ↓
执行 Task
```

主动选择适合当前不控制 Agent Harness 的 Kairos。未来也可以通过 Bridge 在 Task 满足执行条件后启动 Codex、Claude Code 或其他 Agent Harness。

任务的组织方式与执行者的参与方式是两个独立维度：

| Task 组织方式 | 可采用的参与方式 |
| --- | --- |
| Workflow | 当前按 Role 主动 Claim；未来支持外部派发 |
| Blackboard | 当前按 Role 主动 Claim；未来支持外部派发 |

## 4. 共享工作上下文

Agent Harness 中的上下文通常是临时且局部的。Kairos 将协作信息归属于工作本身：

```text
WorkItem
├── 目标、背景、约束与验收标准
├── Task A
│   ├── 生命周期与执行责任
│   └── 交付成果
├── Task B
│   ├── 生命周期与执行责任
│   └── 交付成果
└── Task C
    ├── 生命周期与执行责任
    └── 交付成果
```

> Task 属于 WorkItem，Task 的生命周期与成果共同表达共享 WorkItem 的进展。

每次正式提交形成一条不可变的 Task Submission。Submission 关联产生它的 Claim，并保存该轮交付结果；后续返工产生新的 Submission，不覆盖此前成果。Review 直接关联被审核的 Submission，Failure 关联失败的 Claim，使全部提交、反馈和失败原因都能够追溯。

不同执行者通过各自 Task 的成果形成协作链条：

```text
人 A 执行 Task A：确认需求
          ↓ 共享成果
Agent B 执行 Task B：实现
          ↓ 共享成果
Agent C 执行 Task C：测试
```

每个执行者对自己的 Task 承担完整责任，并通过已有 Task 的成果理解前置工作。由此形成的共享上下文具有以下作用：

- 后续执行者可以理解已经完成的工作；
- 并行执行者可以了解 WorkItem 的最新态势；
- 人类可以观察各个 Task 对整体目标的贡献；
- Agent Harness 结束后，交付成果仍然保留在工作模型中。

## 5. Workflow 与 Blackboard

Workflow 和 Blackboard 使用相同的执行协作模型，区别集中在候选 Task 如何产生。

| 维度 | Workflow | Blackboard |
| --- | --- | --- |
| 候选 Task | 由正式 Task Graph 计算 | 根据共享 Task Graph 和当前上下文形成 |
| 前置关系 | 限定合法候选 | 提供推进建议 |
| 执行者 | 执行者类型限制全部参与者；allowed roles 只限制 Agent | 执行者类型限制全部参与者；allowed roles 只限制 Agent |
| 执行责任 | 通过唯一 Claim 建立 | 通过唯一 Claim 建立 |
| WorkItem 进展 | 由 Task 生命周期与持久成果表达 | 由 Task 生命周期与持久成果表达 |

Workflow 限定合法的选择空间。Blackboard 提供动态演化的工作结构和建议关系。两种模式都支持人或 Agent 主动选择，也都可以接入外部派发。

## 6. Kairos、Bridge 与 Agent Harness

Kairos 的核心协作语义适用于人和 Agent，并独立于 Agent 如何被运行。

```text
┌──────────────────────────────┐
│         Kairos Core          │
│ WorkItem / Task / Claim      │
│ Shared Context / Result      │
└───────────────┬──────────────┘
                │
          Integration / Bridge
                │
┌───────────────▼──────────────┐
│        Agent Harness         │
│ Codex / Claude Code / Others │
└──────────────────────────────┘
```

Kairos Core 表达工作、提供候选 Task、建立执行责任并保存共享上下文。人通过交互界面参与执行；Agent Harness 负责运行 Agent，规划中的 Bridge 将负责特定 Harness 的启动与结果回传。

这一协作模型可以归纳为五项原则：

1. 一次协作执行围绕明确的 Task 展开。
2. 一个 Task 在执行期间仅由一个执行者负责。
3. Claim 的建立方式不影响其责任语义。
4. Task 生命周期变化与成果共同表达共享 WorkItem 的进展。
5. Task 的组织方式与执行者的参与方式彼此独立。

> People and agents share a WorkItem, while each Task has one responsible executor.
> Kairos coordinates work independently of how that executor participates.
