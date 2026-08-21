# Kairos Workflow 模式

> 预定义 Task Graph 中的约束与执行者自主性

## 摘要

Workflow 使用带版本的正式定义组织一个 WorkItem。WorkItem 创建时绑定固定的 Workflow ID 与 Version，系统在执行过程中按需产生 Task，并根据正式关系决定如何继续推进。

Workflow 也可以显式保留执行者的判断空间。每个 Task 可以配置执行者类型与 Role、是否允许跳过，以及是否需要人工 Review。执行者在完成当前 Task 时可以同时作出推进决策；执行者为 Agent 时，无需为判断 optional Task 额外启动一次 Agent。

## 1. Workflow 结构

WorkItem 创建时绑定一个已发布的 Workflow Definition ID 与 Version。这个绑定在 WorkItem 生命周期内保持不变，Workflow 后续发布的新版本不会改变已经开始的工作。

Workflow Definition 还可以提供作用于全部运行时 Task 的 Agent Instructions 与 Suggested Tags。Suggested Tags 由执行者用于动态标注具体 Task，不参与 Workflow 前置关系和候选资格计算。

Workflow Graph 由起点、Task Definition、单向 Relation 和 `MaxTaskExecutions` 组成。一个 Workflow 可以有多个起点；WorkItem 创建时同时产生全部起始 Task，因此起始 Task 必须是 required。Task Definition 可以配置 Default Tags，系统在产生运行时 Task 时复制这些标签，执行者仍可按实际情况调整。

Relation 可以配置可选的 `Label` 与 `AgentGuidance`。`Label` 是图上显示的简短交接提示；`AgentGuidance` 进入当前 Task 的 Workflow execution context，帮助执行者判断已有的 optional、continue 或 exit 决策。两者都可以留空，尤其是没有判断空间的简单单通路。Guidance 只解释编译后已经合法的推进方式，不会把普通 Relation 变成条件分支，也不会改变 required、optional、并行或循环语义。

运维 UI 会把不可变的 Definition Graph 与运行时 Task、Relation 合并投影。尚未产生运行时 Task 的 Definition 节点显示为“尚未到达”；它们只用于展示，不能 Claim，也不能打开 Task execution context。完整图展示不会预先创建 Task，也不会改变 Workflow Activation 与 Transition 语义。循环 Relation 保留为返回边；同一循环 Definition 节点的多次运行会在主图节点上汇总执行次数。选择节点时默认打开最新的运行时 Task，并可使用上一项和下一项控件逐个查看保留在执行历史中的具体实例。

Workflow Definition 描述可以重复到达的任务节点和推进关系，运行时则从定义的起点开始，在到达相应节点时产生具体 Task：

```text
Workflow Definition：设计 → 实现 → 测试

WorkItem Runtime：设计 #1 → 实现 #1 → 测试 #1
```

每个 Task 实例具有独立的 Claim、进展和成果。系统在前一批 Task 正式结束后产生后续 Task；运行时的一个 Task 实例具有多个前置 Task 时，默认等待这些具体实例全部结束。

每个 Task Definition 还可以声明具名 Artifact 交付指引。运行时 Submission 必须包含 Definition 声明的每个名称。契约用 Description 指导执行者，但不规定文件类型或存储方式，同时允许额外 Artifact。

Kairos 使用内部的 Workflow Task Activation 汇聚同一次展开产生的前置结果。Activation 通过 correlation 区分并行分支和不同循环轮次；输入全部确定后才产生可执行的 Task，它本身不会被执行者看到或 Claim。

Workflow Definition 可以包含循环：

```text
实现 → 测试
 ↑      │
 └──────┘
```

再次经过同一个定义节点时，系统创建新的 Task 实例：

```text
实现 #1 → 测试 #1 → 实现 #2 → 测试 #2
```

Workflow 发布时，系统根据图结构为每个 Task 推导推进选择：每条留在当前循环中的出边分别形成一个 Continue Group，离开循环的出边合并为一个 Exit Group。多个 Continue Group 与 Exit Group 互斥；执行者选择其中一个组。循环内出边不表达并行或前置关系。普通无环节点只有一个 Exit Group。

选择 Continue Group 即表示保留并产生其目标 Task，该次激活不再应用目标 Task 的 optional 配置。未选择的 Continue Group 不产生 Task。

选择 Exit Group 后，其中的 required Task 自动产生，optional Task 仍由执行者判断是否保留。循环必须存在出口；`MaxTaskExecutions` 限制一个 WorkItem 最多产生的 Task 实例总数，只作为失控保护。配置为零时使用系统默认值，不表示无限。运行时 Task Graph 只连接具体实例，因此始终记录为无环的执行历史。

执行者提交 Task 时，Kairos 保存一条 Transition Decision，记录选择的 Group、触发或跳过的 Relation、执行者和理由。需要 Review 时，Decision 暂不应用；Review 通过后再应用并产生下游 Task。被驳回的 Decision 作为未应用历史保留，同一个运行时 Task 最多应用一条 Decision。Decision、Activation、下游 Task 与 Task Relation 在同一事务中更新。

并行前置关系仍按实例聚合：

```text
前端实现 ─┐
后端实现 ─┼→ 集成测试
编写文档 ─┘
```

Task 的前置关系只有一种统一含义：全部前置 Task 均已完成或跳过，当前 Task 才能继续推进。

## 2. Task 配置

Workflow 为每个 Task 配置四项设置：

```text
executor:
  agent
  human
  either

roles:
  - backend

execution:
  required
  optional

review:
  none
  executor_decides
  required
```

`executor` 定义 Task 可以由 Agent、人或两者中的任意一方执行。

`roles` 限定可以发现和领取该 Task 的 Agent Role。人工 Task 不受 Agent Role 影响。

`execution` 定义 Task 是否允许跳过：

| 配置 | 语义 |
| --- | --- |
| `required` | Task 必须执行 |
| `optional` | 执行者可以保留或跳过 Task |

optional 配置应用于 Exit Group 中的 Task。Task 通过 Continue Group 被选择时，该选择本身构成 keep 判断，Task 直接产生。

没有前置 Task 的起始 Task 必须配置为 `required`。每个 optional Task 至少具有一个前置 Task，其是否执行由前置执行者判断。

`review` 定义 Task 结束前的人工 Review 要求：

| 配置 | 语义 |
| --- | --- |
| `none` | 无需人工 Review |
| `executor_decides` | 执行者判断是否请求人工 Review |
| `required` | 必须通过人工 Review |

这些配置定义执行者可以作出判断的位置，同时保持 Workflow 的整体结构稳定。

## 3. 候选 Task

一个 required Task 在满足以下条件后进入候选集合：

```text
所有前置 Task 已完成或跳过
+ 当前 Task 尚未结束
+ 当前没有 Claim
+ 执行者类型匹配
+ 执行者为 Agent 时 Role 匹配
```

多个 Task 同时满足条件时，系统返回多个候选。人或 Agent 可以主动选择，Bridge 也可以派发。

```text
[前端实现, 后端实现, 编写文档]
```

未被选择的 required Task 继续保留在候选集合中，直到被执行。optional Task 也可以由执行者决定跳过。

## 4. Optional Task 的推进

每个前置 Task 结束时，都附带对其所连接 optional Task 的判断。该判断由完成前置 Task 或决定跳过它的执行者给出；执行者为 Agent 时不会增加 Agent 调用。

当 optional Task 的所有前置 Task 都已结束后，系统聚合各个执行者的判断：

- 任意执行者选择保留：Task 进入候选集合；
- 所有执行者都选择跳过：形成跳过决定；
- 某个执行者未给出判断：默认保留 Task。

```text
前端执行者：跳过 ─┐
后端执行者：保留 ─┼→ 编写文档进入候选集合
设计执行者：跳过 ─┘
```

跳过采用一致同意原则：

```text
keep = OR(keep₁, keep₂, ..., keepₙ)
skip = AND(skip₁, skip₂, ..., skipₙ)
```

一个 optional Task 只有一个前置 Task 时，无需等待其他执行者的判断，相关 Review 要求满足后即可生效。连续出现多个 optional Task 时，同一个执行者可以在一次推进中依次判断：

```text
后端实现
    ↓
编写文档（optional） → 跳过
    ↓
更新示例（optional） → 跳过
    ↓
集成测试（required） → 进入候选集合
```

执行者将这些判断作为 Skip Intent 随当前提交保存，只需列出本次允许跳过的 optional Task。Kairos 根据 Workflow Definition 将其应用到当前可达路径；遇到需要执行的 Task 即停止继续应用。并行路径独立推进，多条路径汇合时采用一致同意原则。任一路径需要 Review 时，该路径等待 Review 通过后继续推进。

## 5. Review

执行者提交当前 Task 时，根据 Review 配置决定后续过程：

```text
提交 Task
    ↓
Review Policy
 ├── none ───────────→ 结束
 ├── executor_decides → 结束 / Review
 └── required ───────→ Review

Review 通过 ─────────→ 结束
Review 驳回 ─────────→ Pending → 重新 Claim
```

Review 是同一个 Task 的状态。执行者提交 Review 时，系统从当前 Claim 创建不可变的 Task Submission，Review 关联该 Submission，随后结束 Claim 并将 Task 置为 `InReview`。等待 Review 期间没有 Active Claim，Reviewer 处理审核记录，不领取该 Task。

每次 Review 请求、决定和反馈都记录在当前 Task 下，并按时间顺序保留为完整审核历史。Task 上下文向执行者提供全部 Review 记录。Review 通过后 Task 正式结束；Review 驳回后 Task 回到 `Pending`，由原执行者或其他执行者重新 Claim，并在完整审核历史上继续处理。

optional Task 的跳过也是一种结束决定，其 Review 配置以相同方式生效：

- `none`：直接跳过；
- `executor_decides`：任意前置执行者请求人工确认时进入 Review；
- `required`：人工确认后跳过。

跳过决定 Review 通过后，optional Task 标记为 Skipped；Review 驳回后，该 Task 被保留并进入候选集合。Review 检查的是前置执行者作出的跳过决定，不会为 optional Task 建立 Claim。

每个执行者对 optional Task 的判断随当前 Task 一并提交，并在当前 Task 所需的 Review 通过后参与聚合。跳过决定所需的 Review 通过后，Workflow 继续推进；执行者为 Agent 时无需再次启动 Agent。

## 6. 执行者自主性

Workflow 通过配置明确执行者的判断空间：

```text
人类定义 Task Graph 和策略
            ↓
系统保证前置关系
            ↓
执行者判断 optional Task 与 Review
```

执行者的自主性体现在：

- 从多个候选 Task 中选择工作；
- 判断当前 Task 所连接的 optional Task 是否值得执行；
- 在 `executor_decides` 模式下判断是否需要人工 Review。

前置依赖、required Task 和 required Review 仍然由 Workflow 强制保证。执行者为 Agent 时，这些决策体现 Agent 自主性。

## 7. WorkItem 完成

Task 有两种结束结果：

```text
Completed
Skipped
```

需要 Review 的结果在 Review 通过后才正式生效。所有已经产生的 Task 均已完成或跳过，并且 Workflow 已没有后续 Task 需要产生时，WorkItem 完成：

```text
∀ Runtime Task: Completed or Skipped
+ No Next Task
            ↓
    WorkItem Completed
```

Task 实例总数达到 `MaxTaskExecutions` 时，WorkItem 进入 Failed。

> Workflow defines the constraints; executor autonomy operates at explicitly configured decision points.
