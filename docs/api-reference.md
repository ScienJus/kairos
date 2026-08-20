# Kairos API Reference

[简体中文](api-reference.zh-CN.md)

This reference covers transport, authentication, HTTP resources, MCP tools, and execution response contracts. Product concepts and coordination semantics remain in the README and whitepapers.

## Server

The default server uses SQLite and Trusted Mode:

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_AGENT_CLAIM_LEASE=5m \
go run ./cmd/kairos-server
```

`GET /healthz` is unauthenticated. HTTP management and execution routes use `/api/v1`; Streamable HTTP MCP uses `/mcp`.

## Identity Modes

Trusted Mode accepts transport headers inside a trusted boundary:

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`X-Kairos-Actor-Kind` defaults to `agent`. Human identities have no role. Mutation requests should provide `Idempotency-Key`; an identical retry reuses the same key, while changed arguments require a new key.

Authenticated Mode is intended for shared environments:

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<at-least-32-character-high-entropy-token>' \
go run ./cmd/kairos-server
```

Admin identity routes require `Authorization: Bearer <admin-token>`. Work routes require an issued identity token. Authenticated Mode ignores trusted actor headers.

## HTTP Resources

| Resource | Routes |
| --- | --- |
| Workflow Definitions | `GET/POST /api/v1/definitions/workflows`, `GET /api/v1/definitions/workflows/{id}/versions/{version}` |
| Blackboard Definitions | `GET/POST /api/v1/definitions/blackboards`, `GET /api/v1/definitions/blackboards/{id}/versions/{version}` |
| WorkItems | `GET/POST /api/v1/work-items`, `GET /api/v1/work-items/{id}/context`, `POST /completion`, `POST /acceptance` |
| Discovery | `GET /api/v1/work` |
| Task detail and execution | `GET /api/v1/tasks/{id}`, `/context`, `/claims`, `/submissions`, `/failures`, `/reviews` |
| Blackboard planning | WorkItem Tasks, relations, completion; Task decomposition, children, and skipping |
| Human attention | `GET /api/v1/human-attention` |
| Identities | `GET/POST /api/v1/identities`, token rotation and revocation routes |

Definition versions are immutable. Workflow WorkItems instantiate start Tasks from the graph; empty Blackboard WorkItems remain planning candidates. After Blackboard Tasks converge, `find_work` returns `blackboard_completion`; a collaborator either creates more Tasks or posts a durable completion result. That submission then applies `acceptance_mode`: `none` (default) completes immediately, `agent` returns `work_item_acceptance`, and `human` enters human acceptance. Acceptance is a separate `POST /acceptance` action. Agent acceptance candidates are visible only to Agent identities. Lifecycle decisions are returned before executable or planning candidates, and `limit` applies globally across those candidate kinds.

While acceptance is pending, `WorkItem.Result` contains the submitted completion proposal. After acceptance, the same field contains the accepted final outcome. Reopening Agent acceptance by creating another Task clears the stale proposal.

`GET /api/v1/work-items/{id}/context` remains available after terminal completion and returns the aggregate result, normalized Task and relation collections, complete Claim history in `Claims`, and the currently live subset in `ActiveClaims`. A completed Task's executor can be resolved through `Submission.ClaimID -> Claims[].ID -> Executor`.

`GET /api/v1/tasks/{id}` is a viewer-facing Task Detail endpoint and does not require the current identity to be able to execute the Task. It returns backend-projected `Responsibility`, `Outcome`, `CurrentReview`, normalized `History`, and identity-specific `Capabilities`. `GET /api/v1/tasks/{id}/context` remains an executor context protected by execution authorization; clients must not use it to load ordinary detail or human Review operations.

## Claim Leases

Agent Claims use leases; human Claims do not. Agent claim and heartbeat requests may choose a duration from 15 seconds through 30 minutes. Omitted durations use `KAIROS_AGENT_CLAIM_LEASE`, five minutes by default. `lease_until` is the earliest time when the background reaper may end the Claim and return its Task to Pending; reaching that timestamp does not itself change ownership. Until the reaper commits that transition, the current executor may continue Task operations or renew the Claim, while every other executor remains unable to Claim the working Task. After reaping, the old Claim ID remains fenced and cannot be renewed or used for submission.

## MCP

MCP shares the HTTP identity resolver. Trusted Mode supplies actor headers at transport level; Authenticated Mode supplies `Authorization: Bearer <identity-token>`. Identity never appears in tool arguments.

The execution surface contains fifteen tools:

- discovery and context: `find_work`, `get_task_context`, `get_work_item_context`;
- Claim lifecycle: `claim_task`, `heartbeat_claim`, `release_claim`, `submit_task`, `fail_task`;
- Blackboard planning and closure: `create_blackboard_task`, `add_blackboard_relation`, `decompose_blackboard_task`, `add_blackboard_child_task`, `skip_blackboard_task`, `submit_blackboard_completion`, `accept_blackboard_completion`.

Every MCP mutation requires an `operation_id`. Reuse it only for an identical retry. Workflow discovery is determined by role and graph state and ignores tag filters; Blackboard discovery may use tags. Workflow Task context exposes controlled upstream summaries and durable results without granting arbitrary access to other Tasks.

Project Codex configuration is in `.codex/config.toml`; execution guidance is in `.agents/skills/kairos-agent/SKILL.md`.
