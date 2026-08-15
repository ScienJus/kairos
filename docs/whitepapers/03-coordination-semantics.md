# Kairos Coordination Semantics

> Workflow 与 Blackboard 如何从同一个 Task Graph 产生候选工作

## 摘要

Workflow 和 Blackboard 共享相同的 WorkItem、Task 与 Task Relation。两者的核心差异在于 Task Graph 具有怎样的权威性，以及系统如何据此产生当前候选 Task。

Workflow 将图解释为正式计划，Blackboard 将图解释为协作者对工作的当前共同判断。人和 Agent 在两种模式下都可以从候选集合中选择工作。

## 1. 统一形式

一个 WorkItem 内的工作结构可以表示为：

```text
G = (T, R)

T = Task 集合
R = Task Relation 集合
```

协调模式根据 Task Graph 和当前上下文产生候选集合：

```text
Candidates = Coordination(mode, G, context)
```

候选集合只定义当前的选择空间。人或 Agent 可以主动选择，Bridge 也可以派发；Claim 随后建立唯一的执行责任。

## 2. Workflow 候选语义

Workflow 将 Task Relation 作为正式约束。一个 Task 进入候选集合，需要满足其前置关系：

```text
Cworkflow(actor) = {
  t ∈ T |
  unfinished(t)
  ∧ unclaimed(t)
  ∧ prerequisites_satisfied(t)
  ∧ (required(t) ∨ kept(t))
  ∧ executor_matched(t, actor)
  ∧ role_matched(t, actor)
}
```

`kept(t)` 表示 optional Task 已由前置执行者决定保留；Role 约束只对 Agent 执行者生效。

例如：

```text
设计 ──→ 实现 ──→ 测试
```

“设计”完成后，“实现”进入候选集合；“实现”完成后，“测试”进入候选集合。一个 Task 连接多个后续 Task 时，可以同时产生多个候选 Task。

Workflow 负责保证候选集合符合正式计划，执行者继续在合法范围内作出选择。

## 3. Blackboard 候选语义

Blackboard 将 Task Relation 作为共享的推进建议。候选集合主要来自未完成、未被 Claim 且符合当前查询上下文的 Task：

```text
Cblackboard = {
  t ∈ T |
  unfinished(t)
  ∧ unclaimed(t)
  ∧ matches(t, context)
}
```

例如：

```text
设计 ⇢ 实现 ⇢ 测试
```

“设计”尚未完成时，“实现”仍可作为候选 Task，同时携带“建议等待设计完成”的信息。执行者结合 WorkItem 目标、已有成果、建议关系和自身上下文作出判断。

Blackboard 通过共享信息帮助执行者理解选择空间，Task Graph 也会随着这些判断持续演化。

## 4. 图的权威性与演化

| 维度 | Workflow | Blackboard |
| --- | --- | --- |
| 图的角色 | 正式计划 | 当前共同判断 |
| Relation 语义 | 执行约束 | 推进建议 |
| Task 产生方式 | 来自流程定义 | 协作者动态创建 |
| 结构修改 | 按正式规则进行 | 随工作认知持续调整 |
| 系统职责 | 执行计划并限定合法候选 | 保存计划并提供共享态势 |

Workflow 的典型过程是：

```text
绑定 Workflow Version → 按需产生 Task → 执行 → 继续推进 → 完成
```

Workflow Definition 可以包含循环。每次再次经过同一个定义节点时，系统创建新的 Task 实例，运行时 Task Graph 因而保持为实际执行历史。Workflow 的推进选择决定继续循环或退出，Task 实例总数上限只作为失控保护；超过上限时 WorkItem 失败。

Blackboard 形成持续反馈循环：

```text
规划 → 执行 → 观察 → 调整规划
 ↑                       ↓
 └───────────────────────┘
```

因此，Blackboard 的动态性贯穿整个执行过程。协作者一边完成 Task，一边完善对 WorkItem 的结构化理解。

## 5. 完成语义

两种模式采用相同的完成条件：

```text
所有已产生的 Task 均已完成或跳过
+ 没有待处理的结构推进
                ↓
        WorkItem 完成
```

Workflow 的结构推进由 Definition、路径选择和循环状态决定。Blackboard 的执行者在结束当前 Task 前判断是否需要扩展计划；需要继续时先创建后续 Task，没有后续工作时最后一个 Task 的结束会完成 WorkItem。空 Blackboard 可以由协作者直接确认完成。

协调模式决定 Task 如何产生，WorkItem 的闭合条件保持一致。

## 6. 模式边界

一个 WorkItem 在同一时刻采用一种 Coordination Mode，其 Task Relation 由该模式统一解释：

```text
Workflow  → Relation 表示正式约束
Blackboard → Relation 表示推进建议
```

统一解释可以保持两种模式的语义清晰，也让同一个底层 Task Graph 无需为每条关系配置独立的协调策略。

Workflow 与 Blackboard 的选择取决于工作本身：

- 工作顺序需要系统保证时，使用 Workflow；
- 执行过程需要协作者持续发现和调整计划时，使用 Blackboard。

## 7. 核心结论

Kairos 的协调语义可以归纳为：

1. Workflow 和 Blackboard 共享相同的 Task Graph 结构。
2. Workflow 使用强约束计算合法候选 Task。
3. Blackboard 使用共享结构和上下文提供候选 Task 与推进建议。
4. 两种模式都保留执行者对具体 Task 的选择权。
5. 一个 WorkItem 的 Task Relation 在同一时刻采用统一的模式语义。

> Workflow executes an authoritative graph; Blackboard evolves a shared graph while executing the work.
