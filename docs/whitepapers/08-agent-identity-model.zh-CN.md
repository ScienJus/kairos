# Kairos Agent 身份模型

> Kairos 如何确认正在行动的是哪个 Agent，以及它可以领取哪些 Task

## 摘要

每次收到 Agent 请求时，Kairos 都要回答两个问题：是谁在行动，它可以领取哪些工作？Agent Identity 提供稳定标识和一个 Role。在 Authenticated Mode 下，Token 用来证明身份，Kairos 只返回这个 Agent 有资格发现和领取的 Task。

本地或已经受信的环境可以使用 Trusted Mode，由运行环境直接提供 id 和 role。两种模式下的 Task 发现和执行方式相同，区别只在于身份由谁提供、可信程度有多高。

## 1. Agent Identity

Agent Identity 表达一个 Agent 在 Kairos 中的稳定身份：

```text
Agent Identity
├── id
├── role
└── credentials
```

- `id` 是稳定且可读的标识，同时用于展示和协作记录；
- `role` 表达 Agent 可以承担的工作类型；
- `credentials` 用于证明该身份。

`id` 不应随意改名，因为 Claim 所有权、幂等记录和协作历史都引用它。未来如果需要可变展示信息，可以在 Agent Profile 中增加独立的展示标签，而不改变身份标识。

一个 Agent Identity 只具有一个 Role：

```text
id: codex-backend
role: backend
```

Role 保持简单、显式，并由项目或团队按照自身工作划分定义。需要另一 Role 时创建新的 Agent Identity，避免一个 Token 隐含多组授权。Kairos 不需要为 Role 引入能力评分或自动匹配模型。

## 2. Token

Token 是 Agent Identity 的认证凭证：

```text
Token
  ↓ authenticate
Agent Identity
  ↓ resolve
id + role
```

Agent 调用 Kairos 时只需要携带 Token，不必重复声明 id 和 role。服务只保存 Token Hash，明文仅在签发或轮换时返回。Token 可以被轮换或撤销，Agent Identity 以及它产生的协作记录保持不变。

Token 应保存在 Agent 的运行环境中，不进入 WorkItem、Task 或项目文档。

## 3. Authenticated Mode

Authenticated Mode 由 Kairos 管理 Agent Identity 并签发 Token：

```text
Agent 携带 Token
      ↓
Kairos 验证身份
      ↓
使用已配置的 id 和 role
```

Agent 不能通过请求临时改变自己的 Role。Task 发现和领取均使用 Kairos 中已授予的身份信息。Authenticated Mode 忽略 Trusted Mode 身份头，只接受 Bearer Token。

这种模式适合同一可信协作群体，用于明确身份归属和执行具体操作时的约束。它不提供租户、Team、项目或对象级数据隔离：所有已签发身份都属于同一个全局信任域。互不信任的群体需要分别部署 Kairos 实例。未来可以通过 Team 模型引入隔离边界，但这不属于当前身份契约。

在 Authenticated Mode 下，通过现有登录框使用部署配置的 `KAIROS_ADMIN_TOKEN` 登录，再从账户菜单中唯一的 **Token 管理** 入口打开 `/admin/identities`。在同一页面创建 Human（无角色）或 Agent（必填一个角色，例如 `developer`）、查看身份元数据、轮转和撤销已签发的 Token。轮转和撤销需要确认，旧 Token 立即失效。部署管理的 Admin 凭据在此只读，应通过部署配置更换。普通 Identity Token（包括 `initial-human.token`）不能访问管理功能。`/session` 返回 `can_manage_identities`，仅当凭据为部署 Admin 且身份管理可用时为 true；前端不通过 ID、角色或显示名称推断权限，各管理端点仍独立验证凭据。

管理页面复用当前标签页 sessionStorage 中的登录凭据，不建立第二套管理员会话。退出和当前凭据的 401 清除登录及工作区缓存。新签发的 Token 仅保存在页面内存，不进入 URL、浏览器存储或 Query/Mutation 缓存。请在关闭结果、开始其他操作、离开或刷新页面之前复制保存；剪贴板失败时可手动复制。列表和详情不会返回明文 Token。写请求失败时不自动重试，因为操作可能已成功；应先刷新元数据，再决定是否轮转新 Token。Trusted Mode 保留本地身份设置，不开放管理功能。

## 4. Trusted Mode

Trusted Mode 适合本地开发、受信网络和其他身份已由运行环境保证的场景：

```text
id: local-codex
role: backend
```

Agent 无需 Token，直接声明 id 和 role。Kairos 信任这些信息，并以此进行任务发现和协作记录。

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
Agent role: backend
Task tags: [backend, auth]
```

Kairos 可以根据 Agent Role 提供默认 tags 或查询范围，Agent 再结合 Task 描述和当前上下文作出选择。

Blackboard 的 tags 表达工作分类和发现线索，不自动成为访问权限。需要限制 Agent 执行某个 Task 时，应显式配置 allowed roles；Human 执行只受执行者类型控制，不按 Agent Role 筛选。

Workflow Definition 和 Blackboard Definition 都可以提供 Suggested Tags，例如 `module:*`。Agent 根据实际工作为 Task 添加具体 tags；Definition 只提供推荐词汇，不要求人持续维护每个 Task 的标签。

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

Kairos 可以托管轻量的 Agent Profile，例如 role、展示标签和描述；项目级执行规则仍由仓库维护。

> Identity 告诉 Kairos 是谁在行动，Role 则限定这个 Agent 可以领取哪些工作。

## 部署管理员作为 Human

部署 Admin Token 也可通过 HTTP、MCP 和工作台认证为稳定、绑定数据库、role 为空的普通 Human。业务权限遵循 Human 规则；身份管理仍只接受配置的凭据。更换 Token 保持 actor，并要求重启所有实例，不授予 Agent discovery 或 Executor 权限。持久化、冲突、迁移和会话语义见 [API 参考](../api-reference.zh-CN.md#admin-token-业务身份)。 控制台通过可选会话展示字段显示 `system admin`，actor ID 不变。Admin 配置要求至少 32 个可见 ASCII 字符（0x21–0x7E），不允许空白和控制字符。

### 身份管理布局与 Actor ID
身份管理沿用工作台资料架布局，以已有身份列表为主体，页头提供“创建身份”和“刷新身份列表”。创建及轮转／撤销确认使用共享弹窗。列表分为身份、类型／角色、Token 状态和操作；部署管理身份显示 **system admin**，完整内部 ID 放在次级信息中并支持复制。新 Token 使用独立的一次性结果区域。退出登录仅保留在账户菜单。

Actor ID 必须包含非空白字符，且不能等于 `.` 或 `..`（保留的 URL 路径段）。继续支持 Unicode 和有意义的首尾空白；HTTP 创建身份保留原值，Trusted HTTP/MCP 身份头先去除首尾空白，再执行相同领域校验。详情、轮转和撤销 URL 中应将完整 Actor ID 编码为单一路径参数。非法输入在写入身份或签发凭据前被拒绝，修正后再重试。MCP 身份来自凭据／Trusted 请求头，不来自工具参数。本次无需迁移已有 ID；服务端生成的 Admin ID 已满足规则。
