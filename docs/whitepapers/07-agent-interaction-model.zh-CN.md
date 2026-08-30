# Kairos Agent 交互模型

> Agent 参与 Workflow 与 Blackboard 的统一交互方式

## 摘要

Kairos 为 Agent 提供统一的 Task 交互过程。当前 Agent 可以主动发现和选择工作；未来也可以由 Bridge 根据 Role 选择 Task 并启动 Agent。两种方式都围绕同一个 Task 建立执行责任、读取工作上下文、执行并提交持久成果，其生命周期影响共同构成 WorkItem 进展。

Workflow 与 Blackboard 共享执行过程，同时向 Agent 开放不同的规划能力。Workflow 允许 Agent 在预先配置的位置作出判断，Blackboard 允许 Agent 持续调整 Task Graph。

## 1. 交互过程

Agent 通过两种方式进入执行过程：

```text
主动参与：发现候选 → 选择 Task ─┐
                                  ├→ 建立 Claim → 执行 Task
Bridge 派发：接收 Task ───────────┘
```

完整过程可以概括为：

```text
discover / receive
        ↓
inspect
        ↓
claim
        ↓
execute
        ↓
heartbeat while executing
        ↓
submit result
```

Agent 在执行前读取必要上下文并确认 Task。Agent 在执行开始前建立带 lease 的 Claim，形成唯一执行责任；未来的 Bridge 也会为所选 Agent Identity 建立同样的 Claim。执行期间 Agent 通过 heartbeat 续租，并可以为每一段续租请求不同的时长。Claim 与 Task 状态表示工作正在执行，Submission、Review、Failure、推进决策和 Artifact 则持久描述它对 WorkItem 进展的贡献。到达 `lease_until` 只表示 Claim 可以被 reaper 回收，并不会直接撤销执行权；reaper 提交回收前，当前 Agent 仍可续租或提交。回收完成后 Agent 必须停止，不能复活旧 Claim 或继续提交。

Blackboard 生命周期判断使用并行的 WorkItem Coordination Claim。Agent 在读取完整上下文并判断 `empty_blackboard`、`blackboard_completion` 或 `work_item_acceptance` 候选前先领取它；创建所选 Task、提交完成或接受完成时携带该 Claim ID，并在同一事务内结束 Claim。这保护了尚不存在可执行 Task 时的分析窗口。Coordination Claim 与 Agent Task Claim 使用相同的 lease、heartbeat、reaper 回收和 fencing 规则。

## 2. 发现工作

Agent 只发现允许 Agent 执行的 Task：

```text
executor = agent | either
+ role matched
```

候选 Task 的来源由协调模式决定：

| 模式 | 候选 Task |
| --- | --- |
| Workflow | 前置关系已满足的 required Task，以及决定保留的 optional Task；候选由 role 与图状态决定，不由 tags 过滤 |
| Blackboard | 符合 tags 和查询上下文的 Task |

候选结果提供足够的信息帮助 Agent 比较工作，包括 WorkItem 摘要、Task 目标、协调模式、tags 和当前可执行原因。Agent 可以在建立 Task Claim 前读取 Task 上下文；对于 Blackboard 生命周期候选，必须先建立 Coordination Claim，Active Coordination Claim 会让该 WorkItem 从其他发现查询中隐藏。

空 Blackboard 直接以 WorkItem 作为候选。Agent 先领取候选，再读取目标与全局说明，并携带 Coordination Claim ID 创建首个 Task；后续发现回到通常的 Task 候选。

## 3. Task 上下文

Agent 执行 Task 时获得五类信息：

```text
Definition Context
    Description、Agent Instructions 与 Suggested Tags

WorkItem Context
    目标、背景、约束与验收标准

Task Context
    Task 描述、交付要求、生命周期与已有记录

Related Results
    相关 Task 的成果与 Artifact

Coordination Context
    当前模式、Task Relation 与可用决策
```

Definition Context 对同一协作空间中的全部 WorkItem 生效。Workflow 的 Coordination Context 包含正式前置关系、optional 判断和 Review 配置。Blackboard 则包含建议关系、tags 和当前共享工作态势。

Agent 可以按需读取更多历史和成果。默认上下文应优先提供与当前 Task 直接相关的信息。

Workflow Context 提供按距离排列的受控上游运行时 Task 摘要（包括 durable result）、当前合法的 Choice Group、直接目标、Relation 的可选 Label 与 Agent Guidance，以及本次可判断的 optional Task。Relation Guidance 帮助 Agent 理解已有的推进选择，但不会创建新的条件分支。Agent 提交需要跳过的 Task ID，Kairos 根据 Workflow Definition 负责关系分区和路径展开。Blackboard Context 提供当前共享的 Task 与建议关系，并支持 tags 筛选。直接读取其他 Task 的完整上下文仍受目标 Task 的 role 与 active Claim 限制。

## 4. 执行与提交

执行过程中，heartbeat 只续期 Claim，不写入独立的可变进展说明。WorkItem 的进展通过 Claim、拆分、Submission、Review、Failure、Skip 和创建后续 Task 等持久操作发生变化。

提交 Task 时，Agent 记录已完成工作、与本次交付有关的已发现问题、成果和 Artifact。Kairos 在 Task 下创建不可变的 Submission，并使其成为 WorkItem 的共享上下文。返工后的再次提交形成新的 Submission，不覆盖此前结果。

提交需要人工 Review 时，当前 Claim 随提交结束，Task 在 `InReview` 期间不再要求 Agent 保活。Review 驳回后 Task 回到候选集合，并在新的 Claim 下继续执行。

```text
Task
├── Claim 与生命周期历史
├── Submission 1
│   ├── Result
│   └── Artifacts
└── Submission 2
    ├── Result
    └── Artifacts
```

提交还可以携带当前协调模式允许的推进决策。Kairos 根据这些决策和模式规则更新 Task Graph。

Operation ID 只用于创建 WorkItem、Task Claim、Coordination Claim、Artifact 或 Blackboard Task 的 HTTP 或 MCP 调用。完全相同的重试会返回原资源，避免响应丢失后无法找回服务端生成的 ID；同一 Operation ID 被用于不同参数时返回冲突。Definition 追加使用 base version，生命周期变更不保存旧响应：重试会依据当前 Task 和 WorkItem 状态重新判断，因此首次成功后可能返回冲突。托管 Artifact 上传还会通过 Operation ID 跨数据库与文件 Store 写入恢复。

Agent 无法完成 Task 时，可以提交失败原因并选择重新打开 Task 或使整个 WorkItem 失败。重新打开时可以附加 Retry Prompt；失败记录和提示会进入后续执行者读取的完整 Task 上下文。

Human 可以在 Task 执行之外独立取消所属 WorkItem；Kairos 不向 Agent 或 MCP 提供取消动作。如果 heartbeat、提交、创建 Artifact、报告失败、释放 Claim 或其他变更返回 `work_item_cancelled`，取消决定具有权威性：Agent 立即停止，不再尝试失败、释放或以其他方式更新 Task。

## 5. Workflow 能力

Workflow 中的 Agent 执行已经定义好的 Task，并在配置允许的位置作出判断：

- 从多个候选 Task 中选择工作；
- 判断当前 Task 所连接的 optional Task 应当保留还是跳过；
- 在 `executor_decides` 模式下判断是否请求人工 Review。

Agent 对 optional Task 的判断随当前 Task 一并提交。多个前置 Task 的判断由 Kairos 聚合，任意执行者选择保留时，该 Task 进入候选集合。

正式 Task Graph、required Task 和 required Review 继续由 Workflow 保证。

## 6. Blackboard 能力

Blackboard 中的 Agent 同时参与执行与规划，可以：

- 创建新的 Task；
- 拆分已有 Task；
- 创建带有发现 tags 的 Task；
- 添加建议性的 Task Relation；
- 将失去价值的 Task 标记为 Skipped；
- 根据当前成果请求人工 Review；
- 判断 WorkItem 是否已经满足目标。

这些变化进入共享 Task Graph，后续的人和 Agent 都能看到最新的工作结构与成果。

## 7. 规划中的 Bridge

Bridge 连接 Kairos 与特定 Agent Harness：

```text
Kairos Candidate Task
         ↓
       Bridge
         ↓
Codex / Claude Code / Other Harness
```

未来的 Bridge 可以为符合 Role 的 Agent 选择 Task、启动 Agent、提供上下文并回传生命周期操作与成果。Agent 主动参与和 Bridge 派发使用相同的 Task、Claim 与提交语义。

因此，Kairos 的 Agent 交互模型独立于具体 Harness，也独立于 Agent 如何开始执行。

## 8. MCP 与 Skill 接入面

Kairos 通过无状态 Streamable HTTP MCP 端点暴露主动执行闭环。每个 HTTP 请求都独立通过 Trusted 或 Authenticated Mode 解析 Actor，因此身份不依赖 MCP Session，也不会作为工具参数被接受。

MCP 接入面包含工作发现、Task 上下文、可读取终态的 WorkItem 上下文、Task 与 Coordination Claim 生命周期、外部 Artifact 登记、Base64 托管 Artifact 上传、提交、失败，以及 Blackboard 规划与关闭。Task 与 Coordination Claim 的创建和 heartbeat 都接受可选的 `lease_seconds`，服务端返回实际批准的时长与 `lease_until`。Blackboard Task 上下文中的顶层 `task` 是当前任务；`blackboard.tasks` 会有意排除当前任务，并通过 `blackboard.current_task_id` 提供关联。响应使用紧凑的 `snake_case` 执行视图，不直接暴露完整持久化模型。Definition 与 Identity 管理、人工 Review 决策仍位于 Agent 接入面之外。仓库级 Codex Skill 为兼容的 Harness 提供执行与 heartbeat 循环及资源创建重试纪律，`.codex/config.toml` 则负责将 Codex 连接到本地项目服务。

> One execution protocol, two coordination modes.
