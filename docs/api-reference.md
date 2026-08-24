# Kairos API Reference

[简体中文](api-reference.zh-CN.md)

This reference covers transport, authentication, HTTP resources, MCP tools, and execution response contracts. Product concepts and coordination semantics remain in the README and whitepapers.

## Server

The default server uses SQLite and Trusted Mode:

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

Set `KAIROS_POSTGRES_DSN` to use PostgreSQL instead of SQLite:

```bash
KAIROS_POSTGRES_DSN='postgres://kairos:<password>@127.0.0.1:5432/kairos?sslmode=disable' \
go run ./cmd/kairos-server
```

When `KAIROS_POSTGRES_DSN` is non-empty it takes precedence over `KAIROS_SQLITE_PATH`. Startup verifies the connection and applies the embedded PostgreSQL migrations before serving requests; an invalid or unavailable explicitly configured database causes startup to fail.

`GET /healthz` is unauthenticated. HTTP management and execution routes use `/api/v1`; Streamable HTTP MCP uses `/mcp`.

## Identity Modes

Trusted Mode accepts transport headers inside a trusted boundary:

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`X-Kairos-Actor-Kind` defaults to `agent`. Human identities have no role. Mutation requests should provide `Idempotency-Key`; an identical retry reuses the same key, while changed arguments require a new key.

Authenticated Mode is intended for shared environments within one trusted collaboration group:

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<at-least-32-character-high-entropy-token>' \
go run ./cmd/kairos-server
```

Admin identity routes require `Authorization: Bearer <admin-token>`. Work routes require an issued identity token. Authenticated Mode ignores trusted actor headers.

Authentication establishes caller identity and operation-specific rules still constrain actions such as Task discovery, claiming, and Claim-owned execution. It is not a data-isolation boundary: all issued identities belong to one global trust domain, and Kairos does not currently partition reads or writes by tenant, team, project, or object. Deploy separate Kairos instances when groups must not access one another's data.

The operations console discovers the configured mode through the public `GET /api/v1/auth/config` endpoint. In Authenticated Mode it presents a Token login before loading any workspace data, validates the Token through `GET /api/v1/session`, and then uses it as the Bearer credential for API requests, managed uploads, and Artifact downloads. The Token is held only in browser `sessionStorage`, so it is scoped to the current tab session rather than persisted as a durable browser login; the console reports an unavailable state if the browser blocks that storage. Signing out clears the Token and cached API data. A `401` response, including one caused by Token revocation or rotation, also clears the session and returns the console to login.

`GET /api/v1/auth/config` is unauthenticated and returns `{ "data": { "mode": "trusted" | "authenticated" } }`. `GET /api/v1/session` uses the normal work-route authentication and returns the transport-resolved identity as `{ "data": { "id": string, "kind": "human" | "agent", "role": string } }`. Clients should use this resolved identity rather than deriving identity fields from a Token.

## HTTP Resources

| Resource | Routes |
| --- | --- |
| Authentication and session | `GET /api/v1/auth/config`, `GET /api/v1/session` |
| Workflow Definitions | `GET/POST /api/v1/definitions/workflows`, `GET /api/v1/definitions/workflows/{id}/versions/{version}` |
| Blackboard Definitions | `GET/POST /api/v1/definitions/blackboards`, `GET /api/v1/definitions/blackboards/{id}/versions/{version}` |
| WorkItems | `GET/POST /api/v1/work-items`, `GET /api/v1/work-items/{id}/context`, `POST /completion`, `POST /acceptance` |
| Artifacts | `GET /api/v1/work-items/{id}/artifacts`, `POST /api/v1/tasks/{id}/artifacts`, `POST /api/v1/tasks/{id}/artifact-uploads`, `GET /api/v1/artifacts/{id}/content` |
| Discovery | `GET /api/v1/work` |
| Task detail and execution | `GET /api/v1/tasks/{id}`, `/context`, `/claims`, `/submissions`, `/failures`, `/reviews` |
| Blackboard planning | WorkItem Tasks, relations, completion; Task decomposition, children, and skipping |
| Human attention | `GET /api/v1/human-attention` |
| Identities | `GET/POST /api/v1/identities`, token rotation and revocation routes |

Definition versions are immutable. Workflow WorkItems instantiate start Tasks from the graph; empty Blackboard WorkItems remain planning candidates. After Blackboard Tasks converge, `find_work` returns `blackboard_completion`; a collaborator either creates more Tasks or posts a durable completion result. That submission then applies `acceptance_mode`: `none` (default) completes immediately, `agent` returns `work_item_acceptance`, and `human` enters human acceptance. Acceptance is a separate `POST /acceptance` action. Agent acceptance candidates are visible only to Agent identities. Lifecycle decisions are returned before executable or planning candidates, and `limit` applies globally across those candidate kinds.

Each Workflow Definition `graph.relations[]` entry accepts optional `label` and `agent_guidance` strings. Empty strings mean no additional guidance. Neither field changes graph compilation or progression semantics. HTTP Workflow Task context exposes complete guidance in each Choice Group's `Relations`. MCP `get_task_context` annotates the corresponding `targets[]` entry with merged `relation_guidance` (preferring `agent_guidance`, then `label`), avoiding duplicate target structures.

While acceptance is pending, `WorkItem.Result` contains the submitted completion proposal. After acceptance, the same field contains the accepted final outcome. Reopening Agent acceptance by creating another Task clears the stale proposal.

`GET /api/v1/work-items/{id}/context` remains available after terminal completion and returns the aggregate result, normalized Task and relation collections, complete Claim history in `Claims`, and the currently live subset in `ActiveClaims`. A completed Task's executor can be resolved through `Submission.ClaimID -> Claims[].ID -> Executor`.

Workflow Task Definitions may declare required `artifacts[]` entries with only `name` and `description`. The description is an execution instruction, not a file-type schema. An executor creates external Artifacts with an absolute URI or uploads managed content while holding a Claim, then passes their IDs in `artifact_ids` to `submit_task`. Submission atomically binds the staged Artifacts and rejects a Workflow result missing a declared name. Blackboard Tasks have no structured Artifact contract. Submitted Artifacts are visible throughout the WorkItem; staged Artifacts remain with their creating Claim.

Managed upload always targets the server's single managed Store; callers cannot select a Store. The bundled implementation registers a stable `kairos://` upload URI in the pending database record before writing bytes below `KAIROS_ARTIFACT_DIR`, then flushes the file and directory chain before completing the database operation; the resulting Blob metadata stores a SHA-256 integrity digest separately.

`POST /tasks/{id}/artifacts` accepts JSON fields `claim_id`, `name`, and `uri`. `POST /tasks/{id}/artifact-uploads` accepts multipart fields `claim_id`, `name`, and `file`; it has no Store field. `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` sets the uploaded file content limit and defaults to 16 MiB; an oversized upload returns `413 artifact_too_large`. The bundled managed upload is a small-file convenience path. Large deliverables should be published to durable external storage such as S3 and registered through the URI endpoint. External-URI creation uses the normal optional `Idempotency-Key`; managed upload requires it so the server can persist the upload URI and pending state before writing content to the Store. The Store computes and returns the digest and size, which are persisted in the pending record before the final transaction creates Blob metadata, the staged Artifact, and the completed operation. A pending retry rewrites the registered URI and verifies the previously recorded digest and size, so it can recover even when cleanup removed the file but failed to delete the pending record. An identical completed retry can recover its retained staged Artifact after the Claim ends; once GC removes that Artifact, it cannot be recovered through the old key.

Artifact GC runs every `KAIROS_ARTIFACT_GC_INTERVAL` (15 minutes by default). An unsubmitted Artifact becomes eligible when its Claim is no longer active and the Artifact is older than `KAIROS_ARTIFACT_GC_RETENTION` (24 hours by default). Pending managed-upload records older than the same retention are deleted together with the file at their registered upload URI; completed idempotency records are retained. Submitted Artifacts are retained. Managed Blob content and metadata are deleted only after no Artifact URI references that Blob. All three Artifact numeric or duration settings must be positive.

`GET /api/v1/tasks/{id}` is a viewer-facing Task Detail endpoint and does not require the current identity to be able to execute the Task. It returns backend-projected `Responsibility`, `Outcome`, `CurrentReview`, normalized `History`, submitted `Artifacts` belonging to that Task, and identity-specific `Capabilities`. The `Artifacts` collection is `[]` when the Task has no submitted deliverables. `GET /api/v1/tasks/{id}/context` remains an executor context protected by execution authorization; clients must not use it to load ordinary detail or human Review operations.

## Claim Leases

Agent Claims use leases; human Claims do not. Agent claim and heartbeat requests may choose a duration from 15 seconds through 30 minutes. Omitted durations use `KAIROS_AGENT_CLAIM_LEASE`, five minutes by default. `lease_until` is the earliest time when the background reaper may end the Claim and return its Task to Pending; reaching that timestamp does not itself change ownership. Until the reaper commits that transition, the current executor may continue Task operations or renew the Claim, while every other executor remains unable to Claim the working Task. After reaping, the old Claim ID remains fenced and cannot be renewed or used for submission.

## MCP

MCP shares the HTTP identity resolver. Trusted Mode supplies actor headers at transport level; Authenticated Mode supplies `Authorization: Bearer <identity-token>`. Identity never appears in tool arguments.

The execution surface contains seventeen tools:

- discovery and context: `find_work`, `get_task_context`, `get_work_item_context`;
- Claim lifecycle and delivery: `claim_task`, `heartbeat_claim`, `create_artifact`, `upload_artifact`, `release_claim`, `submit_task`, `fail_task`;
- Blackboard planning and closure: `create_blackboard_task`, `add_blackboard_relation`, `decompose_blackboard_task`, `add_blackboard_child_task`, `skip_blackboard_task`, `submit_blackboard_completion`, `accept_blackboard_completion`.

Every MCP mutation requires an `operation_id`. Reuse it only for an identical retry. Workflow discovery is determined by role and graph state and ignores tag filters; Blackboard discovery may use tags. Workflow Task context exposes controlled upstream summaries, durable results, and optional Relation guidance without granting arbitrary access to other Tasks or creating branches absent from the Definition.

`upload_artifact` accepts standard Base64 bytes in `content_base64` without a data URI prefix, decodes them into the server-configured Artifact Store, and returns the staged Artifact ID used by `submit_task.artifact_ids`. Its decoded size limit is the same `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` used by HTTP multipart uploads; the MCP request-body limit includes the corresponding Base64 expansion. This tool is intended for small files only because Base64 adds roughly one third to the transfer size and the MCP request is buffered in memory. Use `create_artifact` with an S3 or other durable external URI for large files.

Project Codex configuration is in `.codex/config.toml`; execution guidance is in `.agents/skills/kairos-agent/SKILL.md`.
