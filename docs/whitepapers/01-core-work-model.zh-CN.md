# Kairos 核心工作模型

当工作只存在于对话里，目标会逐渐模糊，责任会悄然转移，下一位执行者还得重新拼接已经发生的事情。Kairos 为这些事实提供持久的名字：WorkItem 承载目标，Task 描述一次可交付执行，Relation 把 Task 连接成图。

Workflow 和 Blackboard 以不同方式组织这张图，但不会形成两套工作模型。两种模式都用相同记录保存责任、结果、审核和历史。

## Definition、WorkItem 与 Task

### Definition

Definition 是可复用、带版本的协作模板，提供名称、说明、默认指引和模式配置。WorkItem 绑定一个具体版本，后续修改不会改变已开始的工作。

### WorkItem

WorkItem 是一次具体的协作目标，包含：

- 意图：目标、背景、约束和验收标准；
- 模式：Workflow 或 Blackboard；
- 工作结构：Task 与 Relation；
- 进展记录：Claim、Submission、Review、Failure、Artifact 和领域事件；
- 生命周期：执行中、待验收或终态。

WorkItem 结束后仍保留完整历史，不被重写为一个只含最终结果的快照。

### Task

Task 是单个执行者一次连贯工作的边界，记录目标、描述、执行者约束、验收要求和生命周期。

Task 可以独立完成，也可以拆分为子 Task。拆分后，父 Task 变成聚合节点，不再产生自己的 Submission；子任务共同表达其完成过程。

## Task Graph

WorkItem 中的 Task 与 Relation 构成 Task Graph：

```text
WorkItem
├── Task A ──→ Task C
└── Task B ──↗
```

Relation 的权威性由协调模式决定：

| 模式 | 图的来源 | Relation 含义 | 图如何变化 |
| --- | --- | --- | --- |
| Workflow | 带版本的 Definition | 约束合法推进路径 | 运行中不修改 Definition；只追加实例和决策 |
| Blackboard | 协作者当前的共同认知 | 推进建议，不构成硬阻塞 | 执行期间可追加 Task 和 Relation |

候选计算、图的权威性和完成语义见[协调语义](03-coordination-semantics.zh-CN.md)；两种模式的具体规则分别见 [Workflow](04-workflow.zh-CN.md) 和 [Blackboard](05-blackboard.zh-CN.md)。

## 跨会话保留的工作记录

Task 不只保存当前状态，还保存状态是如何形成的：

- **Claim**：某个执行者在一段时间内承担责任；
- **Submission**：一次正式提交及其结果；
- **Review**：针对某次 Submission 的审核决定与反馈；
- **Failure**：某次 Claim 为何无法完成；
- **Artifact**：可持续访问的交付物引用或托管内容；
- **Event**：按顺序追加的领域变化记录。

这些记录属于 Task 和 WorkItem，不属于临时的 Agent 会话。后续执行者因此可以从持久事实继续工作。

## 生命周期边界

- Task 状态与它的活动 Claim 必须一致；终态 Task 不能再被修改。
- WorkItem 的终态会结束活动 Claim，并阻止后续 Task 变更。
- 执行失败、人工取消和验收拒绝是不同领域事实，不互相伪装。
- 历史 Claim、Submission、Review、Failure 和 Artifact 不因重试或恢复而被覆盖。
- 列表字段为空时编码为 `[]`；真正可选的单值才使用 `null`。

## 这套模型保护什么

WorkItem 是团队共同推进的目标，Task 是由一个责任执行者完成的一次可交付执行。Workflow 约束预定义路径何时能够推进，Blackboard 则允许团队随着理解变化追加当前计划。进展由持久生命周期记录表达，因此工作不依赖任何对话或 Agent 会话持续在线。
