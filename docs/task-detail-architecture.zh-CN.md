# Task Detail 展示与操作架构

> 状态：主体已实现。本文只记录前后端边界与投影原则；精确响应结构以 [OpenAPI](openapi.yaml) 为准。

## 1. 问题

Task 页同时服务两种目的：

1. 查看一个 Task 的责任、结果与历史；
2. 在有资格时领取或执行该 Task。

`GET /api/v1/tasks/{id}/context` 是受执行资格保护的执行上下文。如果普通详情也依赖它，Human 就无法查看自己无权执行的 Agent Task，Review 也会被错误绑定到原执行权限。

## 2. 接口分工

| 接口 | 目的 | 权限 |
| --- | --- | --- |
| `GET /api/v1/tasks/{id}` | 稳定的 Task Detail 投影 | 允许查看 Task，不要求执行资格 |
| `GET /api/v1/tasks/{id}/context` | 完成 Task 所需的执行上下文 | 执行者资格与 Claim scope |
| Claim / submit / fail / review / planning 命令 | 改变领域状态 | 每个命令独立重新校验 |

查看、执行和审核是三种不同能力。前端不得因为一个执行上下文请求失败，就隐藏基本详情或 Review。

## 3. Detail 投影

Detail 由后端解释生命周期，前端只负责呈现：

| 区块 | 含义 |
| --- | --- |
| `responsibility` | 执行者类型、允许 Role、当前或最后一次 Claim 责任 |
| `outcome` | 最新有效交付、完成/跳过/失败摘要 |
| `current_review` | 当前待决定 Review，不存在时为 `null` |
| `history` | 规范化的 Claim、Submission、Review、Failure 和 Transition Decision |
| `artifacts` | 属于该 Task 的已提交 Artifact |
| `capabilities` | 当前查看者在当前版本下可执行的操作 |

集合字段始终为 `[]`；可选单值才使用 `null`。Responsibility 和 Outcome 是读模型，不用于反向重建持久对象。

## 4. Capabilities

Capabilities 用于告诉前端当前可以展示哪些操作，例如 Claim、Submit、Release、Fail、Review、Decompose、Append Child 或 Skip。

它们必须：

- 与命令复用同一应用层校验；
- 只表达当前身份和当前数据版本的快照；
- 不包含授权 secret，也不替代命令端重新校验；
- 在操作成功或冲突后通过重新读取刷新。

前端不扫描 history 来推导当前责任、待审核状态或命令资格。

## 5. 页面数据流

```text
打开 Task
  ├── 立即加载 Detail
  └── 用户展开执行操作
        └── 按需加载 Execution Context
```

- Detail 失败时显示真实读取错误。
- Execution Context 失败只影响依赖它的操作，不覆盖 Detail。
- 没有 capability 与请求失败是不同状态，不得用同一空态表示。
- Mutation 失败保留输入；成功后只清理对应表单，再刷新 Detail 和当前 WorkItem。
- 切换 Task 时以 Task ID 重建局部状态，避免草稿串到另一对象。

## 6. 关键状态投影

| Task 状态 | 主要呈现 | 可能操作 |
| --- | --- | --- |
| `pending` | 尚未建立责任 | Claim；Blackboard 可 Skip |
| `working` | Active Claim 与 executor | owner 可 Submit / Release / Fail / Decompose |
| `waiting_children` | 子 Task 收敛进度 | 允许时 Append Child |
| `in_review` | 当前 Submission 与 Review | Human reviewer 可决定 |
| 终态 | 最终 Outcome 和完整历史 | 无 Task 执行变更 |

Workflow 未到达的 Definition 节点不是 Task，不打开 Task Detail。历史实例可查看，但 capabilities 保持关闭。

## 7. 实现状态

已实现 Detail API、Responsibility/Outcome/History/Capabilities 投影、Human 查看 Agent Task、按需加载 Execution Context，以及 Detail 与操作错误隔离。

尚未完成的增强仅包括：Review Actor 投影仍为 ID 字符串，部分 Workflow/Blackboard 关系原因和 capability 禁用原因尚未进入 Detail。
