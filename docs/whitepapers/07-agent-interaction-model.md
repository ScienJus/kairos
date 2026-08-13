# Kairos Agent Interaction Model

> Agent 参与 Workflow 与 Blackboard 的统一交互方式

## 摘要

Kairos 为 Agent 提供统一的 Task 交互过程。Agent 可以主动发现和选择工作，也可以由 Bridge 启动并接收已经分配的 Task。两种方式最终都围绕同一个 Task 建立执行责任、读取工作上下文、记录进展并提交成果。

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
record progress
        ↓
submit result
```

Agent 在执行前读取必要上下文并确认 Task。Claim 由 Agent 或 Bridge 建立，并在执行开始前形成唯一的执行责任。执行期间的进展和最终成果都记录在 Task 中。

## 2. 发现工作

Agent 只发现允许 Agent 执行的 Task：

```text
executor = agent | either
+ role matched
```

候选 Task 的来源由协调模式决定：

| 模式 | 候选 Task |
| --- | --- |
| Workflow | 前置关系已满足的 required Task，以及决定保留的 optional Task |
| Blackboard | 符合 tags 和查询上下文的 Task |

候选结果提供足够的信息帮助 Agent 比较工作，包括 WorkItem 摘要、Task 目标、协调模式、tags 和当前可执行原因。Agent 可以进一步读取完整上下文后再建立 Claim。

## 3. Task 上下文

Agent 执行 Task 时获得四类信息：

```text
WorkItem Context
    目标、背景、约束与验收标准

Task Context
    Task 描述、交付要求与当前进展

Related Results
    相关 Task 的成果与 Artifact

Coordination Context
    当前模式、Task Relation 与可用决策
```

Workflow 的 Coordination Context 包含正式前置关系、optional 判断和 Review 配置。Blackboard 则包含建议关系、tags 和当前共享工作态势。

Agent 可以按需读取更多历史和成果。默认上下文应优先提供与当前 Task 直接相关的信息。

## 4. 执行与提交

Agent 在执行过程中可以持续记录：

- 当前进展；
- 已完成的工作；
- 发现的问题；
- 产生的成果和 Artifact。

提交 Task 时，Agent 提供本次交付的结果摘要和相关成果。Kairos 将这些信息保存到 Task，并使其成为 WorkItem 的共享上下文。

```text
Task
├── Progress
├── Result
└── Artifacts
```

提交还可以携带当前协调模式允许的推进决策。Kairos 根据这些决策和模式规则更新 Task Graph。

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
- 添加或调整 tags；
- 维护建议性的 Task Relation；
- 将失去价值的 Task 标记为 Skipped；
- 根据当前成果请求人工 Review；
- 判断 WorkItem 是否已经满足目标。

这些变化进入共享 Task Graph，后续的人和 Agent 都能看到最新的工作结构与成果。

## 7. Bridge

Bridge 连接 Kairos 与特定 Agent Harness：

```text
Kairos Candidate Task
         ↓
       Bridge
         ↓
Codex / Claude Code / Other Harness
```

Bridge 可以选择 Task、启动 Agent、提供上下文并回传进展与成果。Agent 主动参与和 Bridge 派发使用相同的 Task、Claim 与提交语义。

因此，Kairos 的 Agent 交互模型独立于具体 Harness，也独立于 Agent 如何开始执行。

> One execution protocol, two coordination modes.
