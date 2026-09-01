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
KAIROS_HTTP_READ_TIMEOUT=60s \
KAIROS_HTTP_WRITE_TIMEOUT=120s \
KAIROS_HTTP_IDLE_TIMEOUT=120s \
go run ./cmd/kairos-server
```

Set `KAIROS_POSTGRES_DSN` to use PostgreSQL instead of SQLite:

```bash
KAIROS_POSTGRES_DSN='postgres://kairos:<password>@127.0.0.1:5432/kairos?sslmode=disable' \
go run ./cmd/kairos-server
```

When `KAIROS_POSTGRES_DSN` is non-empty it takes precedence over `KAIROS_SQLITE_PATH`. Startup verifies the connection and applies the embedded PostgreSQL migrations before serving requests; an invalid or unavailable explicitly configured database causes startup to fail.

The built-in local persistence is private to the operating-system user running Kairos. SQLite database, WAL, and shared-memory files are forced to mode `0600`; existing database files are tightened when opened. The managed Artifact root must be a dedicated non-root, non-symlink directory with mode `0700`; Kairos rejects an existing root with broader permissions instead of changing it. Hash directories and managed files are created with `0700` and `0600`, including files replaced by a retry. Deployments that intentionally share these paths between operating-system users must configure access outside Kairos rather than relying on group-readable defaults.

The HTTP read timeout bounds the total time spent reading a request, including JSON, MCP, and managed Artifact uploads. Go applies the absolute write deadline after reading the request headers, so reading the body, running the handler, and writing the response share that budget; it is not a separate response-only timer. The default write timeout is therefore twice the read timeout and also bounds Artifact downloads. The idle timeout bounds the gap between requests on a keep-alive connection. All three settings use Go duration syntax and must be positive.

For an internet-facing deployment, place Kairos behind a reverse proxy that terminates TLS and enforces connection and request-rate limits. Configure the proxy's upstream timeouts slightly above the corresponding Kairos timeouts so Kairos closes slow requests predictably. A proxy may intentionally impose a smaller upload limit; otherwise its request-body limit must allow `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` plus multipart overhead.

Runtime fields used by discovery, authorization narrowing, filtering, or ordering are stored in dedicated columns as well as in the aggregate payload. PostgreSQL uses native `TEXT[]` columns for WorkItem/Task tags and Task allowed roles, with GIN indexes for the current containment queries. SQLite stores the same logical fields as validated JSON-array `TEXT` columns and evaluates containment through `json_each`; SQLite is intended for local and smaller deployments. Empty collections are stored and returned as arrays, never `null`.

Database timestamps are normalized at the application boundary to UTC with microsecond precision, so API values, aggregate payloads, and query columns use the same instant. PostgreSQL uses `TIMESTAMPTZ`; SQLite uses a fixed-width RFC 3339 UTC representation so textual range comparisons preserve chronological order. Definitions, WorkItems, Tasks, Workflow activations, and Identities have `created_at` and `updated_at` matching their domain metadata. Task Relations persist their immutable `created_at`; mutable Claims, staged Artifacts, and idempotency records update `updated_at` when their state changes. Rows with a more specific immutable-event or lifecycle timestamp retain names such as `occurred_at`, `claimed_at`, or `applied_at` instead of adding a meaningless duplicate timestamp.

`GET /healthz` is unauthenticated. HTTP management and execution routes use `/api/v1`; Streamable HTTP MCP uses `/mcp`.

## HTTP Contract

The machine-readable <a href="{{ '/openapi.yaml' | relative_url }}">OpenAPI 3.1 document</a> is the exact contract for all 43 registered HTTP operations. It defines authentication, path and query parameters, JSON and multipart request bodies, response status codes, enums, defaults, binary Artifact downloads, and every response field. This guide keeps the behavioral context that does not belong in a schema.

All API JSON field names use `snake_case`. JSON request objects are closed contracts; an unknown field is rejected with `400 invalid_request`, including unknown fields inside nested objects. JSON success responses use `{ "data": ... }`; JSON errors use `{ "error": { "code": string, "message": string } }`. Release and token-revocation operations return `204` without a body, `/healthz` returns `{ "status": "ok" }`, and Artifact content is returned as `application/octet-stream`.

Collection fields and list responses are always arrays, including when empty. Optional single values such as `active_claim_id`, `parent_task_id`, `current_review`, `workflow`, `blackboard`, completion timestamps, and cancellation actor/time are `null` when absent. Repeated `status`, `mode`, and `tag` query parameters are represented as repeated query keys. Common error codes are:

| Status | Code |
| --- | --- |
| `400` | `invalid_request` |
| `401` | `unauthenticated` |
| `403` | `forbidden` |
| `404` | `not_found` |
| `409` | `conflict` |
| `409` | `work_item_cancelled` |
| `413` | `artifact_too_large` |
| `500` | `internal_error` |

## Identity Modes

Trusted Mode accepts transport headers inside a trusted boundary:

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Kind: agent
X-Kairos-Actor-Role: backend
```

`X-Kairos-Actor-Kind` defaults to `agent`. Agent identities must provide `X-Kairos-Actor-Role`; Human identities must omit it. HTTP requests that create a WorkItem, Blackboard Task, Claim, external Artifact, decomposition, or child Task may provide a stable `Idempotency-Key`; an identical retry returns the original resource, while changed arguments require a new key. Definition appends use `base_version`, and lifecycle transitions evaluate current state instead of replaying an old response. Managed Artifact upload requires a stable key because file storage and database persistence cannot be committed in one transaction.

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
| Workflow Definitions | catalog `GET /api/v1/definitions/workflows`; latest `GET /{id}`; history and append `GET/POST /{id}/versions`; exact version `GET /{id}/versions/{version}` |
| Blackboard Definitions | catalog `GET /api/v1/definitions/blackboards`; latest `GET /{id}`; history and append `GET/POST /{id}/versions`; exact version `GET /{id}/versions/{version}` |
| WorkItems | `GET/POST /api/v1/work-items`, `GET /api/v1/work-items/{id}/context`, `POST /completion`, `POST /acceptance`, `POST /cancellation`; Coordination Claims use `POST /{id}/coordination-claims`, `POST /{id}/coordination-claims/{claim_id}/heartbeat`, and `DELETE /{id}/coordination-claims/{claim_id}` |
| Artifacts | `GET /api/v1/work-items/{id}/artifacts`, `POST /api/v1/tasks/{id}/artifacts`, `POST /api/v1/tasks/{id}/artifact-uploads`, `GET /api/v1/artifacts/{id}/content` |
| Discovery | `GET /api/v1/work` |
| Task detail and execution | `GET /api/v1/tasks/{id}`, `/context`, `/claims`, `/submissions`, `/failures`, `/reviews` |
| Blackboard planning | WorkItem Tasks, relations, completion; Task decomposition, children, and skipping |
| Human attention | `GET /api/v1/human-attention` |
| Identities | `GET/POST /api/v1/identities`, token rotation and revocation routes |

The WorkItem, Human Attention, Definition catalog, Definition version-history, and submitted Artifact list routes use cursor pagination. `limit` defaults to 50 and accepts 1-200. A page returns `{ "data": [...], "next_cursor": string | null }`; pass a non-null value back as `cursor` on the same collection route, preserving any filters. Cursors are opaque and collection-specific. An invalid cursor or limit returns `400 invalid_request`. WorkItems are ordered by `updated_at DESC, id ASC`, Human Attention puts Reviews first and otherwise orders by item update time, Definition catalogs by `id ASC`, version histories by `version DESC`, and Artifacts by `created_at ASC, id ASC`.

The operations console loads active and settled WorkItems through separate status-filtered cursors, so loading older history does not affect the active-work queue.

Each Definition catalog returns the maximum stored version for every ID. `GET /definitions/{mode}/{id}` returns that latest version, while `GET /definitions/{mode}/{id}/versions` pages that ID's immutable history. The console resolves an unknown ID or version directly and does not scan unrelated catalog pages.

Definition IDs contain only lowercase ASCII letters, digits, and hyphens (`^[a-z0-9-]+$`).

Creating a new Definition ID omits `base_version` and receives version 1. Appending a version must send the latest version the editor was based on as `base_version`. The server compares it under the Definition lock and assigns `max(version) + 1`; a missing or stale base for an existing ID returns `409 conflict`.

Creating a WorkItem intentionally accepts a Definition ID and mode rather than a version. At creation time the server resolves and binds the maximum stored version for that ID.

Definition versions are immutable. Workflow WorkItems instantiate start Tasks from the graph; empty Blackboard WorkItems remain planning candidates. After Blackboard Tasks converge, `find_work` returns `blackboard_completion`; a collaborator either creates more Tasks or posts a durable completion result. That submission then applies `acceptance_mode`: `none` (default) completes immediately, `agent` returns `work_item_acceptance`, and `human` enters human acceptance. Acceptance is a separate `POST /acceptance` action. Agent acceptance candidates are visible only to Agent identities. Before an Agent reasons about `empty_blackboard`, `blackboard_completion`, or `work_item_acceptance`, it must create a WorkItem Coordination Claim. The active Claim hides that candidate from discovery; the chosen Task creation, completion submission, or acceptance must carry its ID and ends it atomically.

The operations console exposes these WorkItem lifecycle decisions only to Human identities. Agents execute them through the MCP discovery and Coordination Claim loop, which establishes the Claim before full context is loaded and reasoning begins.

Workflow Definitions accept at most 100 Task Definitions and 1,000 Relation Definitions. Start Task IDs must be unique, refer to required Tasks in the graph, and are therefore bounded by the Task Definition limit. `max_task_executions` defaults to 100 when sent as zero and may not exceed 500; it limits total runtime Task instances for one WorkItem. These limits protect graph size and cyclic execution without adding duplicate runtime graph checks.

`find_work` returns unclaimed candidates in `work_item_acceptance`, `blackboard_completion`, `task`, then `empty_blackboard` groups. Its `limit` applies independently to each group, defaults to 5 when omitted or zero, and accepts values up to 50. An Agent can therefore receive at most four times the limit; a Human does not receive Agent-acceptance candidates. Each group is bounded in its database query rather than after loading the complete candidate set.

One Blackboard WorkItem may contain at most 1,000 Task instances and 10,000 suggested Relations, including completed history and Tasks created as decomposition children. The server checks these hard ceilings inside the write transaction for root Task creation, decomposition, child creation, and Relation creation; an operation that would exceed a ceiling returns `409 conflict` and does not create a partial result.

To keep executor and WorkItem contexts bounded without truncating history, each WorkItem accepts at most 128 Coordination Claims, and each Task accepts at most 128 Claims, 64 Submissions, 64 Reviews, 64 ordinary Failures, 64 Transition Decisions, and 64 Artifacts. Attempting to append beyond any history ceiling fails the WorkItem and ends active Claims; the rejected operation does not append another history record. A terminal `fail_work_item` Failure may add one final record, so the Failure history can contain 65 records in that case. Accepted history remains complete in context responses. Result, reason, retry prompt, feedback, transition reason, and event message values retained in history are limited to 32 KiB of UTF-8 encoded bytes. A Task Submission can bind longer deliverables as Artifacts through `artifact_ids` while keeping a concise Result. For text-only lifecycle actions that do not accept Artifact IDs, including failure and retry details, Review feedback, cancellation reasons, and skip reasons, store longer material in durable external storage and include a concise summary and absolute URI in the text field.

Each Workflow Definition `graph.relations[]` entry accepts optional `label` and `agent_guidance` strings. Empty strings mean no additional guidance. Neither field changes graph compilation or progression semantics. HTTP Workflow Task context exposes complete guidance in each Choice Group's `relations`. MCP `get_task_context` annotates the corresponding `targets[]` entry with merged `relation_guidance` (preferring `agent_guidance`, then `label`), avoiding duplicate target structures.

For Blackboard, `work_item.result` contains the submitted completion proposal while acceptance is pending and the accepted final outcome after acceptance. Reopening Agent acceptance by creating another Task clears the stale proposal. Workflow completion is structural, so Workflow WorkItems keep `result` empty; their durable outcomes remain on Task Submissions and Artifacts.

`POST /api/v1/work-items/{id}/cancellation` is a Human-only management action for `open`, `awaiting_agent_acceptance`, and `awaiting_human_acceptance` WorkItems. It requires a non-empty `reason` and records `cancelled_at`, `cancelled_by`, and `cancellation_reason`; any pending completion proposal is cleared. In the same transaction, every active Task Claim and Coordination Claim ends with `work_item_cancelled`, each claimed Task loses `active_claim_id`, and a `working` Task returns to `pending`. Existing Task outcomes are not rewritten and no Task Failure is created. A cancelled WorkItem remains readable, while subsequent Task mutations return `409 work_item_cancelled`; agents should stop when they receive that response. Cancellation is not exposed as an MCP tool.

`GET /api/v1/work-items/{id}/context` remains available after any terminal state and returns the WorkItem, normalized Task and relation collections, complete Task Claim history in `claims`, the currently live Task subset in `active_claims`, complete WorkItem decision history in `coordination_claims`, and its optional live value in `active_coordination_claim`. Empty histories are encoded as `[]`; the optional active Coordination Claim is `null` when absent. The returned `work_item.result` follows the mode-specific semantics above: it contains the completion proposal or accepted outcome for Blackboard and remains empty for Workflow, whose outcomes are available through Task Submissions and Artifacts. A completed Task's executor can be resolved through `submission.claim_id -> claims[].id -> executor`.

Workflow Task Definitions may declare required `artifacts[]` entries with only `name` and `description`. The description is an execution instruction, not a file-type schema. An executor creates external Artifacts with an absolute URI or uploads managed content while holding a Claim, then passes their IDs in `artifact_ids` to `submit_task`. Submission atomically binds the staged Artifacts and rejects a Workflow result missing a declared name. Blackboard Tasks have no structured Artifact contract. Submitted Artifacts are visible throughout the WorkItem; staged Artifacts remain with their creating Claim.

Managed upload always targets the server's single managed Store; callers cannot select a Store. The bundled implementation registers a stable `kairos://` upload URI in the pending database record before writing bytes below `KAIROS_ARTIFACT_DIR`, then flushes the file and directory chain before completing the database operation; the resulting Blob metadata stores a SHA-256 integrity digest separately.

`POST /tasks/{id}/artifacts` accepts JSON fields `claim_id`, `name`, and `uri`. `POST /tasks/{id}/artifact-uploads` accepts multipart fields `claim_id`, `name`, and `file`; it has no Store field. `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` sets the uploaded file content limit and defaults to 16 MiB; an oversized upload returns `413 artifact_too_large`. The bundled managed upload is a small-file convenience path. Large deliverables should be published to durable external storage such as S3 and registered through the URI endpoint. External-URI creation accepts the optional resource-creation key; managed upload requires one so the server can persist the upload URI and pending state before writing content to the Store. The Store computes and returns the digest and size, which are persisted in the pending record before the final transaction creates Blob metadata, the staged Artifact, and the completed operation. A pending retry rewrites the registered URI and verifies the previously recorded digest and size, so it can recover even when cleanup removed the file but failed to delete the pending record. An identical completed retry can recover its retained staged Artifact after the Claim ends while the upload record remains inside the configured retention window.

Artifact GC runs every `KAIROS_ARTIFACT_GC_INTERVAL` (15 minutes by default). An unsubmitted Artifact becomes eligible when its Claim is no longer active and the Artifact is older than `KAIROS_ARTIFACT_GC_RETENTION` (24 hours by default). Pending managed-upload records older than the same retention are deleted together with the file at their registered upload URI; completed external-registration and managed-upload replay records expire after that retention window as well. Submitted Artifacts are retained. Managed Blob content and metadata are deleted only after no Artifact URI references that Blob. All three Artifact numeric or duration settings must be positive.

`GET /api/v1/tasks/{id}` is a viewer-facing Task Detail endpoint and does not require the current identity to be able to execute the Task. It returns backend-projected `responsibility`, `outcome`, `current_review`, normalized `history`, submitted `artifacts` belonging to that Task, and identity-specific `capabilities`. The `artifacts` collection is `[]` when the Task has no submitted deliverables. `GET /api/v1/tasks/{id}/context` remains an executor context protected by execution authorization; clients must not use it to load ordinary detail or human Review operations.

## Claim Leases

Agent Task Claims and WorkItem Coordination Claims use leases; human operations do not. Agent claim and heartbeat requests may choose a duration from 15 seconds through 30 minutes. Omitted durations use `KAIROS_AGENT_CLAIM_LEASE`, five minutes by default. `lease_until` is the earliest time when the background reaper may end the Claim and return its Task or lifecycle candidate to discovery; reaching that timestamp does not itself change ownership. Until the reaper commits that transition, the current Agent may continue the protected operation or renew the Claim, while every other Agent remains unable to claim that work. After reaping, the old Claim ID remains fenced and cannot be renewed or used for a lifecycle mutation.

## MCP

MCP shares the HTTP identity resolver. Trusted Mode supplies actor headers at transport level; Authenticated Mode supplies `Authorization: Bearer <identity-token>`. Identity never appears in tool arguments.

The execution surface contains twenty tools:

- discovery and context: `find_work`, `get_task_context`, `get_work_item_context`;
- Task Claim lifecycle and delivery: `claim_task`, `heartbeat_claim`, `create_artifact`, `upload_artifact`, `release_claim`, `submit_task`, `fail_task`;
- Coordination Claim lifecycle: `claim_work_candidate`, `heartbeat_coordination_claim`, `release_coordination_claim`;
- Blackboard planning and closure: `create_blackboard_task`, `add_blackboard_relation`, `decompose_blackboard_task`, `add_blackboard_child_task`, `skip_blackboard_task`, `submit_blackboard_completion`, `accept_blackboard_completion`.

Only resource-creating MCP tools require `operation_id`: `claim_task`, `claim_work_candidate`, `create_artifact`, `upload_artifact`, `create_blackboard_task`, `decompose_blackboard_task`, and `add_blackboard_child_task`. These tools replay an identical retry so a lost response does not orphan a server-generated ID; changed arguments require a new ID. Lifecycle transitions and relation creation instead evaluate the current domain state and may return a conflict when retried after success. Workflow discovery is determined by role and graph state and ignores tag filters; Blackboard discovery may use tags. Workflow Task context exposes controlled upstream summaries, durable results, and optional Relation guidance without granting arbitrary access to other Tasks or creating branches absent from the Definition.

`upload_artifact` accepts standard Base64 bytes in `content_base64` without a data URI prefix, decodes them into the server-configured Artifact Store, and returns the staged Artifact ID used by `submit_task.artifact_ids`. Its decoded size limit is the same `KAIROS_ARTIFACT_MAX_UPLOAD_BYTES` used by HTTP multipart uploads; the MCP request-body limit includes the corresponding Base64 expansion. This tool is intended for small files only because Base64 adds roughly one third to the transfer size and the MCP request is buffered in memory. Use `create_artifact` with an S3 or other durable external URI for large files.

Project Codex configuration is in `.codex/config.toml`; execution guidance is in `.agents/skills/kairos-agent/SKILL.md`.
