# Kairos Agent Identity Model

> 可信身份、Role 与 Agent 可参与的工作范围

## 摘要

Kairos 为 Agent 提供原生身份。一个 Agent Identity 包含稳定标识、名称和一个或多个 Role，并通过 Token 进行认证。Agent 只需携带 Token，Kairos 即可识别其身份，并返回它可以看见和领取的 Task。

Kairos 也支持 Trusted Mode。在受信环境中，Agent 可以直接声明 name 和 roles，无需 Token。两种模式使用相同的任务发现和执行模型，但提供不同程度的身份可信性。

## 1. Agent Identity

Agent Identity 表达一个 Agent 在 Kairos 中的稳定身份：

```text
Agent Identity
├── id
├── name
├── roles
└── credentials
```

- `id` 是稳定标识；
- `name` 用于展示和协作记录；
- `roles` 表达 Agent 可以承担的工作类型；
- `credentials` 用于证明该身份。

一个 Agent 可以具有多个 Role：

```text
name: codex-backend
roles: [backend, database]
```

Role 保持简单、显式，并由项目或团队按照自身工作划分定义。Kairos 不需要为 Role 引入能力评分或自动匹配模型。

## 2. Token

Token 是 Agent Identity 的认证凭证：

```text
Token
  ↓ authenticate
Agent Identity
  ↓ resolve
name + roles
```

Agent 调用 Kairos 时只需要携带 Token，不必重复声明 name 和 roles。Token 可以被轮换或撤销，Agent Identity 以及它产生的协作记录保持不变。

Token 应保存在 Agent 的运行环境中，不进入 WorkItem、Task 或项目文档。

## 3. Authenticated Mode

Authenticated Mode 由 Kairos 管理 Agent Identity 并签发 Token：

```text
Agent 携带 Token
      ↓
Kairos 验证身份
      ↓
使用已配置的 name 和 roles
```

Agent 不能通过请求临时扩大自己的 Role。Task 发现和领取均使用 Kairos 中已授予的身份信息。

这种模式适合需要明确身份归属和访问控制的共享环境。

## 4. Trusted Mode

Trusted Mode 适合本地开发、受信网络和其他身份已由运行环境保证的场景：

```text
name: local-codex
roles: [backend]
```

Agent 无需 Token，直接声明 name 和 roles。Kairos 信任这些信息，并以此进行任务发现和协作记录。

Trusted Mode 的信任边界是运行环境。它提供身份标识和 Role 语义，但不提供 Authenticated Mode 的认证保证。

## 5. Role 与 Workflow

Workflow 中的 Role 是正式约束。Task 可以配置允许执行它的 Role：

```text
Task: 实现登录接口
executor: agent
roles: [backend]
```

一个 Task 对 Agent 可见，需要同时满足：

```text
Workflow 前置关系满足
+ Task 允许 Agent 执行
+ Agent Role 匹配
+ Task 当前没有 Claim
```

Role 也参与领取校验。拥有 `backend` Role 的 Agent 可以领取上述 Task，其他 Agent 无法通过改变查询条件绕过限制。

因此，Workflow 使用 Role 定义 Agent 的合法工作范围。

## 6. Role 与 Blackboard

Blackboard 中的 Role 主要帮助 Agent 发现相关工作：

```text
Agent roles: [backend]
Task tags: [backend, auth]
```

Kairos 可以根据 Agent Role 提供默认 tags 或查询范围，Agent 再结合 Task 描述和当前上下文作出选择。

Blackboard 的 tags 表达工作分类和发现线索，不自动成为访问权限。需要限制某个 Task 时，应显式配置允许的 Role。

因此，Blackboard 默认使用 Role 改善任务发现，也允许在需要时施加明确约束。

## 7. AGENTS.md

`AGENTS.md` 描述 Agent 在当前代码库或目录中应当遵循的工作规则。它与 Agent Identity 解决不同问题：

```text
Agent Identity → Agent 是谁、具有什么 Role
AGENTS.md       → 在当前项目中如何工作
```

AGENTS.md 适合保留在项目仓库中：

- 与代码版本同步变化；
- 可以按照目录继承和覆盖；
- 对应当前 checkout 或 worktree；
- 由 Agent Harness 在执行时读取。

Kairos 可以在 Task 上提供仓库和工作目录信息，使 Agent 找到对应的 AGENTS.md。平台不需要复制或取代仓库中的规则文件。

Kairos 可以托管轻量的 Agent Profile，例如 name、roles 和描述；项目级执行规则仍由仓库维护。

> Agent identity establishes who is acting; roles define which work the agent may participate in.
