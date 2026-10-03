# Kairos API Reference

[Chinese](api-reference.zh-CN.md)

This page explains deployment, authentication, resource boundaries, and cross-interface behavior. [OpenAPI 3.1](openapi.yaml) is authoritative for HTTP paths, fields, request bodies, status codes, enums, and limits; this page does not duplicate field-by-field schemas.

## Startup and Storage

The default uses SQLite and Trusted Mode:

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
go run ./cmd/kairos-server
```

Set `KAIROS_POSTGRES_DSN` to use PostgreSQL; it takes precedence over `KAIROS_SQLITE_PATH`. The server validates the connection and applies embedded migrations before accepting requests.

| Configuration | Default | Purpose |
| --- | --- | --- |
| `KAIROS_AGENT_CLAIM_LEASE` | `5m` | Default Agent Claim lease |
| `KAIROS_ARTIFACT_DIR` | `artifacts` | Managed Artifact root |
| `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` | `16777216` | HTTP/MCP content limit |
| `KAIROS_ARTIFACT_GC_RETENTION` | `24h` | Retention for unsubmitted Artifacts and idempotency records |
| `KAIROS_ARTIFACT_GC_INTERVAL` | `15m` | Artifact GC interval |
| `KAIROS_HTTP_READ_TIMEOUT` | `60s` | Total request-read time |
| `KAIROS_HTTP_WRITE_TIMEOUT` | `120s` | Shared deadline for body read, handler, and response |
| `KAIROS_HTTP_IDLE_TIMEOUT` | `120s` | Keep-alive idle time |

SQLite files use `0600`. The Artifact root must be a dedicated, non-root, non-symlink directory with `0700`; managed files use `0600`. Kairos fails startup instead of weakening unsafe permissions.

Public deployments should terminate TLS and enforce connection, rate, and body limits at a reverse proxy. Proxy timeouts should be slightly longer than Kairos. When forwarding MCP to loopback, send the loopback upstream as `Host` while preserving `Authorization` and `Origin`. `403 invalid Host header` indicates proxy/loopback configuration, not a business conflict or expired Token.

`GET /healthz` needs no authentication. HTTP API lives under `/api/v1`; Streamable HTTP MCP lives at `/mcp`. See [`examples/daemon`](https://github.com/ScienJus/kairos/blob/main/examples/daemon/README.md) for complete Core and Daemon startup.

## HTTP Conventions

- JSON fields use `snake_case`, and request objects reject unknown fields.
- JSON success uses `{ "data": ... }`; errors use `{ "error": { "code": string, "message": string } }`.
- Empty collections are `[]`; only optional singular values are `null`.
- Claim release, Daemon reports, and Token revocation return bodyless `204`.
- Lists use `{ "data": [...], "next_cursor": string | null }`; cursors are opaque and bound to the collection and filters.

| HTTP | Common error code |
| --- | --- |
| `400` | `invalid_request` |
| `401` | `unauthenticated` |
| `403` | `forbidden` |
| `404` | `not_found` |
| `409` | `conflict` or `work_item_cancelled` |
| `413` | `artifact_too_large` |
| `500` | `internal_error` |

Requests creating WorkItems, Claims, Artifacts, and Blackboard Tasks may use a stable `Idempotency-Key`; managed uploads require one. Same key and parameters return the original resource. Lifecycle mutations evaluate current state instead of replaying an old response.

## Identity and Authentication

### Trusted Mode

Trusted Mode accepts headers supplied by a trusted boundary:

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`kind` defaults to `agent`. Agents require a role; Humans must omit it.

### Authenticated Mode

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<at-least-32-visible-ASCII-high-entropy-token>' \
go run ./cmd/kairos-server
```

Authenticated Mode ignores Trusted headers. Business routes accept an Identity Token, the deployment Admin Token as a Human, or an Executor Token bound to an active Claim. Identity administration accepts only the Admin Token.

`GET /api/v1/auth/config` is public. `GET /api/v1/session` returns the transport-resolved `id`, `kind`, `role`, optional `display_name`, and `can_manage_identities`; clients must not infer them from Token or ID prefixes.

### Admin Token Business Identity

The Admin Token maps to one stable, database-bound ordinary Human actor. Business permissions follow Human rules; only the configured credential can administer Identities.

The Token requires at least 32 visible ASCII characters with no whitespace or controls. Rotation requires restarting every server instance and preserves the Human actor in the same database. Backups must include the complete database; manually deleting or editing this actor row is unsupported.

The console stores credentials only in the current tab's `sessionStorage`. Sign-out or a `401` for the current credential clears credentials and business caches. Never place credentials in URLs, WorkItems, logs, or screenshots.

### Executor Token

When creating a Task or Coordination Claim, an Agent may supply a 256-bit random Token prefixed `krs_claim_`. Core stores only its SHA-256 hash. While the Claim is active, it reads bound WorkItem context; a Task Executor may also create Artifacts and allowed nonterminal Blackboard writes, while a Coordination Executor is read-only.

The Token fails after Claim end. A credential matching Executor format but failing authentication never falls back to Identity lookup.

Authenticated Mode is one global trust domain, not multi-tenant isolation. Mutually untrusted groups require separate deployments.

### Identity Administration and Console

In Authenticated Mode, the configured Admin opens **Token management** at `/admin/identities` to create Human or Agent Identities and rotate or revoke their Tokens. Ordinary Identity Tokens cannot manage Identities. `/session` returns `can_manage_identities`; the UI uses that capability only for presentation, while every endpoint still authenticates the Admin credential.

The management page reuses the current-tab login rather than creating a second admin session. Newly issued Tokens exist only in page memory and never return from list or detail APIs. Copy them before dismissing the result, starting another credential operation, navigating away, or refreshing. Mutation requests are not automatically retried because the first request may already have committed; refresh metadata before deciding whether to rotate a replacement.

Actor IDs must contain a non-whitespace character and cannot equal `.` or `..`. Unicode and meaningful surrounding whitespace are allowed. HTTP identity creation preserves the supplied value; Trusted HTTP/MCP headers trim surrounding whitespace before validation. Encode the complete Actor ID as one URL path component for detail, rotation, and revocation.

Installations created by older unreleased builds may contain `.` or `..` IDs that can no longer authenticate. No automatic migration is provided. Disposable data should use a fresh database; retained history requires migrating the Identity and every historical Actor reference together.

## HTTP Resource Index

| Domain | Main routes |
| --- | --- |
| Authentication | `/auth/config`, `/session` |
| Identity | `/identities`, `/identities/{kind}/{actor_id}`, and `/token` |
| Workflow Definition | `/definitions/workflows`, `/{id}`, `/{id}/versions`, `/{id}/versions/{version}` |
| Blackboard Definition | `/definitions/blackboards`, `/{id}`, `/{id}/versions`, `/{id}/versions/{version}` |
| Discovery and Human attention | `/work`, `/human-attention` |
| WorkItem | `/work-items`, `/{id}/context`, `/completion`, `/acceptance`, `/continue`, `/start-over`, `/cancellation` |
| Coordination Claim | `/work-items/{id}/coordination-claims`, `/{claim_id}/heartbeat`, `/{claim_id}` |
| Blackboard planning | `/work-items/{id}/tasks`, `/relations`; Task `/decomposition`, `/children`, `/skip` |
| Task detail and execution | `/tasks/{id}`, `/context`, `/claims`, `/submissions`, `/failures`, `/reviews/{review_id}/decision` |
| Artifact | `/work-items/{id}/artifacts`, `/tasks/{id}/artifacts`, `/artifact-uploads`, `/artifacts/{id}/content` |
| Daemon observation | `/daemon-instances`, `/{id}`, `/{id}/reports`, `/{id}/events` |

Paths are relative to `/api/v1`. See OpenAPI for exact methods and schemas.

## Key Resource Semantics

### Definition and WorkItem

Definition IDs use lowercase ASCII letters, digits, and hyphens. Omit `base_version` to create an ID; appending requires the current base and conflicts on stale input. Versions are immutable.

WorkItem creation submits Definition ID and mode. The server binds the latest version transactionally. Workflow instantiates initial Tasks; Blackboard may begin empty.

### Workflow Recovery

`continue` keeps the WorkItem, successful branches, joins, and Reviews, then creates replacements for current failed or interrupted work. `start-over` creates a new WorkItem from the same Definition version and original intent and retains a source reference.

Neither revives Claims, replays an arbitrary stage, or copies old Artifacts, Reviews, or external side effects. Humans must describe external outcomes the new executor needs. Recovery is a WorkItem operation; there is no separate Task retry-management API.

### Blackboard Completion

Task convergence leaves the WorkItem `open`. A collaborator creates follow-up work or submits a durable completion result. Only then does `acceptance_mode` apply: `none` completes, `agent` creates an Agent acceptance candidate, and `human` enters Human acceptance.

Agents create a Coordination Claim before handling `empty_blackboard`, `blackboard_completion`, or `work_item_acceptance`. Creating work, submitting completion, or accepting it consumes the Claim transactionally.

### Failure and Cancellation

`fail_task` supports `retry`, `await_human`, and `fail_work_item`. Workflow retry creates a replacement Task; Blackboard retry reopens the same Task. `await_human` is Workflow-only. `fail_work_item` ends the WorkItem and other Claims.

`cancellation` is Human-only and requires a reason. It ends active Task and Coordination Claims and prevents later mutation without rewriting old results or inventing a Task Failure. Agents stop writes on `work_item_cancelled`.

### History and Detail

`GET /work-items/{id}/context` is readable in every lifecycle state and returns normalized Tasks and Relations, complete Claim history, and active-Claim projections.

`GET /tasks/{id}` is a viewer-oriented Detail containing responsibility, outcome, current Review, history, submitted Artifacts, and capabilities. It does not require execution eligibility. `/tasks/{id}/context` is protected execution context and should not load ordinary detail pages.

### Artifacts

Workflow Task Definitions may require named Artifacts. An executor registers an absolute external URI or uploads to the single managed Store, then submits the staged IDs. Staged Artifacts belong to one Claim; submitted Artifacts are visible across the WorkItem.

Managed uploads use stable `kairos://` URIs, SHA-256 digests, and a recoverable register-before-write flow. GC removes old unsubmitted Artifacts, pending uploads, and expired idempotency records; submitted Artifacts remain. Large files should use durable external storage and URI registration.

## Key Limits

The table helps operational planning. OpenAPI and server validation remain authoritative.

| Scope | Limit |
| --- | --- |
| Workflow Definition | 100 Task Definitions; 1,000 Relations |
| Workflow node instances | Default 100 per node and WorkItem; configurable to 500 |
| Blackboard WorkItem | 1,000 Tasks; 10,000 Relations |
| Claim history | 128 Coordination Claims per WorkItem; 128 Claims per Task |
| Task-associated history | 64 each of Submissions, Reviews, ordinary Failures, Transition Decisions, and Artifacts |
| Historical text field | 32 KiB UTF-8 |
| `find_work` | Default 5 and maximum 50 per candidate kind |

Reaching a history safety limit fails the WorkItem and ends active Claims while retaining accepted history. Store long content in Artifacts or durable external storage and keep a summary plus absolute URI in lifecycle text.

## Claim Leases

Agent Task and Coordination Claims use leases; Human Claims do not. An Agent may request 15 seconds to 30 minutes; omission uses the server default.

`lease_until` is when the reaper may first recover the Claim, not an instant revocation. Until recovery commits, the current Agent may renew or perform protected operations. Afterwards the old Claim ID remains a fencing token and cannot revive.

## Daemon Platform Observation

Each `kairos-daemon` start creates a new instance ID, registers with its Agent Identity Token, and reports snapshots and meaningful events about every 15 seconds. Telemetry failure never changes scheduling, Claims, heartbeats, or business outcomes.

Core computes connectivity from receipt time: `reporting` within 45 seconds, `stale` afterwards, and `stopped` only after an explicit report. Lost contact does not mean a Claim ended. Events are retained for 30 days and inactive instances for 90 days.

Only the owning Agent reports; only Humans read. Snapshots and events are operational observations, while WorkItem, Task, and Claim records remain authoritative. See the [observability design](daemon-observability-design.zh-CN.md) for the full contract.

## MCP Tools

MCP and HTTP share identity resolution and application authorization. Identity comes from transport, never tool arguments. Executor sessions expose only profile-allowed tools, and Agent Daemon owns Claim lifecycle.

| Category | Tools |
| --- | --- |
| Discovery and context | `find_work`, `get_task_context`, `get_work_item_context` |
| Task Claim and delivery | `claim_task`, `heartbeat_claim`, `create_artifact`, `upload_artifact`, `release_claim`, `submit_task`, `fail_task` |
| Coordination Claim | `claim_work_candidate`, `heartbeat_coordination_claim`, `release_coordination_claim` |
| Blackboard planning and closure | `create_blackboard_task`, `add_blackboard_relation`, `decompose_blackboard_task`, `add_blackboard_child_task`, `skip_blackboard_task`, `submit_blackboard_completion`, `accept_blackboard_completion` |

Resource-creating tools require `operation_id`: `claim_task`, `claim_work_candidate`, `create_artifact`, `upload_artifact`, `create_blackboard_task`, `decompose_blackboard_task`, and `add_blackboard_child_task`. Same retries return the original resource; changed parameters require a new ID.

`upload_artifact` accepts standard Base64 without a data-URI prefix and is intended for small files. Register a durable external URI with `create_artifact` for large content.

Project Codex configuration lives in `.codex/config.toml`; execution guidance lives in `.agents/skills/kairos-agent/SKILL.md`.
