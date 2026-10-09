# Kairos 协调语义

Workflow 和 Blackboard 都把工作呈现为 Task Graph，因此很容易被看成同一种机制的两种界面。其实并不是：在 Workflow 中，图是执行规则；在 Blackboard 中，图是团队留给自己的当前建议。

这个权威性差异解释了其他几乎所有区别：Task 何时成为候选、谁能改变图，以及何时可以认定 WorkItem 已经完成。

## 共用的协调循环

```text
Task Graph + 当前上下文
             ↓
          候选集合
             ↓
      选择执行者并 Claim
             ↓
          提交成果
             ↓
       更新图或生命周期
```

候选只表示“当前允许考虑的工作”。它不预先建立责任，也不按列表顺序表达优先级。人、主动 Agent 或 Agent Daemon 选中候选后，Claim 才为具体 Actor 建立唯一责任。

## Workflow 如何产生候选

Workflow 的 Definition Graph 是正式计划。运行时根据前置完成、分支决策、Review 和循环状态产生 Task 实例。

一个 Task 只有在下列条件都满足时才是候选：

- 它已从绑定 Definition 产生；
- 状态允许领取，且没有 Active Claim；
- WorkItem 仍允许执行；
- 执行者类型与 Agent Role 匹配。

正式 Relation 可以阻止下游 Task，执行者不能通过主观判断绕过。完整的分支、optional、Review、循环和恢复语义见 [Workflow 模式](04-workflow.zh-CN.md)。

## Blackboard 如何产生候选

Blackboard 的 Task Graph 记录当前共同计划，Relation 只提供建议。未完成的前置 Task 不会自动阻止后续 Task；执行者结合关系、已有成果和当前目标判断是否开始。

假设“收集证据”指向“起草报告”。在 Workflow 中，证据 Task 未满足图规则前，报告 Task 不会开放；在 Blackboard 中，同一条箭头会提醒报告作者证据尚不完整，但不会阻止其在掌握情况后先开始起草。图形相似，权威性并不相同。

普通 Task 候选需满足：

- 处于可领取状态且没有 Active Claim；
- 执行者类型、Role 和查询 tags 匹配；
- WorkItem 仍允许执行。

空 Blackboard、已收敛 Blackboard 和待 Agent 验收的 WorkItem 不依赖某个 Task，而是产生协调候选。Agent 必须先建立 Coordination Claim，再读取完整上下文并创建 Task、提交完成或验收完成。

完整的拆分、追加、Relation、Skip 和验收语义见 [Blackboard 模式](05-blackboard.zh-CN.md)。

## 图的演化：固定规则还是可追加计划

| 维度 | Workflow | Blackboard |
| --- | --- | --- |
| 规划权威 | 绑定的 Definition 版本 | WorkItem 中已提交的 Task 与 Relation |
| 执行期间修改计划 | 不修改 Definition；只提交预留决策 | 可追加 Task、子 Task 和 Relation |
| Relation 效果 | 约束推进 | 提供建议 |
| 历史 | 保留每个运行时 Task 实例和决策 | 保留所有已提交的结构与结果 |

两种模式都使用追加历史，不会为了表达新计划而重写已完成事实。

## 如何抵达完成

- **Workflow**：当已选路径上所有已产生 Task 结束，且不再有后续 Task 需要产生时完成。
- **Blackboard**：Task 收敛只产生完成判断机会。协作者必须显式提交完成结果，再按 `acceptance_mode` 立即完成或进入 Agent/Human 验收。

取消和失败是独立终态路径。任何 WorkItem 终态都必须结束活动 Claim，并阻止后续 Task 变更。

## 两种模式的边界

一个 WorkItem 只使用一种协调模式。Workflow 不在运行中引入任意新 Task，Blackboard 也不把建议 Relation 偷偷升级为强制依赖。

实际判断可以归结为一句话：Workflow 关心现在允许走哪条预定路径，Blackboard 关心基于当前认知，哪些工作仍值得继续。
