# Kairos API 参考

[English](api-reference.md)

本页说明部署、认证、资源边界和跨接口行为。[OpenAPI 3.1](openapi.yaml) 是 HTTP 路径、字段、请求体、响应码、枚举和限制的权威契约；本页不重复逐字段 Schema。

## 启动与存储

默认使用 SQLite 和 Trusted Mode：

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
go run ./cmd/kairos-server
```

设置 `KAIROS_POSTGRES_DSN` 后使用 PostgreSQL；它优先于 `KAIROS_SQLITE_PATH`。服务在接收请求前验证连接并应用内嵌 migration，显式配置的数据库不可用时启动失败。

| 配置 | 默认值 | 用途 |
| --- | --- | --- |
| `KAIROS_AGENT_CLAIM_LEASE` | `5m` | Agent Claim 默认 lease |
| `KAIROS_ARTIFACT_DIR` | `artifacts` | 托管 Artifact 根目录 |
| `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` | `16777216` | HTTP/MCP 上传内容上限 |
| `KAIROS_ARTIFACT_GC_RETENTION` | `24h` | 未提交 Artifact 与幂等记录保留时间 |
| `KAIROS_ARTIFACT_GC_INTERVAL` | `15m` | Artifact GC 间隔 |
| `KAIROS_HTTP_READ_TIMEOUT` | `60s` | 读取完整请求的总时间 |
| `KAIROS_HTTP_WRITE_TIMEOUT` | `120s` | 请求体、Handler 和响应共享的绝对 write deadline |
| `KAIROS_HTTP_IDLE_TIMEOUT` | `120s` | keep-alive 请求之间的空闲时间 |

SQLite 文件使用 `0600`。Artifact 根目录必须是专用、非根目录、非符号链接且权限为 `0700` 的路径；托管文件使用 `0600`。无法满足安全条件时服务拒绝启动，不自动放宽权限。

公网部署应由反向代理终止 TLS，并限制连接数、速率和请求体。代理的超时应略长于 Kairos 对应配置。MCP 转发到 loopback 时，上游 `Host` 应使用 loopback 目标，同时保留 `Authorization` 和 `Origin`。`403 invalid Host header` 表示代理/loopback 配置错误，不是业务冲突或 Token 过期。

`GET /healthz` 无需认证。HTTP API 位于 `/api/v1`，Streamable HTTP MCP 位于 `/mcp`。Core 与 Daemon 的完整启动示例见 [`examples/daemon`](https://github.com/ScienJus/kairos/blob/main/examples/daemon/README.zh-CN.md)。

## HTTP 约定

- JSON 字段统一使用 `snake_case`，请求对象拒绝未知字段。
- JSON 成功响应使用 `{ "data": ... }`；错误使用 `{ "error": { "code": string, "message": string } }`。
- 集合为空时返回 `[]`；真正可选的单值才返回 `null`。
- 释放 Claim、Daemon 上报和 Token 撤销返回无 Body 的 `204`。Artifact 内容使用 `application/octet-stream`。
- 列表使用 `{ "data": [...], "next_cursor": string | null }`。Cursor 不透明且与集合/过滤条件绑定。

| HTTP | 通用错误 code |
| --- | --- |
| `400` | `invalid_request` |
| `401` | `unauthenticated` |
| `403` | `forbidden` |
| `404` | `not_found` |
| `409` | `conflict` 或 `work_item_cancelled` |
| `413` | `artifact_too_large` |
| `500` | `internal_error` |

创建 WorkItem、Claim、Artifact 和 Blackboard Task 的请求可以使用稳定 `Idempotency-Key`；托管上传必须提供。相同 key 与相同参数返回原资源，参数改变时必须使用新 key。生命周期变更不重放旧响应，而是按当前状态重新判断。

## 身份与认证

### Trusted Mode

Trusted Mode 接受受信边界提供的 Header：

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`kind` 默认为 `agent`。Agent 必须提供 role，Human 必须省略 role。

### Authenticated Mode

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<at-least-32-visible-ASCII-high-entropy-token>' \
go run ./cmd/kairos-server
```

Authenticated Mode 忽略 Trusted Header。业务路由接受 Identity Token、部署 Admin Token 映射的 Human，或绑定 Active Claim 的 Executor Token。身份管理只接受 Admin Token。

`GET /api/v1/auth/config` 无需认证，返回当前模式。`GET /api/v1/session` 返回传输层实际解析的 `id`、`kind`、`role`、可选 `display_name` 和 `can_manage_identities`；客户端不应从 Token 或 ID 前缀推导这些字段。

### Admin Token 业务身份

Admin Token 同时映射为一个稳定、绑定数据库的普通 Human Actor。它的业务权限遵循 Human 规则；管理凭据本身才能管理 Identity。

Token 必须至少 32 个可见 ASCII 字符，不含空白或控制字符。轮换后需重启所有服务实例；同一数据库保留原 Human Actor。备份必须包含整个数据库，不支持手工删除或修改该 Actor 行。

控制台只在当前标签页的 `sessionStorage` 保存凭据。退出或当前凭据收到 `401` 时清除凭据与业务缓存。不得把凭据写入 URL、WorkItem、日志或截图。

### Executor Token

Agent 创建 Task/Coordination Claim 时可附带以 `krs_claim_` 开头的 256 位随机 Token。Core 只保存 SHA-256 hash。该凭据只能在 Claim Active 期间读取绑定 WorkItem 上下文；Task Executor 可额外创建 Artifact 和执行允许的 Blackboard 非终态写入，Coordination Executor 只读。

Claim 结束后 Token 失效。完整匹配 Executor 格式但认证失败的凭据不回退到 Identity 查询。

Authenticated Mode 是同一全局信任域，不是多租户隔离。互不信任的群体必须分别部署 Kairos。

### Identity 管理与控制台

Authenticated Mode 下，配置的 Admin 通过 `/admin/identities` 中的 **Token 管理** 创建 Human 或 Agent Identity，并轮换或撤销 Token。普通 Identity Token 不能管理身份。`/session` 返回 `can_manage_identities`；前端只用它决定是否展示入口，各管理端点仍独立认证 Admin 凭据。

管理页面复用当前标签页登录，不建立第二套管理员会话。新 Token 只存在于页面内存，列表与详情接口不会再次返回明文。关闭结果、开始其他凭据操作、离开或刷新前必须复制保存。Mutation 不自动重试，因为首次请求可能已经提交；应先刷新元数据，再决定是否轮换替代 Token。

Actor ID 必须包含非空白字符，且不能等于 `.` 或 `..`。允许 Unicode 和有意义的首尾空白。HTTP 创建 Identity 时保留原值；Trusted HTTP/MCP 请求头会先去除首尾空白再校验。详情、轮换和撤销 URL 中必须把完整 Actor ID 编码为一个路径参数。

较早的未发布构建可能保存了 `.` 或 `..` ID，这些身份将无法继续认证，系统不提供自动迁移。可丢弃数据应使用新数据库；如需保留历史，必须同时迁移 Identity 与全部历史 Actor 引用。

## HTTP 资源索引

| 领域 | 主要路由 |
| --- | --- |
| 认证 | `/auth/config`、`/session` |
| Identity | `/identities`、`/identities/{kind}/{actor_id}` 及 `/token` |
| Workflow Definition | `/definitions/workflows`、`/{id}`、`/{id}/versions`、`/{id}/versions/{version}` |
| Blackboard Definition | `/definitions/blackboards`、`/{id}`、`/{id}/versions`、`/{id}/versions/{version}` |
| 发现与人工关注 | `/work`、`/human-attention` |
| WorkItem | `/work-items`、`/{id}/context`、`/completion`、`/acceptance`、`/continue`、`/start-over`、`/cancellation` |
| Coordination Claim | `/work-items/{id}/coordination-claims`、`/{claim_id}/heartbeat`、`/{claim_id}` |
| Blackboard 规划 | `/work-items/{id}/tasks`、`/relations`；Task 下的 `/decomposition`、`/children`、`/skip` |
| Task 详情与执行 | `/tasks/{id}`、`/context`、`/claims`、`/submissions`、`/failures`、`/reviews/{review_id}/decision` |
| Artifact | `/work-items/{id}/artifacts`、`/tasks/{id}/artifacts`、`/artifact-uploads`、`/artifacts/{id}/content` |
| Daemon 观测 | `/daemon-instances`、`/{id}`、`/{id}/reports`、`/{id}/events` |

表中路径都相对 `/api/v1`。精确 HTTP 方法与 Schema 见 OpenAPI。

## 关键资源语义

### Definition 与 WorkItem

Definition ID 只允许小写 ASCII 字母、数字和连字符。创建 ID 时省略 `base_version`；追加版本必须提供当前基线，过期基线返回冲突。Definition 版本不可变。

创建 WorkItem 时只提交 Definition ID 和 mode，服务端在事务中绑定最新版本。Workflow 实例化起始 Task；Blackboard 可从空图开始。

### Workflow 恢复

`continue` 保留原 WorkItem、成功分支、汇合和审核，并为当前失败/中断工作创建替代 Task。`start-over` 使用同一 Definition 版本和原始目标创建新 WorkItem，并保留来源引用。

两者都不复活旧 Claim、不回放任意阶段、不自动复制旧 Artifact、Review 或外部副作用。Human 应在说明中写明新执行者需要的外部成果。恢复是 WorkItem 操作，没有独立 Task 重试管理接口。

### Blackboard 完成

Task 收敛后 WorkItem 仍为 `open`。协作者可以创建后续 Task，或提交持久完成结果。只有提交后才应用 `acceptance_mode`：`none` 立即完成，`agent` 产生 Agent 验收候选，`human` 进入人工验收。

Agent 在处理 `empty_blackboard`、`blackboard_completion` 或 `work_item_acceptance` 前必须建立 Coordination Claim。创建 Task、提交完成或验收完成会在同一事务中消耗该 Claim。

### 失败与取消

`fail_task` 支持 `retry`、`await_human` 和 `fail_work_item`。Workflow `retry` 创建替代 Task，Blackboard `retry` 重新开放原 Task；`await_human` 只适用 Workflow；`fail_work_item` 终止整个 WorkItem 并结束其他 Claim。

`cancellation` 只允许 Human 调用，并要求非空原因。取消会结束 Active Task/Coordination Claim，阻止后续变更，但不改写旧结果或伪造 Task Failure。Agent 收到 `work_item_cancelled` 后必须停止写入。

### 历史与 Detail

`GET /work-items/{id}/context` 在所有生命周期状态下可读，返回规范化 Task/Relation、完整 Claim 历史和当前 Active Claim 投影。

`GET /tasks/{id}` 是查看者 Detail，返回 responsibility、outcome、current review、history、已提交 Artifact 和 capabilities；它不要求执行资格。`GET /tasks/{id}/context` 是受执行权限保护的执行上下文，不用于普通详情页。

### Artifact

Workflow Task Definition 可以声明必交 Artifact 名称与说明。执行者可以登记绝对外部 URI，或上传到唯一托管 Store，再在 `submit_task.artifact_ids` 中提交。暂存 Artifact 只属于创建它的 Claim，已提交 Artifact 对整个 WorkItem 可见。

托管上传使用稳定 `kairos://` URI、SHA-256 digest 和先登记后写文件的恢复流程。未提交 Artifact、pending 上传和已完成的幂等重放记录按 retention 回收；已提交 Artifact 不由该生命周期删除。大文件应使用外部持久存储并登记 URI。

## 关键上限

下表便于运营评估；服务端精确校验以 OpenAPI 与实现为准。

| 范围 | 上限 |
| --- | --- |
| Workflow Definition | 100 个 Task Definition，1,000 条 Relation |
| Workflow 节点实例 | 默认每节点/WorkItem 100，可配置上限 500 |
| Blackboard WorkItem | 1,000 个 Task，10,000 条 Relation |
| Claim 历史 | 每 WorkItem 128 条 Coordination Claim；每 Task 128 条 Claim |
| Task 关联历史 | 各 64 条 Submission、Review、普通 Failure、Transition Decision 和 Artifact |
| 历史文本字段 | 32 KiB UTF-8 |
| `find_work` | 每个 candidate kind 默认 5，最大 50 |

达到历史安全上限会使 WorkItem 失败并结束 Active Claim，已接受历史仍完整保留。长内容应存入 Artifact 或外部持久存储，生命周期文本保留摘要和绝对 URI。

## Claim 租约

Agent Task Claim 和 Coordination Claim 使用 lease，Human Claim 不使用。Agent 可请求 15 秒至 30 分钟，省略时使用服务端默认值。

`lease_until` 是 reaper 最早可以回收的时间，不会在该时刻自动撤销执行权。reaper 提交回收前，当前 Agent 仍可续租或执行受保护操作；回收后旧 Claim ID 继续作为 fencing token，不能复活。

## Daemon 平台观测

`kairos-daemon` 每次启动生成新实例 ID，使用 Agent Identity Token 注册，并约每 15 秒上报快照与关键事件。遥测失败不影响调度、Claim、heartbeat 或业务结果。

Core 根据接收时间计算连接状态：45 秒内为 `reporting`，超时为 `stale`，只有明确退出报告才是 `stopped`。失联不等于 Claim 已结束。事件保留 30 天，已停止上报的实例保留 90 天。

上报只允许实例所属 Agent，读取只允许 Human。快照和事件是运行观测，WorkItem、Task 和 Claim 记录才是业务权威。完整契约见[可观测性设计](daemon-observability-design.zh-CN.md)。

## MCP 工具

MCP 与 HTTP 复用身份解析和应用层授权。身份来自传输层，不作为工具参数。Executor 会话只暴露 Profile 允许的工具，Claim 生命周期由 Agent Daemon 管理。

| 类别 | 工具 |
| --- | --- |
| 发现与上下文 | `find_work`、`get_task_context`、`get_work_item_context` |
| Task Claim 与交付 | `claim_task`、`heartbeat_claim`、`create_artifact`、`upload_artifact`、`release_claim`、`submit_task`、`fail_task` |
| Coordination Claim | `claim_work_candidate`、`heartbeat_coordination_claim`、`release_coordination_claim` |
| Blackboard 规划与关闭 | `create_blackboard_task`、`add_blackboard_relation`、`decompose_blackboard_task`、`add_blackboard_child_task`、`skip_blackboard_task`、`submit_blackboard_completion`、`accept_blackboard_completion` |

创建资源的工具要求 `operation_id`：`claim_task`、`claim_work_candidate`、`create_artifact`、`upload_artifact`、`create_blackboard_task`、`decompose_blackboard_task` 和 `add_blackboard_child_task`。相同重试返回原资源，参数改变时必须换 ID。

`upload_artifact` 接受不带 data URI 前缀的标准 Base64，只适合小文件；大文件应通过 `create_artifact` 登记持久外部 URI。

项目 Codex 配置位于 `.codex/config.toml`，执行指引位于 `.agents/skills/kairos-agent/SKILL.md`。
