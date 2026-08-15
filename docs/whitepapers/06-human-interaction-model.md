# Kairos Human Interaction Model

> 以 WorkItem List 与 Kanban 为中心的人类工作界面

## 摘要

Kairos 以 List 和 Kanban 展示 WorkItem 集合。List 提供高密度管理能力，Kanban 提供完整工作的状态总览。两种视图使用相同的数据、筛选条件和操作结果。

进入 WorkItem 后，内部 Task 根据协调模式呈现：Workflow 使用 Flow Graph，Blackboard 使用 Checklist。人通过这些界面观察 Agent 工作，也可以直接成为 Task 的执行者。

## 1. 交互结构

Kairos 的人类界面分为两个层次：

```text
WorkItem Collection
    ├── List
    └── Kanban
          ↓ 打开 WorkItem
WorkItem Detail
    ├── Workflow  → Flow Graph
    └── Blackboard → Checklist
```

List 和 Kanban 的对象都是 WorkItem。Task 存在于 WorkItem 详情中，并按照 Workflow 或 Blackboard 的语义进行交互。

WorkItem 详情还提供按顺序追加的事件历史，用于追溯 Task 创建、Claim、提交、Review、失败和 Workflow 推进决策。

## 2. List

List 用于管理大量 WorkItem，重点提供：

- 搜索、筛选和排序；
- 高密度字段展示；
- 快速定位当前执行者、进度和更新时间；
- 批量管理；
- 打开 WorkItem 详情。

List 适合回答：

```text
有哪些工作？
哪些工作符合当前条件？
我需要找到哪一个具体 WorkItem？
```

## 3. Kanban

Kanban 将同一组 WorkItem 按整体状态组织，使人能够快速理解工作如何流动。

它主要承担四个作用：

### 3.1 共享工作态势

Kanban 汇总当前有哪些工作尚未开始、正在推进、需要关注或已经结束。人无需进入每个 WorkItem，即可了解整体进展。

### 3.2 发现积压与异常

卡片在列中的分布能够暴露长期未推进的工作、过多的进行中工作以及等待人工处理的事项。

### 3.3 连接人和 Agent

WorkItem 卡片可以展示当前 Task 进度、执行者和待 Review 提示。人可以从卡片进入详情，查看 Agent 成果、处理 Review，或者领取适合人工执行的 Task。

### 3.4 提供统一入口

Workflow 和 Blackboard 使用同一个 Kanban。卡片展示完整工作，协调模式决定 WorkItem 内部如何推进。

> Kanban is the shared operational view of work performed by people and agents.

## 4. WorkItem Card

Kanban 卡片用于概括一个完整工作，可以包含：

- 标题与目标摘要；
- Workflow 或 Blackboard 标识；
- Task 完成进度；
- 当前执行者摘要；
- 待 Review 提示；
- tags、优先级和更新时间。

卡片只提供足以判断工作态势的信息。具体 Task、依赖、成果和操作进入 WorkItem 详情后呈现。

## 5. Workflow Detail

Workflow WorkItem 使用 Flow Graph 展示内部 Task：

```text
设计
 ├──→ 前端实现 ─┐
 ├──→ 后端实现 ─┼→ 集成测试
 └──→ 编写文档 ─┘
```

每个 Task 节点可以展示：

- 当前状态；
- 执行者类型和责任执行者；
- required 或 optional；
- Review 配置与当前 Review 状态；
- 进展与成果入口。

人可以在 Flow Graph 中领取人工 Task、查看 Agent 提交和处理 Review。人作为前置 Task 的执行者时，也可以对后续 optional Task 作出判断。

Flow Graph 的操作服从 Workflow 语义。前置关系、required Task 和 required Review 继续由系统保证。

## 6. Blackboard Detail

Blackboard WorkItem 使用 Checklist 展示动态形成的 Task：

```text
[x] 设计登录方案
[ ] 实现登录功能
    [ ] 实现密码登录      backend, auth
    [ ] 实现会话管理      backend, auth
    [ ] 增加暴力破解防护  security
[ ] 测试登录功能          test
```

Checklist 支持：

- 创建、拆分和调整 Task；
- 查看 tags 和建议关系；
- 领取 Task；
- 更新进展与成果；
- 发起或处理 Review；
- 将失去价值的 Task 标记为 Skipped。

Checklist 直接体现 Blackboard 的动态规划方式。协作者围绕同一列表持续补充和修正对 WorkItem 的理解。

## 7. 语义一致性

界面操作最终作用于统一 Work Model，并遵守当前协调模式：

- Kanban 卡片移动需要满足 WorkItem 的状态规则；
- Workflow 的 Flow Graph 保证正式依赖和 Review 要求；
- Blackboard 的 Checklist 允许协作者动态调整 Task；
- Claim 始终建立唯一的 Task 执行责任；
- 人和 Agent 的进展与成果使用相同的记录方式。

因此，同一个操作在界面上可以具有统一形式，其有效性由 Workflow 或 Blackboard 的语义决定。

## 8. 核心定义

Kairos 的人类交互模型可以归纳为：

```text
List      = WorkItem 的管理视图
Kanban    = WorkItem 的状态与流动视图
Flow Graph = Workflow Task 的执行视图
Checklist = Blackboard Task 的协作视图
```

> List helps people find work; Kanban helps them understand its flow; WorkItem details let them act.
