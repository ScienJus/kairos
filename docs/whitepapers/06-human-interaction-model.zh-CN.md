# Kairos 人类交互模型

> 人如何找到需要自己处理的工作、理解进度并直接采取行动

## 摘要

人不应该为了了解进度而逐个翻阅 Agent 对话。Kairos 提供一个统一工作区，用来查看正在推进的 WorkItem、找到必须由人决定的事项，并直接进入需要处理的 Task。

Workflow 以流程图呈现，Blackboard 以持续变化的 Task 层级呈现。人在这些界面中可以查看 Agent 活动、领取人工 Task、提交结果、处理 Review，并在当前模式允许时参与规划。

## 1. 交互结构

operations console 包含三个相互连接的界面：

```text
Workspace
├── 全部工作
└── 需要人工关注
        ↓ 打开 WorkItem
WorkItem 详情
├── Workflow   → Flow Graph
└── Blackboard → Task 层级
        ↓ 打开 Task
Task 详情与操作
```

Workspace 概括完整的 WorkItem。协调和执行仍位于每个 WorkItem 内部，其中的 Task 按照 Workflow 或 Blackboard 语义呈现。

## 2. Workspace

Workspace 区分仍在推进的工作与已经进入终态的工作，并通过 WorkItem 的标题、目标和状态直接进入其协调详情。

当前“需要人工关注”投影聚合：

- 待处理的 Review；
- `executor=human`、Pending 且尚未被 Claim 的 Task；
- 等待人工验收的 WorkItem 完成提案。

它是同一份持久工作模型的行动投影，不是具有独立生命周期语义的另一套队列。当前投影不包含 `executor=either` Task，尽管人仍可通过 Task 详情 Claim 这类 Task。

## 3. WorkItem 进展

Kairos 不维护独立、可变的 `Task.Progress` 字段。WorkItem 的进展由其 Task 状态和持久记录共同表达：

```text
创建或拆分 Task
      ↓
Claim 与当前执行责任
      ↓
Submission、Review、Failure 或 Skip
      ↓
Task Graph 与 WorkItem 状态推进
```

Claim、提交、Review、失败、Skip、拆分以及创建后续 Task，都会改变所属 WorkItem 可观察到的进展。Result 与 Artifact 则持久保留每次完成执行对整体目标的贡献。

因此，WorkItem 详情将目标和状态与 Task 结构组合展示，并可进入各 Task 查看责任、成果、Review、Failure、Artifact 和可用操作。

## 4. Workflow 详情

Workflow WorkItem 使用流程图展示正式结构和运行历史。流程图将不可变 Definition 与具体 Task 实例合并，使“尚未到达”的节点与真正可执行的工作保持明确区别。

流程图节点展示当前生命周期状态、执行者类型、允许的 Agent Role 和运行实例数量。选择具体 Task 后，详情展示责任摘要、验收标准、最新提交结果、Artifact、完整 Review 与 Failure 历史，以及当前可用操作。

人可以 Claim 符合条件的人工 Task、查看 Agent 提交、处理 Review，并在提交自己的工作时作出已配置的推进决策。所有操作继续服从 Workflow 前置关系和 Review 规则。

## 5. Blackboard 详情

Blackboard WorkItem 使用分层 Task 工作区展示协作者当前共享的计划，其中包含父子层级、tags、描述和生命周期状态。选择 Task 后可以查看详情与可用操作。

当前交互面支持：

- 创建 Task 和添加子 Task；
- 将已 Claim 的 Task 拆分为包含子 Task 的聚合边界；
- Claim 和提交 Task；
- 请求或处理 Review；
- 使用持久原因 Skip Pending Task；
- 在当前 Task 收敛后提交或验收 WorkItem 完成结果。
- 使用持久原因终止取消仍在推进的 WorkItem。

这些操作直接更新共享 Task Graph，因此也会更新 WorkItem 可观察到的进展。建议 Relation 已存在于持久模型、HTTP API、MCP 工具和执行上下文中，但当前控制台既不展示也不能创建 Relation。

控制台只向 Human identity 提供空 Blackboard 规划、收敛完成和 WorkItem 验收决策。Agent 通过 MCP 执行这些决策，以便在开始分析前先领取并维护所需的 Coordination Claim。

取消被明确建模为 WorkItem 级管理动作。详情页只在 WorkItem 仍活跃或等待验收时向 Human 身份提供该操作，确认原因后进入只读终态，并展示记录的操作者、时间和原因。

## 6. 历史

Kairos 已经为 Task 创建、Claim、Submission、Review、Failure 和 Workflow 推进决策持久追加 WorkItem Event。`GET /api/v1/tasks/{id}` 已返回所选 Task 规范化的 Claim、Submission、Review、Failure 和 Transition Decision 历史。当前控制台只渲染责任摘要、最新提交结果、完整 Review 历史和完整 Failure 历史，尚未渲染 Claim 历史、全部 Submission 或 Transition Decision。

operations console 中完整的 WorkItem 级事件时间线仍在规划中。在该界面实现前，持久事件流属于内部审计记录，不描述为用户可见能力。

## 7. 语义一致性

所有界面操作都作用于统一工作模型，并服从当前协调模式：

- Workflow 继续以正式依赖和 Review 要求作为强约束；
- Blackboard 通过当前已支持的规划操作扩展和组织共享计划；
- 执行者类型决定允许参与的 Actor 类型，allowed roles 只进一步限制 Agent；
- Claim 确定执行期间唯一负责的具体 Actor；
- Task 生命周期变化与持久成果共同表达所属 WorkItem 的进展。

## 8. 核心定义

```text
Workspace       = 完整 WorkItem 的运营总览
需要人工关注    = 当前投影选取的人工关注信号
WorkItem 详情   = 一个完整目标的协调状态与进展
Task 详情       = 一个执行单元的责任、历史与操作
```

> Workspace 回答“哪里需要我”；WorkItem 详情解释整体进度；Task 详情提供可以立即执行的操作。
