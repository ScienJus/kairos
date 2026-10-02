# Kairos 人类交互模型

人不应该先翻译一套协调数据库，才能判断下一步该做什么。Kairos 界面解释 Agent 同样使用的 WorkItem、Task、Claim 和结果，再以适合人的尺度呈现当前局面。

这是一层投影，不是一套平行流程。界面先说明正在发生什么，再提供当前使用者真正可以执行的动作。

## 交互层级：从目标走到一个动作

```text
Workspace → WorkItem → Task Detail → 单个操作
```

- **Workspace** 帮助选择目标，区分当前工作、历史和需要人处理的事项。
- **WorkItem** 展示目标、约束、当前执行结构和整体生命周期。
- **Task Detail** 展示一个 Task 的责任、结果、历史和当前可用操作。

进入下一层后，上一层的管理信息应该退出注意力，而不是将所有数据堆在一个仪表盘上。

## Workspace 与人工关注

Workspace 中的主要分类是：

- 尚未结束的 WorkItem；
- 已完成、取消或失败的历史 WorkItem；
- 当前 Human 需要处理的 Task、Review 和 WorkItem 验收。

“需要人处理”是服务端基于领域状态计算的行动投影，不是独立队列或新生命周期。它只包含当前 Human 可领取或已负责的工作，以及待审核和待验收项。

## WorkItem 投影：读懂当前局面

WorkItem 页先展示意图和当前执行局面：

- Workflow 使用流程图，合并 Definition 节点、运行时 Task 实例和已提交决策；
- Blackboard 使用 Task 层级表达动态计划，Relation 是建议而非阻塞依赖；
- 失败、取消、验收中和已结束都是 WorkItem 级别事实，不应由前端从 Task 数量猜测。

界面可以对复杂历史做折叠和汇总，但不能改写底层事实或把未产生的 Workflow Task 当成可执行 Task。

## Task Detail：进入一个 Task

查看 Task 与执行 Task 是两种权限：

- **Detail 投影** 向可查看者返回责任摘要、最新结果、Artifact、Review/Failure 历史和 capabilities；
- **Execution Context** 只向具备执行资格的人或 Agent 提供完成当前工作所需的上下文。

前端只按后端 capabilities 展示操作，不从状态、历史或 Actor ID 重复推导领域权限。执行命令时后端仍必须重新校验。

## 人类操作：在共享生命周期上行动

Human 可以在同一持久模型上：

- 领取、提交、释放或报告 Task 失败；
- 审核 Submission，并在拒绝时留下修改指引；
- 在 Blackboard 中创建、拆分、追加或跳过 Task；
- 提交或验收 Blackboard 完成结果；
- 继续失败的 Workflow，或从原始目标创建新 WorkItem 从头执行；
- 取消仍可变更的 WorkItem。

操作需要显式的成功、冲突和失败反馈。网络错误不得显示为“没有权限”，也不得清空尚未提交的输入。

## 历史与可追溯性：不止是屏幕上的内容

Task 的 Claim、Submission、Review 和 Failure 历史是权威记录。界面可以选择默认展示哪些摘要，但不能把“未展示”描述为“不存在”。

WorkItem 级事件用于审计和后续界面投影；业务状态仍以 WorkItem、Task、Claim 及其关联记录为准。

## 交互不变量

- 查看权限不自动等于执行权限。
- 界面 capabilities 是当前快照，不是授权凭据。
- 人和 Agent 操作同一份生命周期，不建立两套状态。
- 当前工作与历史分开展示，但历史始终可查。
- 复杂领域判断由后端投影，前端负责清晰呈现和收集意图。
