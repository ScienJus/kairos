# Kairos Blackboard 模式

有些工作目标很清楚，却没有诚实的方法预先画出完整路径。硬把它塞进固定 Workflow，只会把不确定性藏进含糊的步骤里。Blackboard 让目标保持稳定，同时允许计划随着证据逐步形成。

因此 WorkItem 可以在一个 Task 都没有时开始。人和 Agent 边做边加入 Task、层级与建议 Relation；但当他们认为目标已经达成时，仍需作出一次明确、可审核的完成判断。

## 可以生长的计划

Blackboard Definition 提供默认说明、Agent 指引、建议 tags 和验收策略，但不预定义完整 Task Graph。

WorkItem Version 是服务端维护的结构修订号。不同协作者并发追加不同 Task 或 Relation 时，操作可串行后全部成功；Task 自身生命周期仍由 Task Version 保护。创建操作使用 `operation_id` 处理响应丢失后的重放。

## 协作者如何改变计划

协作者可以：

- 在 WorkItem 下创建顶层 Task；
- 将已 Claim、尚未产生成果的 Task 拆分为初始子 Task；
- 在聚合 Task 尚未完成时追加子 Task；
- 在 Task 之间追加建议 Relation；
- 将失去价值的未领取 Task 标记为 Skipped。

拆分会结束父 Task 的 Claim 并使其进入 `waiting_children`。父 Task 不再产生自己的 Submission，子 Task 共同表达完成过程。

Harness 使用 Executor Credential 创建的 Task 和 Relation 一经提交就是共享事实，不因原 Claim 后续失败或释放而回滚。

## Relation 与候选：建议，而不是隐藏依赖

Blackboard Relation 只表示建议的推进顺序。它帮助执行者解释上下文，但不会因前置 Task 未完成而强制阻止后续 Task。已有 Relation 不更新或删除，计划变化通过追加新结构与 Skip 原因表达。

普通 Task 在下列条件下成为候选：

```text
state = pending
+ no active Claim
+ WorkItem permits execution
+ executor kind, Agent role, and queried tags match
```

空 Blackboard、Task 已收敛或待 Agent 验收时，WorkItem 本身产生协调候选。Agent 先建立 Coordination Claim，再分析完整上下文并完成创建、提交或验收决策。

## 审核单次结果

执行者可在提交时请求 Human Review，Human 也可以要求当前 Task 的下一次 Submission 进入 Review。

提交进入 Review 后，当前 Claim 结束，Task 处于 `in_review`。通过后完成；拒绝后回到 `pending`，保留全部 Submission、Review 与反馈历史。

## 判断目标已经完成

Task 全部完成或跳过只表示当前计划收敛，WorkItem 仍保持 `open`。协作者必须选择：

- 创建后续 Task，继续执行；
- 提交一份持久的 WorkItem 完成结果。

例如，一次调查的原定 Task 已全部完成，但最终证据表明仍需补做上线检查。自动完成会过早关闭 WorkItem；Blackboard 会留下一个协调决定：增加这项检查，或者说明现有结果为何已经充分并提交完成。

提交完成后才应用 `acceptance_mode`：

| 模式 | 结果 |
| --- | --- |
| `none` | 立即完成 |
| `agent` | 产生 Agent 验收候选 |
| `human` | 进入人工验收 |

验收者可以接受完成结果。Agent 验收者也可以创建新 Task，废弃当前完成提案并让 WorkItem 回到执行。

## Blackboard 不变量

- 已提交的 Task、Relation、Submission、Review 和 Artifact 不被新计划覆盖。
- Relation 始终是建议，不作为硬阻塞条件。
- 对空图、完成判断和 Agent 验收的分析都由 Coordination Claim 保护。
- WorkItem 只有在显式完成结果按验收策略通过后才完成。
