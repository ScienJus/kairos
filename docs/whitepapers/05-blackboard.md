# Kairos Blackboard

> 执行过程中持续形成的共享 Task Graph

## 摘要

Blackboard 从明确的 WorkItem 目标开始，Task Graph 可以为空或不完整。人和 Agent 在执行过程中共同创建、选择和调整 Task，使计划随着工作认知持续演化。

Task Relation 在 Blackboard 中表达推进建议。它帮助执行者理解工作结构，同时保留对执行顺序和下一步工作的判断权。

## 1. Blackboard 结构

一个 Blackboard 至少包含 WorkItem 的目标、背景、约束和验收标准：

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
- 调整 Task 之间的关系；
- 根据新信息将已经失去价值的 Task 标记为 Skipped；
- 使用已有成果规划后续工作。

已完成 Task 及其成果继续保留，为后续判断提供上下文。

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
尚未结束
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

人工 Review 可以由执行者在提交成果时请求，也可以由人直接发起。Review 作用于当前 Task，不要求在 Blackboard 初始结构中预先配置。

Blackboard 的自主性来自持续规划，因此无需通过预配置的 optional Task 标记来预留跳过位置。协作者只创建当前认为有价值的 Task，也可以在判断改变后将已有 Task 标记为 Skipped 并记录原因。

## 6. WorkItem 完成

当前所有 Task 结束后，协作者根据 WorkItem 的目标和验收标准重新判断整体工作：

```text
当前 Task 均已结束
        ↓
检查 WorkItem 目标
 ├── 已满足 → WorkItem 完成
 └── 未满足 → 创建新的 Task
```

这个检查也可以在执行过程中随时发生。新的发现可能扩展 Task Graph，目标已经满足时则可以将剩余的低价值 Task 标记为 Skipped。

> Blackboard grows a shared plan while people and agents execute the work.
