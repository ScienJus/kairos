# Task Detail 展示与操作架构设计

## 1. 背景

当前 Web Task Detail 同时承担了两类职责：

1. 向查看者展示 Task 的完整生命周期；
2. 向执行者提供 Claim、提交、失败、分解等操作。

现有 `/api/v1/tasks/{id}/context` 是执行者上下文。它会通过 `identityCanExecute` 和 Claim 可见性规则校验请求身份，适合 Agent 或 Human 执行 Task，但不适合作为通用详情接口。例如 Human 查看一个由 Agent 完成的 Task 时，不具备该 Task 的执行权限，执行上下文会返回 `403 Forbidden`。如果 Task Detail 依赖该接口，静态详情、历史责任人和人工 Review 操作也会一并失效。

本文定义 Task Detail 的目标展示模型、接口边界和前后端职责。本文描述的是目标设计；未完成部分不得在 API 文档或项目状态中描述为已实现。

## 2. 核心原则

### 2.1 查看与执行分离

- **Task Detail** 是面向查看者的生命周期视图，不要求查看者能够执行 Task。
- **Task Execution Context** 是面向执行者的操作上下文，继续执行现有身份、角色和 Claim 权限校验。
- **Review Decision** 是独立的人工操作，校验 Review 权限，不继承原 Task 的 Agent 执行角色要求。

### 2.2 后端投影，前端渲染

后端负责：

- 解释 Task 状态和历史记录；
- 选择当前责任人、当前结果和当前 Review；
- 判断当前查看身份可执行的操作；
- 返回稳定、规范化的数据结构。

前端负责：

- 按投影字段渲染详情；
- 按后端返回的 capability 显示操作；
- 收集表单输入并调用对应命令接口。

前端不得通过扫描 `Reviews`、`Claims` 或 `Submissions` 推导当前责任、Review 是否待处理或当前身份是否有权操作。

### 2.3 展示失败与权限不足不是操作状态

- 不需要执行上下文时，不应请求执行上下文。
- 没有某项操作权限时，不渲染该操作；不得依靠接口 `403` 推导按钮可见性。
- 请求失败应显示真实错误，不得显示为“正在准备任务”。
- 一个操作区失败不得阻断基本信息和生命周期历史的展示。

## 3. 状态展示矩阵

| Task 状态 | 主要责任信息 | 结果与历史 | 可选操作 |
| --- | --- | --- | --- |
| `pending` | 尚未认领；执行类型和允许角色 | Workflow 到达原因或 Blackboard 约束 | 有权限时 Claim；Blackboard 有权限时 Skip |
| `working` | 当前 Claim 执行人、认领时间和租约 | 历史 Claim | Claim 所有人可提交、释放、失败或分解 |
| `in_review` | 结果提交人 | Submission；当前 Review 请求人、时间、状态和历史 | 有 Review 权限时批准或拒绝 |
| `completed` | 最终执行人，或完成分解的人 | 最终结果、完成时间、最终 Review、完整历史 | 无执行操作 |
| `skipped` | 作出 Skip 决定的人 | Skip 来源、原因、时间和 Review（若有） | 无执行操作 |
| `failed` | 发生失败的执行人 | 失败原因、时间、动作、重试指引和历史 | 状态允许且有权限时重新 Claim |
| `waiting_children` | 作出分解的人 | 子任务、状态汇总和阻塞关系 | Blackboard 有权限时添加子任务 |

“执行类型”与“实际责任人”必须分开：

- `Executor: agent` 表示 Task 对执行者类型的要求；
- `Responsibility.Actor` 表示该生命周期阶段实际负责或作出决定的人；
- 已完成、跳过或失败的 Task 不应显示“尚未认领”作为主要责任信息。

## 4. 接口边界

### 4.1 Task Detail API

新增面向查看者的详情接口：

```text
GET /api/v1/tasks/{id}
```

建议响应模型：

```go
type TaskDetailView struct {
    Task           TaskView
    Responsibility TaskResponsibilityView
    Outcome        TaskOutcomeView
    CurrentReview  *TaskReviewView
    History        TaskHistoryView
    Workflow       *TaskWorkflowDetailView
    Blackboard     *TaskBlackboardDetailView
    Capabilities   TaskCapabilitiesView
}
```

该接口允许已认证查看者读取 Task 生命周期信息。具体可见范围仍由 WorkItem 的查看授权控制，但不得使用“能否执行该 Task”作为查看条件。

### 4.2 Task Execution Context API

保留：

```text
GET /api/v1/tasks/{id}/context
```

它只返回执行 Task 所需的上下文，例如：

- Definition 执行指引；
- Workflow choice groups 和受控上游输入；
- Blackboard 可分解信息；
- 当前执行者可见的 Claim。

该接口继续使用执行权限校验。Task Detail 不得依赖它展示静态信息、历史、最终责任人或 Review 操作。

### 4.3 命令接口

现有命令接口按各自语义授权：

- Claim、Submission、Failure、Release：校验 Task 执行权限和 Claim 所有权；
- Review Decision：校验 Human 身份和 Review 可决定性；
- Blackboard Skip、Decomposition、Child：校验对应规划权限。

详情接口中的 capability 仅用于 UI 呈现，命令接口仍必须独立完成授权，不能信任客户端 capability。

## 5. Task Detail 数据模型

### 5.1 Responsibility

```json
{
  "kind": "executed_by",
  "actor": { "kind": "agent", "id": "ui-dogfood-architect" }
}
```

`kind` 的稳定集合：

- `unclaimed`
- `claimed_by`
- `submitted_by`
- `executed_by`
- `decomposed_by`
- `skipped_by`
- `failed_by`

`actor` 是可选单值；确实未知时为 `null`。后端通过 Claim、Submission、Failure、Skip 和分解记录完成关联，前端不做关联查询。

### 5.2 Outcome

```json
{
  "kind": "completed",
  "reason": "",
  "occurred_at": "2026-08-19T02:10:44.202973Z"
}
```

Outcome 描述 Task 当前生命周期结论，不承载 Review 历史。`kind` 至少包括：

- `pending`
- `active`
- `waiting_children`
- `in_review`
- `completed`
- `skipped`
- `failed`

### 5.3 Current Review 与 Review History

```json
{
  "current_review": {
    "id": "review-id",
    "submission_id": "submission-id",
    "status": "pending",
    "requested_by": { "kind": "agent", "id": "agent-id" },
    "requested_at": "2026-08-19T02:10:44Z",
    "decided_by": null,
    "decided_at": null,
    "feedback": ""
  },
  "history": {
    "claims": [],
    "submissions": [],
    "reviews": [],
    "failures": [],
    "transition_decisions": []
  }
}
```

- `current_review` 表示与当前状态最相关的一轮 Review；没有时为 `null`。
- `history.reviews` 是按时间排序的完整历史，空集合返回 `[]`。
- `requested_by` 和 `decided_by` 统一使用 ActorRef，不能一个是字符串、另一个是对象。
- 已完成 Task 可以保留最后一轮 Review 作为 `current_review`，用于展示最终验收结论。

如果现有领域模型只保存 `ActorID`，在实现 ActorRef 投影前必须先确认 actor kind 的可靠来源；不能由前端猜测，也不能伪造默认 kind。

### 5.4 Capabilities

```json
{
  "can_claim": false,
  "can_submit": false,
  "can_release": false,
  "can_fail": false,
  "can_review": true,
  "can_skip": false,
  "can_decompose": false,
  "can_add_child": false
}
```

Capabilities 针对当前请求身份计算。建议为每项能力返回布尔值；只有 UI 确实需要解释禁用原因时，再增加结构化 reason，避免把后端错误字符串变成前端业务契约。

## 6. 前端组件组织

```text
TaskDetailPage
├── TaskIdentity
├── TaskSpecification
├── TaskLifecycleSummary
├── TaskResult
├── TaskReview
├── TaskHistory
└── TaskActions
    ├── ClaimAction
    ├── ExecutionActions
    ├── ReviewActions
    └── BlackboardPlanningActions
```

- `TaskDetailPage` 只请求并渲染 Task Detail。
- `TaskActions` 根据 `Capabilities` 选择子组件。
- 只有某个执行操作实际展开或需要执行数据时，才请求 Execution Context。
- `ReviewActions` 使用 `CurrentReview.ID`，不扫描 `Task.Reviews`。
- 历史组件只遍历后端已规范化的数组。

## 7. 错误与加载状态

Task Detail 和操作数据必须分别管理加载状态：

- Detail 加载中：显示详情骨架；
- Detail 失败：显示详情请求的真实错误；
- Execution Context 加载中：只在相关操作面板内显示加载状态；
- Execution Context `403`：操作不可用，但已加载的详情继续展示；正常情况下 capability 应避免发起该请求；
- Review 命令失败：保留反馈表单并显示命令错误。

不得使用 Task 状态猜测加载文案。例如 completed Task 的请求失败不能显示“正在准备任务”。

## 8. 实施步骤

1. 定义 Application 层 `TaskDetailView`、生命周期投影和 capability 投影。
2. 新增 Task Detail HTTP 路由和 transport view；规范空数组与可选单值。
3. 为每个 Task 状态增加 Application 单元测试。
4. 增加 HTTP 合约测试，特别覆盖 Human 查看 Agent Task 和空历史数组。
5. 前端 Task Detail 切换到新接口，移除对 Execution Context 的展示依赖。
6. 将执行上下文改为操作级按需请求。
7. ReviewActions 改用 `CurrentReview` 与 `Capabilities.can_review`。
8. 增加前端状态矩阵测试和真实服务 dogfood。
9. 删除 `TaskExecutionContext` 中仅为详情展示而添加的临时投影字段；若 Agent/MCP 执行仍需要其中部分字段，应基于执行语义单独保留并测试。
10. 更新 API 参考、前端手册和相关白皮书中的已实现行为。

## 9. 验收场景

至少覆盖以下端到端场景：

1. Human 查看 Agent 已完成 Task：显示 `Executor: Agent`、实际执行 Agent、结果和完成时间，无 `403`，无操作错误。
2. Human 查看 Agent 的 `in_review` Task：可查看 Submission 和 Review；有权限时可批准或拒绝。
3. 无 Review 权限的 Human 查看 `in_review` Task：可查看，但不显示 Review 命令。
4. Agent 查看并执行匹配 role 的 pending/working Task：按需取得 Execution Context 并完成操作。
5. 不匹配 role 的 Agent 查看 Task：详情可见，执行操作不可见，不通过 `403` 驱动 UI。
6. Skipped Task：显示决定跳过的人、原因和时间，不显示“尚未认领”。
7. Waiting Children Task：显示分解人和子任务状态；只有具备能力时显示添加子任务。
8. Failed Task：显示失败执行人、原因、动作和重试指引。

## 10. 当前实现状态

当前已经实现：

- 独立的 `GET /api/v1/tasks/{id}` Task Detail API；
- Responsibility、Outcome、CurrentReview、History 和 Capabilities 投影；
- 空历史集合规范化为 `[]`；
- Human 可查看 Agent Task，不经过 Task 执行权限校验；
- Web Task Detail 使用 Detail 投影展示责任、结果和历史；
- Review 操作使用 `CurrentReview` 和 `Capabilities.CanReview`；
- Execution Context 仅在 Claim、Submit、Release、Fail 或 Decompose 操作需要时加载；
- Detail 与 Execution Context 独立处理加载和错误状态。

仍需后续完善：Review 领域记录目前只持久化 `ActorID`，尚不能在不猜测的情况下把请求人和决定人投影为完整 ActorRef；因此 Detail 暂时沿用现有 Review 结构。Workflow/Blackboard 的关系原因和更丰富的 capability 禁用原因也尚未进入 Detail 投影。
