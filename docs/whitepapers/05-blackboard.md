# Kairos Blackboard

> 执行过程中持续形成的共享 Task Graph

## 摘要

Blackboard 从明确的 WorkItem 目标开始，Task Graph 可以为空或不完整。人和 Agent 在执行过程中共同创建、选择和调整 Task，使计划随着工作认知持续演化。

Task Relation 在 Blackboard 中表达推进建议。它帮助执行者理解工作结构，同时保留对执行顺序和下一步工作的判断权。

## 1. Blackboard 结构

Blackboard Definition 定义一个共享协作空间，包括名称、说明、Agent Instructions 与 Suggested Tags。它不预先定义 Task Graph。每个 WorkItem 绑定一个固定的 Definition Version，并在这个空间内提供自己的目标、背景、约束和验收标准：

```text
WorkItem：实现登录功能
Tasks：[]
```

协作者根据当前理解建立初始 Task：

```text
[ ] 设计登录方案
[ ] 实现登录功能
[ ] 测试登录功能
```

执行过程中发现的新信息会继续改变结构：

```text
[x] 设计登录方案
[ ] 实现密码登录
[ ] 实现会话管理
[ ] 增加暴力破解防护
[ ] 测试登录功能
```

Blackboard 中的 Task Graph 是当前工作认知的共享表达。

Blackboard 的结构追加基于服务端最新状态提交。多个协作者同时创建不同 Task 或 Relation 时，操作依次写入并可以全部成功；WorkItem Version 作为服务端维护的结构修订号。Operation ID 负责识别请求重试，Task Version 负责保护单个 Task 的状态变化。

Task Graph 为空时，WorkItem 本身作为候选工作被发现。协作者读取整体目标和 Blackboard Instructions 后创建首个 Task；WorkItem Tags 用于这种初始发现。

Suggested Tags 提供开放的标签词汇，例如 `module:*` 或 `kind:*`。Agent 在创建和推进 Task 时根据实际内容选择具体 tags；这些建议不构成权限或格式约束。

## 2. 规划与执行

Blackboard 将规划放在整个执行过程中：

```text
观察当前工作
      ↓
创建或调整 Task
      ↓
选择并执行 Task
      ↓
记录进展与成果
      ↓
重新观察 WorkItem
      ↺
```

协作者可以：

- 创建新的 Task；
- 将较大的 Task 拆分为更清晰的交付单元；
- 向尚未完成的聚合 Task 追加子 Task；
- 调整 Task 之间的关系；
- 根据新信息将已经失去价值的 Task 标记为 Skipped；
- 使用已有成果规划后续工作。

已完成 Task 及其成果继续保留，为后续判断提供上下文。

Task 可以形成层级。执行者 Claim 一个尚未产生成果的 Task 后，可以将它拆分为初始子 Task。父 Task 随即结束 Claim 并进入 `WaitingChildren`，不再产生自己的 Submission；成果由后代 Task 汇总。

`WaitingChildren` 表示一个开放的聚合范围。WorkItem 未完成期间，协作者可以继续向其中追加子 Task。所有直接子 Task 完成或跳过后，父 Task 递归完成并封闭。普通执行 Task、聚合 Task 与 Task Relation 分别表达执行、工作拆分和建议顺序。

## 3. Task Relation

Blackboard 使用 Task Relation 表达当前建议的推进顺序：

```text
设计 ⇢ 实现 ⇢ 测试
```

前置 Task 尚未完成时，后续 Task 仍然可以成为候选。执行者会同时看到建议关系和相关前置成果，并根据实际情况决定是否开始工作。

例如，实现 Task 可以在设计尚未完全结束时提前开始；后续协作者也可以调整原有关系，反映新的工作认知。

> Task Relation records shared judgment about how work should proceed.

## 4. Task 发现与执行

Blackboard 的候选 Task 来自当前共享空间：

```text
Pending 的执行叶子 Task
+ 当前没有 Claim
+ 符合查询上下文
```

查询上下文可以包含 tags、执行者类型以及 WorkItem 范围。例如，一个 Agent 可以寻找带有 `backend` 和 `auth` tags 的 Task，人也可以通过界面查看适合人工处理的 Task。

Task 可以配置执行者类型：

```text
executor:
  agent
  human
  either
```

人或 Agent 可以主动选择候选 Task，Bridge 也可以派发。Claim 为选中的 Task 建立唯一的执行责任。

## 5. 自主性

Blackboard 将规划自主性持续开放给协作者：

- 判断当前哪些工作值得执行；
- 创建遗漏的 Task；
- 调整工作拆分和建议关系；
- 根据成果重新规划下一步；
- 判断是否需要人工 Review。

人工 Review 可以由执行者在提交成果时请求，也可以由人在 Task 正式结束前要求下一次提交进入 Review。Review 作用于当前 Task，不要求在 Blackboard 初始结构中预先配置。

执行者提交成果并发起 Review 时，系统从当前 Claim 创建不可变的 Task Submission，Review 关联该 Submission，随后结束 Claim 并将 Task 置为 `InReview`。每次 Submission、Review 决定和反馈都按时间顺序保留，全部进入该 Task 的共享上下文。审核期间没有 Active Claim，Reviewer 处理审核记录，不领取一个新的 Task。Review 通过后 Task 正式结束；Review 驳回后 Task 回到 `Pending`，由原执行者或其他执行者重新 Claim。

其他未结束且未被 Claim 的 Task 仍可继续执行。Blackboard 的 Task Relation 是推进建议，因此某个 Task 正在 Review 不会自动阻止其他 Task 成为候选。

Blackboard 的自主性来自持续规划，因此无需通过预配置的 optional Task 标记来预留跳过位置。协作者只创建当前认为有价值的 Task，也可以在判断改变后将已有 Task 标记为 Skipped 并记录原因。

## 6. WorkItem 完成

执行者在结束当前 Task 前，根据 WorkItem 的目标和验收标准判断是否需要继续扩展工作：

```text
结束当前 Task 前检查 WorkItem 目标
 ├── 仍需推进 → 先创建后续 Task
 └── 已经满足 → 结束当前 Task
                       ↓
             所有 Task 均已结束
                       ↓
               WorkItem 完成
```

新的发现可以随时扩展 Task Graph，目标已经满足时则可以将剩余的低价值 Task 标记为 Skipped。最后一个 Task 完成或跳过后，WorkItem 自动完成。空 Blackboard 也可以由协作者直接确认完成。

> Blackboard grows a shared plan while people and agents execute the work.
