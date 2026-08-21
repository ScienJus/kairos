# Kairos 核心工作模型

> WorkItem、Task、Workflow 与 Blackboard 的概念定义及统一建模

## 摘要

Kairos 使用 `WorkItem` 表达完整工作目标，使用 `Task` 表达人或 Agent 可执行和交付的工作单元。一个 WorkItem 内的 Task 及其关系组成 Task Graph。

Workflow 和 Blackboard 是 Task Graph 的两种组织语义。Workflow 使用正式且具有约束力的计划，Blackboard 由协作者在执行过程中持续形成和调整计划。两者共享相同的底层工作模型。

## 1. WorkItem 与 Task

### 1.1 WorkItem

`WorkItem` 表示一个完整的工作目标，即人类或系统希望获得的最终结果。

例如：

```text
实现登录功能
修复支付系统的重复扣款问题
完成 Kairos 的第一版概念设计
```

WorkItem 承载：

- 工作目标；
- 背景与上下文；
- 约束与验收标准；
- 最终交付成果；
- 内部 Task 及其关系。

WorkItem 可以在创建时包含完整计划，也可以只包含目标，由协作者在执行过程中逐渐形成计划。

每个 WorkItem 创建时绑定一个固定版本的 Coordination Definition。Definition 决定它采用 Workflow 或 Blackboard，并提供该协作空间的名称、说明、Agent Instructions 与 Suggested Tags。Workflow Definition 另外定义正式执行结构；Blackboard Definition 不预先定义 Task Graph。

> WorkItem 是目标与成果的边界。

### 1.2 Task

`Task` 是从 WorkItem 中拆分出来、可以由人或 Agent 执行和交付的工作单元。

```text
WorkItem：实现登录功能
├── Task：设计登录方案
├── Task：实现登录接口
└── Task：测试登录功能
```

Task 是单一执行者的执行边界：

- 可以作为独立的候选工作；
- 具有明确的执行内容和交付结果；
- 执行期间只有一个责任执行者；
- 进展和成果记录在 Task 中，并构成 WorkItem 的共享上下文。

Task 的粒度应当支持一个执行者在一次连贯的工作过程中对其负责并产生交付。Task 可以限定由 Agent、人或两者中的任意一方执行。

Blackboard 中的执行者也可以在产生成果前将 Task 拆分为子 Task。拆分后的父 Task 成为聚合边界，不再由执行者提交成果；它在全部子 Task 结束后完成。一个 Task 只采用直接交付或子 Task 聚合中的一种方式。

执行者每次正式提交结果时，Kairos 在 Task 下创建不可变的 Task Submission，并关联产生该结果的 Claim。Task 可以经历多轮执行、提交和 Review，全部 Submission 都作为共享历史保留。

Result 是执行者留下的持久说明，Artifact 是具名、可寻址的实际交付物。执行者持有 Claim 时可以暂存 Artifact，并在创建 Submission 时绑定。绑定后的 Artifact 进入同一份不可变历史，并对整个 WorkItem 可见。

执行者报告失败时，Kairos 在 Task 下创建不可变的 Task Failure。重新打开产生的提示会进入后续执行上下文；全局失败则结束当前 Task 与 WorkItem。Claim、Submission、Review、Failure 和推进决策同时形成按顺序追加的 WorkItem Event 历史。

> WorkItem 回答“最终要完成什么”，Task 回答“接下来具体做什么”。

## 2. Task Graph

一个 WorkItem 包含零个或多个 Task。Task 之间通过有向关系组成 Task Graph。

```text
设计登录方案 → 实现登录接口 → 测试登录功能
```

Task Graph 可以表达：

- Task 的拆分层级；
- 前置依赖；
- 并行工作；
- 一个 Task 连接多个后续 Task；
- 多个 Task 连接同一个后续 Task；
- 工作拆分与组合。

Workflow 和 Blackboard 使用相同的运行时 Task Graph。上层组织语义决定图如何产生、如何演化，以及关系是否构成执行约束；Workflow 另外通过带版本的正式定义决定运行图如何展开。

## 3. Workflow

`Workflow` 使用带版本的正式定义组织 WorkItem 内部的工作。WorkItem 创建时绑定一个已发布的 Workflow Definition ID 与 Version，此后不会随 Workflow 的新版本变化。

```text
设计 ──→ 实现 ──→ 测试
```

定义中的关系具有约束力。“设计 → 实现”表示设计完成后，系统才会为当前 WorkItem 产生“实现”Task。

Workflow 执行时按推进需要实例化 Task。定义中的同一个节点被多次经过时，每次产生新的 Task 实例，使每一轮执行都保留独立的 Claim、进展和成果。由这些实例组成的运行时 Task Graph 记录实际执行历史。

Workflow 的主要特征包括：

- WorkItem 绑定固定的 Workflow Definition ID 与 Version；
- Task 及其关系来自该版本的正式定义；
- Task 实例随 Workflow 推进按需产生；
- 前置依赖由系统强制执行；
- 运行期间的结构修改受到规则约束；
- 系统根据结构计算当前合法的候选 Task；
- WorkItem 的完成通常可以从正式结构中推导。

一个 Workflow 可以同时产生多个合法候选 Task。Workflow 限定选择空间，人或 Agent 可以主动选择，Bridge 也可以派发。

> Workflow 是正式定义且具有约束力的 Task Graph。

## 4. Blackboard

`Blackboard` 是 WorkItem 内由协作者共同维护的开放协作空间。WorkItem 提供目标、背景、约束和验收标准，Task Graph 则在执行过程中逐渐形成。

WorkItem 绑定一个固定版本的 Blackboard Definition。Definition 确定 WorkItem 的协作空间归属，并为参与者提供全局说明、Agent Instructions 与 Suggested Tags，但不提供初始 Task Graph。

初始状态可以只包含工作目标：

```text
WorkItem：实现登录功能
Tasks：[]
```

协作者根据当前理解建立计划：

```text
[ ] 设计登录方案
[ ] 实现登录接口
[ ] 测试登录功能
```

随着工作深入，计划可以继续演化：

```text
[x] 设计登录方案
[ ] 实现密码登录
[ ] 实现会话管理
[ ] 增加暴力破解防护
[ ] 测试登录功能
```

Blackboard 中的 Task 也可以具有前置关系：

```text
设计登录方案 ⇢ 实现登录接口 ⇢ 测试登录功能
```

这些关系表达协作者当前共同认可的推进建议。执行者可以结合实际上下文提前执行、并行推进、调整关系，或者创建新的 Task。

Blackboard 的主要特征包括：

- 初始 Task Graph 可以为空或不完整；
- 协作者动态创建、拆分和调整 Task；
- 前置关系默认作为推进建议；
- 下一步工作由执行者结合目标和共享上下文判断；
- 执行者在结束当前 Task 前决定是否需要创建后续 Task；没有未结束 Task 时 WorkItem 完成。

> Blackboard 是由协作者持续规划和演化的 Task Graph。

## 5. 统一底层模型

Workflow 和 Blackboard 映射到相同的基础结构：

```text
                         WorkItem
                     “实现登录功能”
                            │
                        Task Graph
                            │
              ┌─────────────┴─────────────┐
              │                           │
           Workflow                   Blackboard
        正式、强约束的图              动态、建议性的图
```

底层模型包含三个核心概念。Task 通过 Parent Task 表达层级，通过 Task Relation 表达有向关系：

```text
WorkItem
Task
Task Relation
```

两种组织语义的差异如下：

| 维度 | Workflow | Blackboard |
| --- | --- | --- |
| WorkItem | 完整工作目标 | 完整工作目标 |
| Task | 可执行、可交付单元 | 可执行、可交付单元 |
| 初始 Task Graph | 通常预先定义 | 通常为空或不完整 |
| Task 如何产生 | 来自正式计划 | 由协作者动态规划 |
| Graph 如何演化 | 按规则运行和调整 | 随协作持续演化 |
| Task Relation | 执行约束 | 推进建议 |
| 候选 Task | 由结构计算 | 由结构和上下文形成 |
| 完成判断 | 通常可从正式结构推导 | 回到 WorkItem 目标判断 |

因此，Workflow 和 Blackboard 共享数据结构，并采用不同的 coordination semantics：

> Workflow 的图规定工作如何推进；Blackboard 的图记录协作者当前认为工作应当如何推进。

## 6. 展现形式

WorkItem 集合使用 List 和 Kanban 两种视图：

```text
WorkItem Collection
    ├── List
    └── Kanban
```

List 适合搜索、筛选和高密度浏览，Kanban 用于观察完整工作的状态与流动。两种视图展示同一组 WorkItem。

进入 WorkItem 后，内部 Task 根据协调模式渲染：

```text
WorkItem Detail
    ├── Workflow  → Flow Graph
    └── Blackboard → Checklist
```

Flow Graph 展示 Workflow 的正式依赖和推进状态。Checklist 展示 Blackboard 动态形成的 Task、tags、建议关系和共享成果。

Kanban 只展示 WorkItem。Task 始终存在于所属 WorkItem 内，由 Flow Graph 或 Checklist 承载其交互。

## 7. 核心定义

Kairos 的核心工作模型可以归纳为：

```text
WorkItem   = 完整工作目标
Task       = 可执行、可交付的工作单元
Workflow   = 正式定义且具有约束力的 Task Graph
Blackboard = 由协作者动态规划和演化的 Task Graph
```

> Workflow executes an authoritative plan; Blackboard grows a shared plan while executing the work.
