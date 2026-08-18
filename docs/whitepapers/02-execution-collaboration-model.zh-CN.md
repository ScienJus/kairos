# Kairos 执行协作模型

> Task 责任、共享上下文与执行方式无关的协作设计

## 摘要

人和 Agent 可以围绕同一个 WorkItem，通过各自负责的 Task 协同推进完整工作。Task 可以被主动领取、由人指定或通过外部 Bridge 派发。所有方式都需要在执行前建立明确且唯一的责任，并将进展与成果沉淀到共享工作模型中。

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
执行并记录进展
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

唯一的执行责任可以避免重复工作与结果冲突，同时让进展和成果具有清晰的来源。

> Claim 表示 Task 的独占执行责任，与任务采用何种分发方式无关。

只有 Agent Claim 使用 lease。Agent 可以在领取 Task 和每次 heartbeat 时请求 lease 时长；服务端按策略限制并返回实际批准的 `lease_seconds` 与 `lease_until`。如果 `lease_until` 前没有收到 heartbeat，Kairos 会以 `expired` 结束 Claim、将 Task 恢复为 Pending，并允许新的执行者领取。旧 Claim ID 作为 fencing token，过期后不能复活，也不能继续提交结果。

Human Claim 不使用 lease 或 heartbeat。它持续有效，直到提交、失败、主动释放或管理员撤销，从而避免将基础设施存活机制暴露给人类交互。

Claim 只覆盖执行者正在处理 Task 的阶段。执行者提交 Task 进入人工 Review 时，当前 Claim 结束；等待 Review 期间 Task 不需要保活，也不能被新的执行者领取。Review 驳回后 Task 重新进入候选集合，由原执行者或其他执行者建立新的 Claim。

执行者无法完成 Task 时也会结束当前 Claim，并创建不可变的 Task Failure：

```text
reopen         → Task 回到 Pending
fail_work_item → Task 与 WorkItem 进入 Failed
```

`reopen` 可以携带 Retry Prompt。全部失败原因与 Retry Prompt 都保留在 Task 上下文中，新的执行者在后续 Claim 中继续处理。`fail_work_item` 停止产生和领取新 Task，其他 Active Claim 随 WorkItem 失败而结束。

## 3. 责任建立方式

Claim 与任务获取方式相互独立。Task 的执行者可以通过多种方式产生：

| 参与方式 | 责任建立过程 |
| --- | --- |
| Agent 主动选择 | Agent 查询候选 Task，自主选择并建立 Claim |
| 外部系统派发 | Bridge 选择执行者、建立 Claim 并启动 Agent |
| 人工执行 | 人主动领取 Task，或者由其他人指定 |

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

Agent 主动选择适合当前不控制 Agent Harness 的 Kairos。未来也可以通过 Bridge 在 Task 满足执行条件后启动 Codex、Claude Code 或其他 Agent Harness。

任务的组织方式与执行者的参与方式是两个独立维度：

| Task 组织方式 | 可采用的参与方式 |
| --- | --- |
| Workflow | 主动领取、外部派发或人工指定 |
| Blackboard | 主动领取、外部派发或人工指定 |

## 4. 共享工作上下文

Agent Harness 中的上下文通常是临时且局部的。Kairos 将协作信息归属于工作本身：

```text
WorkItem
├── 目标、背景、约束与验收标准
├── Task A
│   ├── 执行进展
│   └── 交付成果
├── Task B
│   ├── 执行进展
│   └── 交付成果
└── Task C
    ├── 执行进展
    └── 交付成果
```

> Task 属于 WorkItem，Task 的进展和成果也应进入共享工作模型。

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
| 执行者 | 主动领取、Bridge 派发或人工指定 | 主动领取、Bridge 派发或人工指定 |
| 执行责任 | 通过唯一 Claim 建立 | 通过唯一 Claim 建立 |
| 进展与成果 | 沉淀到 Task 和 WorkItem | 沉淀到 Task 和 WorkItem |

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

Kairos Core 表达工作、提供候选 Task、建立执行责任并保存共享上下文。人通过交互界面参与执行；Agent Harness 负责运行 Agent，Bridge 负责特定 Harness 的启动与结果回传。

这一协作模型可以归纳为五项原则：

1. 一次协作执行围绕明确的 Task 展开。
2. 一个 Task 在执行期间仅由一个执行者负责。
3. Claim 的建立方式不影响其责任语义。
4. Task 的进展和成果进入共享工作模型。
5. Task 的组织方式与执行者的参与方式彼此独立。

> People and agents share a WorkItem, while each Task has one responsible executor.
> Kairos coordinates work independently of how that executor participates.
