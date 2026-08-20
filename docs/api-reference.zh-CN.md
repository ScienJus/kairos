# Kairos API 参考

[English](api-reference.md)

本文档集中说明传输、认证、HTTP 资源、MCP 工具与执行响应契约。产品概念和协调语义仍由 README 与白皮书承载。

## 服务启动

默认服务使用 SQLite 与 Trusted Mode：

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_AGENT_CLAIM_LEASE=5m \
go run ./cmd/kairos-server
```

`GET /healthz` 无需认证。HTTP 管理与执行路由位于 `/api/v1`，Streamable HTTP MCP 位于 `/mcp`。

## 身份模式

Trusted Mode 在可信边界内接受传输头：

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`X-Kairos-Actor-Kind` 默认为 `agent`，Human 没有 role。变更请求应提供 `Idempotency-Key`；相同重试复用原 key，参数变化后使用新 key。

共享环境应使用 Authenticated Mode：

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<至少-32-字符的高熵-token>' \
go run ./cmd/kairos-server
```

身份管理路由使用 `Authorization: Bearer <admin-token>`，业务路由使用签发的 identity token。Authenticated Mode 忽略 Trusted actor headers。

## HTTP 资源

| 资源 | 路由 |
| --- | --- |
| Workflow Definitions | `GET/POST /api/v1/definitions/workflows`、`GET /api/v1/definitions/workflows/{id}/versions/{version}` |
| Blackboard Definitions | `GET/POST /api/v1/definitions/blackboards`、`GET /api/v1/definitions/blackboards/{id}/versions/{version}` |
| WorkItems | `GET/POST /api/v1/work-items`、`GET /api/v1/work-items/{id}/context`、`POST /completion`、`POST /acceptance` |
| 工作发现 | `GET /api/v1/work` |
| Task 详情与执行 | `GET /api/v1/tasks/{id}`、`/context`、`/claims`、`/submissions`、`/failures`、`/reviews` |
| Blackboard 规划 | WorkItem Task、relation、completion；Task decomposition、children 与 skipping |
| 人工关注 | `GET /api/v1/human-attention` |
| Identities | `GET/POST /api/v1/identities` 及 Token 轮换、撤销路由 |

Definition 版本不可变。Workflow WorkItem 根据图实例化起始 Task；空 Blackboard WorkItem 保持为规划候选。Blackboard Task 收敛后，`find_work` 返回 `blackboard_completion`；协作者可以继续创建 Task，也可以提交持久完成结果。提交后才应用 `acceptance_mode`：`none`（默认）立即完成，`agent` 返回 `work_item_acceptance`，`human` 进入人工验收；验收通过独立的 `POST /acceptance` 动作完成。Agent 验收候选仅对 Agent identity 可见。生命周期决策候选优先于可执行或待规划候选返回，`limit` 对所有候选类型全局生效。

等待验收期间，`WorkItem.Result` 保存已提交的 completion proposal；验收通过后，同一字段表示已接受的最终结果。Agent 验收阶段若通过创建新 Task 重新打开 WorkItem，会清除已经失效的 proposal。

`GET /api/v1/work-items/{id}/context` 在 WorkItem 终态后仍可读取，返回聚合结果、规范化的 Task 与 relation 集合、`Claims` 中的完整 Claim 历史，以及 `ActiveClaims` 中当前仍存活的子集。完成态 Task 的执行人可通过 `Submission.ClaimID -> Claims[].ID -> Executor` 关联。

`GET /api/v1/tasks/{id}` 是面向查看者的 Task Detail，不要求当前身份能够执行该 Task。它返回后端计算的 `Responsibility`、`Outcome`、`CurrentReview`、规范化 `History` 和当前身份的 `Capabilities`。`GET /api/v1/tasks/{id}/context` 仍是受执行权限保护的执行者上下文；客户端不得使用它加载普通详情或人工 Review。

## Claim Lease

Agent Claim 使用 lease，Human Claim 不使用。Agent Claim 与 heartbeat 可选择 15 秒至 30 分钟的时长；省略时使用 `KAIROS_AGENT_CLAIM_LEASE`，默认五分钟。`lease_until` 是后台 reaper 最早可以结束 Claim、将 Task 恢复为 Pending 的时间；到达该时间本身不会改变执行权。reaper 提交回收事务前，当前执行者仍可继续推进 Task 或续租，其他执行者仍不能 Claim 这个 Working Task。回收完成后，旧 Claim ID 继续作为 fencing token，不能续租或提交成果。

## MCP

MCP 与 HTTP 复用身份解析。Trusted Mode 在传输层提供 actor headers，Authenticated Mode 提供 `Authorization: Bearer <identity-token>`；身份不会出现在工具参数中。

执行面包含 15 个工具：

- 发现与上下文：`find_work`、`get_task_context`、`get_work_item_context`；
- Claim 生命周期：`claim_task`、`heartbeat_claim`、`release_claim`、`submit_task`、`fail_task`；
- Blackboard 规划与关闭：`create_blackboard_task`、`add_blackboard_relation`、`decompose_blackboard_task`、`add_blackboard_child_task`、`skip_blackboard_task`、`submit_blackboard_completion`、`accept_blackboard_completion`。

每个 MCP 变更都必须携带 `operation_id`，只有完全相同的重试才能复用。Workflow 候选由 role 与图状态决定，忽略 tag 筛选；Blackboard 可以按 tags 发现。Workflow Task context 提供受控的上游摘要和 durable result，但不授予任意读取其他 Task 的权限。

项目 Codex 配置位于 `.codex/config.toml`，执行指引位于 `.agents/skills/kairos-agent/SKILL.md`。
