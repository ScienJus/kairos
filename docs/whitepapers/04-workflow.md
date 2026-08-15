# Kairos Workflow

> 预定义 Task Graph 中的约束与执行者自主性

## 摘要

Workflow 使用预先配置的 Task Graph 组织一个 WorkItem。Task Relation 表达前置依赖，系统据此产生当前可执行的 Task。

Workflow 也可以显式保留执行者的判断空间。每个 Task 可以配置执行者类型与 Role、是否允许跳过，以及是否需要人工 Review。执行者在完成当前 Task 时可以同时作出推进决策；执行者为 Agent 时，无需为判断 optional Task 额外启动一次 Agent。

## 1. Workflow 结构

Workflow 由 WorkItem 下的 Task 及其前置关系组成：

```text
设计
 ├──→ 前端实现
 ├──→ 后端实现
 └──→ 编写文档
```

一个 Task 完成后，它连接的后续 Task 可能进入候选集合。一个 Task 具有多个前置 Task 时，默认等待全部前置 Task 结束：

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
Review 驳回 ─────────→ 继续处理原 Task
```

Review 是同一个 Task 的状态。人工 Reviewer 检查执行者提交的结果，Task 的执行责任仍然归属于原执行者。

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

需要 Review 的结果在 Review 通过后才正式生效。所有 Task 均已完成或跳过时，WorkItem 完成：

```text
∀ Task: Completed or Skipped
            ↓
     WorkItem Completed
```

> Workflow defines the constraints; executor autonomy operates at explicitly configured decision points.
