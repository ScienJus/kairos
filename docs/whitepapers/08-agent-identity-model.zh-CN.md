# Kairos Agent 身份模型

身份和责任很容易被混为一谈。知道哪个 Actor 发起请求，并不能说明它当前负责哪份工作；持有 Claim，也不应该迫使系统把长期凭据交给一个短命的 Harness。

Kairos 把这些问题分开：Identity 说明谁在操作，Claim 说明它正对什么负责，Claim-bound Executor Credential 则把一个具体 Harness 限制在这次执行内。

## Actor 与 Identity

```text
Identity
├── actor_id   稳定标识
├── kind       human | agent
└── role       Agent 的单一角色；Human 为空
```

Actor ID 表示行为来源，不自动授予权限。Human 权限由操作类型决定；Agent 还需通过 Task 的 `executor` 和 `allowed_roles` 校验。

Actor ID 是稳定的历史引用，必须包含非空白字符，且不能等于 `.` 或 `..`。不同传输中的保留、去除空白和 URL 编码规则见 [API 参考](../api-reference.zh-CN.md#identity-管理与控制台)。

Identity 与 Agent Harness 不是同一对象。一个 Identity 可以被多个会话或 Daemon 实例使用，真正的独占执行责任始终由 Claim 保护。

因此，两个使用同一 Agent Identity 的 Daemon 进程可能发现同一个候选。审计上它们属于同一 Actor，但只有成功建立 Claim 的进程拥有 Task。Identity 解决行为归属，不负责串行化执行。

## 两种身份确认方式

### Trusted Mode

适用于本地开发或已由运行环境保证身份的网络。请求直接提供 Actor ID、kind 和 role，Kairos 信任这些值。

### Authenticated Mode

适用于同一可信协作群体的共享部署。系统持久化 Identity，通过 Bearer Token 解析 Actor 与 Role，并支持 Token 签发、轮换和撤销。请求不能用参数覆盖 Token 所属身份。

两种模式的业务规则完全相同，只是身份证明强度不同。Authenticated Mode 不提供租户、Team、项目或对象级隔离；互不信任的群体必须分别部署。

## Role 缩小候选资格

Agent Identity 有一个单一 Role。Workflow 和 Blackboard 都使用精确 Role 匹配判断 Agent 是否有资格；Human 不受 `allowed_roles` 限制，但仍必须满足 `executor` 类型和操作本身的权限。

Role 只决定可见候选和可领取资格，不表示已取得责任。Claim 仍是防止两个同 Role Agent 同时执行同一 Task 的权威记录。

Blackboard tags 用于发现上下文，不取代 Role 授权。Workflow 候选由图与 Role 决定，不用 tags 缩小法定候选。

## Executor Credential：只用于一次执行

Agent Daemon 不把长期 Identity Token 交给具体 Harness。它在建立 Task 或 Coordination Claim 时生成一次性 Executor Token：

- Token 绑定 Claim、Actor 和权限 Profile；
- Core 只存储 hash，明文只在领取响应中返回一次；
- 读写始终限制在绑定 WorkItem 和允许的操作内；
- Claim 结束、WorkItem 终态或 scope 不匹配时立即失效；
- Identity Token 的后续轮换不改写已发放 Executor Token 的 Claim 生命周期。

Executor Credential 是执行 scope，不是另一个 Identity，不携带 Role，也不用于发现新工作。Token 只应进入受保护的运行配置，不能写入 WorkItem、Task 或项目文档。

## Identity 不替代工作区指引

`AGENTS.md` 描述某个代码库或目录中的工作规则；Agent Identity 描述向 Kairos 提交操作的 Actor。两者正交：

```text
Identity  → 谁在执行
AGENTS.md → 在当前工作区如何执行
```

Role 不应用来编码代码风格、测试命令或目录规则；这些属于项目指引。

## 部署 Admin Token

Authenticated Mode 的 Admin Token 映射为一个稳定、绑定数据库的普通 Human Actor。它的业务权限遵循 Human 规则，不获得 Agent 发现、Role 或 Executor 权限。只有配置的 Admin 凭据可以管理 Identity，普通 Human Identity 不能。

Token 轮换保留同一 Actor，但需要重启所有服务实例。控制台可以把它显示为 `system admin`，但不会改变 Actor ID。配置、持久化、会话、Identity 管理和兼容性细节见 [API 参考](../api-reference.zh-CN.md#admin-token-业务身份)。

## Identity 不变量

- 身份证明与执行责任分开；Identity 不取代 Claim。
- 服务端从认证结果解析 Actor，不信任请求参数自报身份。
- Role 只约束 Agent 资格；Human 权限由明确业务规则决定。
- Executor Credential 的权限不能超过其 Claim scope。
- Authenticated Mode 的 Token 隔离不等于多租户数据隔离。
