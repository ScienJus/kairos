# Kairos API 参考

[English](api-reference.md)

本文档集中说明传输、认证、HTTP 资源、MCP 工具与执行响应契约。产品概念和协调语义仍由 README 与白皮书承载。

## 服务启动

默认服务使用 SQLite 与 Trusted Mode：

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_AGENT_CLAIM_LEASE=5m \
KAIROS_ARTIFACT_DIR=artifacts \
KAIROS_ARTIFACT_MAX_UPLOAD_BYTES=16777216 \
KAIROS_ARTIFACT_GC_RETENTION=24h \
KAIROS_ARTIFACT_GC_INTERVAL=15m \
go run ./cmd/kairos-server
```

设置 `KAIROS_POSTGRES_DSN` 后，服务改用 PostgreSQL：

```bash
KAIROS_POSTGRES_DSN='postgres://kairos:<password>@127.0.0.1:5432/kairos?sslmode=disable' \
go run ./cmd/kairos-server
```

`KAIROS_POSTGRES_DSN` 非空时优先于 `KAIROS_SQLITE_PATH`。服务在开始接收请求前会验证连接并应用内嵌的 PostgreSQL Migration；显式配置的数据库无效或不可访问时，启动将直接失败。

参与发现、授权候选收窄、筛选或排序的运行时字段，除了保留在聚合 payload 中，也存入专用列。PostgreSQL 对 WorkItem/Task tags 和 Task allowed roles 使用原生 `TEXT[]`，并为当前的包含查询建立 GIN 索引。SQLite 将相同逻辑字段保存为经过校验的 JSON 数组 `TEXT` 列，通过 `json_each` 执行包含查询；SQLite 定位于本地和较小规模部署。空集合在存储和响应中均为数组，不使用 `null`。

数据库时间在应用边界统一规范为 UTC、微秒精度，因此 API 值、聚合 payload 和查询列表示同一个时间点。PostgreSQL 使用 `TIMESTAMPTZ`；SQLite 使用固定宽度的 RFC 3339 UTC 文本，使文本范围比较保持时间顺序。Definition、WorkItem、Task、Workflow activation 和 Identity 按照领域元数据使用 `created_at` 与 `updated_at`。Task Relation 持久化其不可变的 `created_at`；可变的 Claim、暂存 Artifact 和幂等记录在状态变化时更新 `updated_at`。具有更精确不可变事件或生命周期语义的行继续使用 `occurred_at`、`claimed_at`、`applied_at` 等字段名，不额外添加没有含义的重复时间列。

`GET /healthz` 无需认证。HTTP 管理与执行路由位于 `/api/v1`，Streamable HTTP MCP 位于 `/mcp`。

## HTTP 契约

机器可读的 [OpenAPI 3.1 文档](openapi.yaml)是当前全部 39 个 HTTP operation 的精确契约，包含认证方式、路径和查询参数、JSON 与 multipart 请求体、响应状态码、枚举、默认值、Artifact 二进制下载及每一个响应字段。本文保留不适合写入 Schema 的行为语义。

所有 API JSON 字段统一使用 `snake_case`。JSON 请求对象是封闭契约；未知字段（包括嵌套对象中的未知字段）会被拒绝并返回 `400 invalid_request`。JSON 成功响应使用 `{ "data": ... }`，JSON 错误响应使用 `{ "error": { "code": string, "message": string } }`。释放 Claim 和撤销 Token 返回无响应体的 `204`；`/healthz` 返回 `{ "status": "ok" }`；Artifact 内容使用 `application/octet-stream`。

集合字段和列表响应即使为空也始终编码为数组。`active_claim_id`、`parent_task_id`、`current_review`、`workflow`、`blackboard`、完成时间以及取消操作者和时间等可选单值在不存在时使用 `null`。重复的 `status`、`mode` 与 `tag` 查询参数通过重复 query key 传递。通用错误码如下：

| 状态码 | Code |
| --- | --- |
| `400` | `invalid_request` |
| `401` | `unauthenticated` |
| `403` | `forbidden` |
| `404` | `not_found` |
| `409` | `conflict` |
| `409` | `work_item_cancelled` |
| `413` | `artifact_too_large` |
| `500` | `internal_error` |

## 身份模式

Trusted Mode 在可信边界内接受传输头：

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`X-Kairos-Actor-Kind` 默认为 `agent`。Agent 身份必须提供 `X-Kairos-Actor-Role`，Human 身份必须省略它。创建 WorkItem、Blackboard Task、Claim、外部 Artifact、拆分结果或子 Task 的 HTTP 请求可以提供稳定的 `Idempotency-Key`；完全相同的重试返回原资源，参数变化时必须使用新 key。Definition 追加使用 `base_version`，生命周期变更依据当前状态判断，不重放旧响应。托管 Artifact 上传必须提供稳定 key，因为文件存储与数据库无法放入同一个事务。

同一可信协作群体内的共享环境应使用 Authenticated Mode：

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<至少-32-字符的高熵-token>' \
go run ./cmd/kairos-server
```

身份管理路由使用 `Authorization: Bearer <admin-token>`，业务路由使用签发的 identity token。Authenticated Mode 忽略 Trusted actor headers。

认证用于确定调用方身份；Task 发现、领取以及基于 Claim 的执行等操作仍受各自规则约束。但认证不是数据隔离边界：所有已签发身份都属于同一个全局信任域，Kairos 当前不会按租户、Team、项目或对象隔离读写。需要阻止不同群体访问彼此数据时，应分别部署 Kairos 实例。

Operations console 通过公开的 `GET /api/v1/auth/config` 识别当前模式。在 Authenticated Mode 下，控制台会在加载任何工作区数据前显示 Token 登录页，通过 `GET /api/v1/session` 验证 Token，之后所有 API 请求、托管上传和 Artifact 下载都使用该 Bearer 凭据。Token 只保存在浏览器 `sessionStorage` 中，因此仅属于当前标签页会话，不会形成持久浏览器登录；如果浏览器禁止访问该存储，控制台会明确报告不可用。退出登录会清除 Token 和缓存的 API 数据；包括 Token 被撤销或轮换在内的任何 `401` 响应，也会清除会话并返回登录页。

`GET /api/v1/auth/config` 无需认证，返回 `{ "data": { "mode": "trusted" | "authenticated" } }`。`GET /api/v1/session` 使用普通业务路由的认证方式，返回传输层实际解析出的身份：`{ "data": { "id": string, "kind": "human" | "agent", "role": string } }`。客户端应信任该结果，而不是自行从 Token 推导身份字段。

## HTTP 资源

| 资源 | 路由 |
| --- | --- |
| 认证与会话 | `GET /api/v1/auth/config`、`GET /api/v1/session` |
| Workflow Definitions | 目录 `GET /api/v1/definitions/workflows`；最新版本 `GET /{id}`；历史与追加 `GET/POST /{id}/versions`；精确版本 `GET /{id}/versions/{version}` |
| Blackboard Definitions | 目录 `GET /api/v1/definitions/blackboards`；最新版本 `GET /{id}`；历史与追加 `GET/POST /{id}/versions`；精确版本 `GET /{id}/versions/{version}` |
| WorkItems | `GET/POST /api/v1/work-items`、`GET /api/v1/work-items/{id}/context`、`POST /completion`、`POST /acceptance`、`POST /cancellation` |
| Artifacts | `GET /api/v1/work-items/{id}/artifacts`、`POST /api/v1/tasks/{id}/artifacts`、`POST /api/v1/tasks/{id}/artifact-uploads`、`GET /api/v1/artifacts/{id}/content` |
| 工作发现 | `GET /api/v1/work` |
| Task 详情与执行 | `GET /api/v1/tasks/{id}`、`/context`、`/claims`、`/submissions`、`/failures`、`/reviews` |
| Blackboard 规划 | WorkItem Task、relation、completion；Task decomposition、children 与 skipping |
| 人工关注 | `GET /api/v1/human-attention` |
| Identities | `GET/POST /api/v1/identities` 及 Token 轮换、撤销路由 |

WorkItem、Human Attention、Definition 目录、Definition 版本历史和已提交 Artifact 的列表路由使用 cursor 分页。`limit` 默认为 50，允许范围为 1-200。每页返回 `{ "data": [...], "next_cursor": string | null }`；当该值非空时，将其作为 `cursor` 传回同一集合路由，并保留原有过滤参数。Cursor 是不透明且与集合绑定的；无效 cursor 或 limit 返回 `400 invalid_request`。WorkItem 按 `updated_at DESC, id ASC` 排序，Human Attention 优先返回 Review，其余按条目更新时间排序；Definition 目录按 `id ASC` 排序，版本历史按 `version DESC` 排序，Artifact 按 `created_at ASC, id ASC` 排序。

Operations console 使用两套按状态过滤的 cursor 分别加载进行中和已结束 WorkItem，因此加载更早历史不会影响进行中工作队列。

每个 Definition 目录对每个 ID 返回最大的已存储版本。`GET /definitions/{mode}/{id}` 返回该最新版本，`GET /definitions/{mode}/{id}/versions` 分页返回该 ID 的不可变版本历史。控制台会直接解析未知 ID 或版本，不扫描无关目录页。

创建新的 Definition ID 时省略 `base_version`，服务端分配版本 1。追加版本时必须通过 `base_version` 提交编辑所基于的最新版本；服务端在 Definition 锁内比较后分配 `max(version) + 1`。已有 ID 缺少基线或基线过期时返回 `409 conflict`。

创建 WorkItem 时有意只提交 Definition ID 和 mode，不提交版本。服务端会在创建时解析并绑定该 ID 最大的已存储版本。

Definition 版本不可变。Workflow WorkItem 根据图实例化起始 Task；空 Blackboard WorkItem 保持为规划候选。Blackboard Task 收敛后，`find_work` 返回 `blackboard_completion`；协作者可以继续创建 Task，也可以提交持久完成结果。提交后才应用 `acceptance_mode`：`none`（默认）立即完成，`agent` 返回 `work_item_acceptance`，`human` 进入人工验收；验收通过独立的 `POST /acceptance` 动作完成。Agent 验收候选仅对 Agent identity 可见。生命周期决策候选优先于可执行或待规划候选返回，`limit` 对所有候选类型全局生效。

Workflow Definition 的每条 `graph.relations[]` 接受可选的 `label` 与 `agent_guidance` 字符串。空字符串表示没有额外 Guidance。两者不改变图编译或推进语义。HTTP Workflow Task context 的每个 Choice Group 在 `relations` 中返回完整 Guidance；MCP `get_task_context` 则在对应的 `targets[]` 项上提供合并后的 `relation_guidance`（优先使用 `agent_guidance`，否则使用 `label`），避免重复目标结构。

等待验收期间，`work_item.result` 保存已提交的 completion proposal；验收通过后，同一字段表示已接受的最终结果。Agent 验收阶段若通过创建新 Task 重新打开 WorkItem，会清除已经失效的 proposal。

`POST /api/v1/work-items/{id}/cancellation` 是只允许 Human 调用的管理动作，适用于 `open`、`awaiting_agent_acceptance` 和 `awaiting_human_acceptance` WorkItem。请求必须提供非空 `reason`，服务端记录 `cancelled_at`、`cancelled_by` 和 `cancellation_reason`，并清除待验收的完成提案。同一事务会让全部 Active Claim 以 `work_item_cancelled` 结束、清除对应 Task 的 `active_claim_id`，并只把 `working` Task 恢复为 `pending`；已有 Task 结果不会被重写，也不会创建 Task Failure。取消后的 WorkItem 仍可读取，后续 Task 变更返回 `409 work_item_cancelled`，Agent 收到后应直接停止。MCP 不提供取消工具。

`GET /api/v1/work-items/{id}/context` 在 WorkItem 终态后仍可读取，返回聚合结果、规范化的 Task 与 relation 集合、`claims` 中的完整 Claim 历史，以及 `active_claims` 中当前仍存活的子集。完成态 Task 的执行人可通过 `submission.claim_id -> claims[].id -> executor` 关联。

Workflow Task Definition 可以声明必交的 `artifacts[]`，每项只有 `name` 和 `description`。Description 是执行指引，不是文件类型 Schema。执行者持有 Claim 时可以用绝对 URI 创建外部 Artifact，或上传托管内容，再通过 `submit_task.artifact_ids` 提交。Submission 在同一事务中绑定暂存 Artifact；缺少 Definition 声明名称的 Workflow 提交会被拒绝。Blackboard Task 没有结构化 Artifact 契约。已提交 Artifact 对整个 WorkItem 可见，暂存 Artifact 只属于创建它的 Claim。

托管上传始终写入服务端唯一的托管 Store，调用方不能选择 Store。内置实现会先把稳定的 `kairos://` 上传 URI 和 pending 状态登记到数据库，再向 `KAIROS_ARTIFACT_DIR` 写入文件，并在完成数据库操作前同步文件和目录链；写入后的 Blob 元数据单独保存 SHA-256 完整性 Digest。

`POST /tasks/{id}/artifacts` 接受 `claim_id`、`name`、`uri` JSON 字段；`POST /tasks/{id}/artifact-uploads` 接受 `claim_id`、`name`、`file` multipart 字段，不存在 Store 字段。`KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` 限制上传文件内容大小，默认 16 MiB；超限返回 `413 artifact_too_large`。内置托管上传是面向小文件的便捷通道；大文件应先发布到 S3 等持久外部存储，再通过 URI 接口登记。外部 URI 创建接受可选的资源创建 key；托管上传必须提供该 Header，以便服务端在写文件前先登记上传 URI 和 pending 状态。Store 流式写入并返回 Digest 和大小，服务端随后更新 pending 记录，再通过最终事务创建 Blob 元数据、暂存 Artifact，并把操作更新为 completed。Pending 重试会覆盖已登记 URI，并校验此前记录的 Digest 和大小，因此即使清理流程删除了文件却未能删除 pending 记录也能恢复。在上传记录仍处于配置的保留窗口内时，完全相同的 completed 上传可以在 Claim 结束后通过原 key 找回暂存 Artifact。

Artifact GC 默认每隔 `KAIROS_ARTIFACT_GC_INTERVAL`（15 分钟）执行一次。Claim 已不再 Active 且 Artifact 创建时间超过 `KAIROS_ARTIFACT_GC_RETENTION`（默认 24 小时）的未提交 Artifact 会进入回收；超过相同保留时间的 pending 托管上传记录及其已登记文件也会删除，completed 外部登记和托管上传重放记录同样在该窗口后过期；已提交 Artifact 始终保留。只有 Blob URI 已不再被任何 Artifact 引用时，托管内容和元数据才会删除。三个 Artifact 数值或时长配置都必须为正数。

`GET /api/v1/tasks/{id}` 是面向查看者的 Task Detail，不要求当前身份能够执行该 Task。它返回后端计算的 `responsibility`、`outcome`、`current_review`、规范化 `history`、属于该 Task 的已提交 `artifacts` 和当前身份的 `capabilities`。Task 没有已提交交付物时，`artifacts` 编码为 `[]`。`GET /api/v1/tasks/{id}/context` 仍是受执行权限保护的执行者上下文；客户端不得使用它加载普通详情或人工 Review。

## Claim Lease

Agent Claim 使用 lease，Human Claim 不使用。Agent Claim 与 heartbeat 可选择 15 秒至 30 分钟的时长；省略时使用 `KAIROS_AGENT_CLAIM_LEASE`，默认五分钟。`lease_until` 是后台 reaper 最早可以结束 Claim、将 Task 恢复为 Pending 的时间；到达该时间本身不会改变执行权。reaper 提交回收事务前，当前执行者仍可继续推进 Task 或续租，其他执行者仍不能 Claim 这个 Working Task。回收完成后，旧 Claim ID 继续作为 fencing token，不能续租或提交成果。

## MCP

MCP 与 HTTP 复用身份解析。Trusted Mode 在传输层提供 actor headers，Authenticated Mode 提供 `Authorization: Bearer <identity-token>`；身份不会出现在工具参数中。

执行面包含 17 个工具：

- 发现与上下文：`find_work`、`get_task_context`、`get_work_item_context`；
- Claim 生命周期与交付：`claim_task`、`heartbeat_claim`、`create_artifact`、`upload_artifact`、`release_claim`、`submit_task`、`fail_task`；
- Blackboard 规划与关闭：`create_blackboard_task`、`add_blackboard_relation`、`decompose_blackboard_task`、`add_blackboard_child_task`、`skip_blackboard_task`、`submit_blackboard_completion`、`accept_blackboard_completion`。

只有创建资源的 MCP 工具要求 `operation_id`：`claim_task`、`create_artifact`、`upload_artifact`、`create_blackboard_task`、`decompose_blackboard_task` 和 `add_blackboard_child_task`。这些工具会重放完全相同的重试，避免响应丢失后无法找回服务端生成的 ID；参数变化时必须使用新 ID。生命周期变更和 Relation 创建直接依据当前领域状态判断，成功后的重试可能返回冲突。Workflow 候选由 role 与图状态决定，忽略 tag 筛选；Blackboard 可以按 tags 发现。Workflow Task context 提供受控的上游摘要、durable result 和可选 Relation Guidance，但不授予任意读取其他 Task 的权限，也不会因为 Guidance 产生 Definition 未允许的分支。

`upload_artifact` 通过不带 data URI 前缀的 `content_base64` 接受标准 Base64 字节，解码后写入服务端配置的 Artifact Store，并返回供 `submit_task.artifact_ids` 使用的暂存 Artifact ID。解码后的大小上限与 HTTP multipart 上传共用 `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES`；MCP 请求体上限会包含对应的 Base64 膨胀。该工具只面向小文件，因为 Base64 会增加约三分之一传输体积，且 MCP 请求会完整缓存在内存中。大文件应使用 `create_artifact` 登记 S3 或其他持久外部 URI。

项目 Codex 配置位于 `.codex/config.toml`，执行指引位于 `.agents/skills/kairos-agent/SKILL.md`。
