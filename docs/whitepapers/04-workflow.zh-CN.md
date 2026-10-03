# Kairos Workflow 模式

Workflow 适合那些在执行前就能说明主要步骤和依赖的工作。这里的图不只是展示，它会决定哪些运行时 Task 可以出现，以及它们何时能够推进。

为了让这份承诺保持稳定，每个 WorkItem 都绑定一个不可变的 Definition 版本。后续编辑可以改善未来的工作，却不会悄悄改变正在执行的规则。

## Definition 与运行时事实

Workflow Definition 包含 Task Definition、Relation 和起始节点。WorkItem 创建时绑定 Definition ID 与 Version；新版本不会改变已运行 WorkItem。

Definition 是规则，运行时 Task 是事实。界面可以将两者合并展示，但未到达的 Definition 节点不是可 Claim 的 Task。循环节点可以产生多个 Task 实例，每个实例保留独立 Claim、Submission 和历史。

Relation 可以附带简短 `label` 和供执行者理解推进选择的 `agent_guidance`。Guidance 只解释已经合法的路径，不创建额外条件分支。

## Workflow 推进：图如何运行

普通无环关系表示：一个节点的所有具体前置实例结束后，该节点才能产生新 Task。并行分支独立推进，汇合节点等待它们对应的具体实例。

循环中的选择按结构编译为：

- 每条留在当前循环的出边各形成一个 Continue Group；
- 离开循环的出边合并为一个 Exit Group；
- Continue Group 与 Exit Group 互斥，执行者只选一组；
- Continue Group 的目标直接保留；Exit Group 内 required 目标自动保留，optional 目标由执行者判断。

循环必须有出口，运行时 Task Graph 只连接具体实例，因此历史始终为无环图。

例如，调查节点可以进入下一轮调查，也可以退出并进入发布。选择继续会创建新的运行时 Task 实例，而不是重新打开或覆盖上一轮。Definition 可以包含循环，记录下来的执行历史仍保持无环。

## Task Definition 控制什么

| 配置 | 含义 |
| --- | --- |
| `executor` | 允许 Human、Agent 或两者执行 |
| `allowed_roles` | 允许的 Agent Role；不限制 Human |
| required / optional | required 路径必须执行；optional 在配置的决策点可跳过 |
| Review policy | 不需要、执行者判断，或必须 Human Review |
| Artifact requirements | 声明 Submission 必须附带的交付物 |

Workflow 限制一个 Definition 的 Task/Relation 规模，并按每个 Definition 节点限制单个 WorkItem 中可产生的 Task 实例数。精确上限与请求约束以 [OpenAPI](../openapi.yaml) 为准。

## Task 何时成为候选

一个已产生 Task 需同时满足以下条件才能被领取：

```text
state = pending
+ no active Claim
+ WorkItem permits execution
+ executor kind and Agent role match
```

Workflow 候选不按 Blackboard tags 过滤。多个 Task 同时满足条件时，人、主动 Agent 或 Agent Daemon 可以选择任意合法候选并建立 Claim。

## Optional 与 Review：留给执行阶段的决定

执行者在提交当前 Task 时同时提交对本次可判断 optional 目标的意图。Kairos 根据 Definition 分区和展开路径，执行者不直接构造新图。

当多条前置路径汇合到同一 optional 目标时，所有相关路径都选择跳过才会跳过；任一路径选择保留就产生 Task。

这样，一条分支就不能丢弃另一条分支仍然需要的工作。若两项分析汇合到一个 optional 验证 Task，只有其中一项选择跳过，并不足以覆盖另一项的保留决定。

Review 作用于当前 Submission。需审核时，提交结束 Claim 并进入 `in_review`；通过后才应用结果和推进决策，拒绝后回到 `pending` 并由新 Claim 修改。

## 完成与恢复

当已选路径上所有已产生 Task 结束，且没有后续 Task 需要产生时，WorkItem 完成。未通过 Review 的结果和未定案 optional 决策不参与推进。

Workflow 失败后有两种 Human 恢复路径：

- **继续执行**：保留当前 WorkItem、成功分支和历史，为当前失败或中断的工作创建替代 Task 实例；
- **从头执行**：使用同一 Definition 版本和原始目标创建新 WorkItem，只携带有界失败摘要与 Human 说明。

两种路径都不复活旧 Claim，也不提供任意阶段回放。重试指引、实例上限、响应与冲突语义见 [API 参考](../api-reference.zh-CN.md)。

## Workflow 不变量

- WorkItem 始终绑定同一 Definition 版本。
- 只有结构允许的 Task 才会被产生和领取。
- 执行者只在 Definition 预留的决策空间内自主判断。
- 历史 Task 实例、Submission、Review 和 Failure 不因后续推进或恢复被覆盖。
- 运行时扩展的存储与查询边界见[性能测量](../workflow-runtime-performance.md)。
