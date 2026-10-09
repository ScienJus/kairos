# Kairos 执行协作模型

协作需要容纳多个人和 Agent，又不能让责任变成模糊的“大家负责”。因此 Kairos 把“谁可以做”与“谁已经接手”分开：Task 配置描述执行资格，Claim 则指出当前真正负责的执行者。

执行结束后仍沿用同样的原则。Submission、Review、Failure 和 Artifact 记录发生过什么，不依赖产生它们的执行会话继续存在。

## 执行边界

Task 应当是一段连贯、可交付的工作：

```text
选择 Task → 建立 Claim → 执行 → 提交或结束责任
```

一个 Task 不用来表示整个团队的无边界工作，也不应需要多个执行者同时共享责任。需要并行或不同专长时，拆成多个 Task。

## 资格与责任：可以执行不等于已经负责

`executor` 限定 Task 允许 Human、Agent 或两者执行；`allowed_roles` 进一步限定 Agent Role。这些字段只定义资格集合，不代表已有人承担责任。

Claim 把资格变成实际责任：

- 一个 Task 同时最多只有一个 Active Claim；
- 所有变更都必须由当前 Claim 或明确的人工管理操作授权；
- Claim ID 是 fencing token，已结束 Claim 不能复活或覆盖新负责人的结果。

Human Claim 持续到提交、失败、释放或管理操作结束。Agent Claim 是可续租的 lease：heartbeat 延长租期，租期过后由 Core reaper 以事务结束 Claim 并重新开放 Task。时间到达本身不会在事务之外瞬间撤销责任。

这一区别在恢复时很重要。某个 Agent 停止 heartbeat 后，另一位执行者不能因为自己的时钟已经越过 `lease_until` 就直接接手；Core 必须先结束旧 Claim，新的 Claim 才能用 fencing 把遗留执行者挡在外面。

## Submission、Review 与 Failure

每次正式交付创建不可变的 Submission，并关联产生它的 Claim。重做会新建 Submission，不覆盖旧结果。

需要 Review 时，提交会结束 Claim，Task 进入 `in_review`。审核通过后 Task 完成；拒绝后回到 `pending`，由新 Claim 承担修改责任。

执行者无法完成时创建 Failure 并结束当前 Claim。后续是重试、等待人工还是结束 WorkItem，由协调模式和明确的失败动作决定；精确规则见 [Agent 交互模型](07-agent-interaction-model.zh-CN.md) 和 [API 参考](../api-reference.zh-CN.md)。

## 共享上下文属于工作

共享上下文归属 WorkItem 与 Task，不归属执行者会话。它由三类信息组成：

1. 意图：WorkItem 目标、约束、验收标准与 Task 说明；
2. 协调：相关 Task、Relation、可用决策和已有结果；
3. 历史：Claim、Submission、Review、Failure 和 Artifact。

执行者不需要读取无关的全部历史，但必须能获得完成当前 Task 所需的上游事实和反馈。

## 责任模型：三种接手方式

| 参与方式 | 责任建立过程 |
| --- | --- |
| Agent 主动参与 | Agent 发现匹配候选并创建 Claim |
| Human 执行 | Human 在界面中领取允许人工执行的 Task |
| Agent Daemon 派发 | Daemon 使用绑定 Identity 领取，再启动 Harness |

三种方式共用同一套 Claim、Submission 和失效语义。分发方式不改变执行责任。

## 运行边界：Core、Daemon 与 Harness

- **Kairos Core** 管理候选资格、Claim、领域生命周期和持久上下文。
- **Agent Daemon** 管理调度、续租、Harness 生命周期和结果收敛。
- **Agent Harness** 在受限凭据下完成具体工作，不拥有跨 Task 的协调权限。

简而言之，资格决定谁可以执行，Claim 记录谁正在负责，Submission 和 Artifact 则保留这个执行者交付了什么。
