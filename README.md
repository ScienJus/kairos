# Kairos

English | [简体中文](README.zh-CN.md)

Kairos coordinates tasks shared by people and agents.

Agent harnesses such as Codex and Claude Code are good at running an agent. Kairos focuses on the collaboration around those agents: what work exists, who is responsible for it, what has already been delivered, and what can happen next.

It does not start or stop agents, choose models, or manage sandboxes. The integration model lets agents connect proactively through MCP / Skills, while a Bridge can dispatch Tasks to a harness when automated startup is needed.

## Why Kairos

Kairos gives every participant one durable view of the work:

- people and agents discover Tasks from the same WorkItem;
- an atomic Claim prevents two executors from working on the same Task at once;
- submissions, Reviews, feedback, and failures remain with the Task instead of an agent session;
- Tasks can be executed by an agent, a person, or either;
- both structured processes and open-ended collaboration use the same execution protocol.

```text
find work → choose → claim → execute + heartbeat → submit → complete
                                                  └── Review → approve / reject
```

## Choose a Coordination Mode

| Use | When it fits |
| --- | --- |
| **Workflow** | The process is known in advance and the system must enforce dependencies and required steps. |
| **Blackboard** | The objective is clear, but the plan should evolve as people and agents learn during execution. |

### Workflow

Workflow defines the legal choice space while allowing executors to make decisions at configured points.

Supported collaboration capabilities:

- **Dependencies**: downstream Tasks become available only after their prerequisites end.
- **Parallelism and joins**: multiple Tasks can run in parallel, and downstream work can wait for several predecessors.
- **Role constraints**: only agents with matching roles can discover and Claim a Task.
- **Autonomous selection**: executors choose from all currently legal Tasks.
- **Autonomous skipping**: upstream executors decide whether Optional Tasks are needed; decisions are combined at joins.
- **Autonomous Review**: a Task can require no Review, let the executor decide, or require Review.
- **Cycles**: executors can continue through a cycle path or exit it, with a maximum execution safeguard.
- **Automatic completion**: the WorkItem completes after every selected path closes.

### Blackboard

Blackboard keeps planning with the collaborators instead of fixing the Task Graph in advance.

Supported collaboration capabilities:

- **Blank planning**: an agent can discover an empty WorkItem and create its first Task.
- **Dynamic planning**: collaborators continuously add Tasks and organize discovery with tags.
- **Suggested dependencies**: relations provide shared guidance without blocking execution.
- **Dynamic skipping**: obsolete Pending Tasks can be skipped with a reason.
- **Task decomposition**: a claimed Task can be decomposed into nested child Tasks before producing a result.
- **Open subtrees**: collaborators can append children until an aggregate Task closes; parents complete recursively.
- **Dynamic Review**: an executor can request human Review when submitting a result.
- **Continuous expansion**: an executor creates follow-up Tasks before ending the current Task when more work is needed.
- **Automatic completion**: the WorkItem completes when its final Task ends; an empty Blackboard can also complete directly.

## Shared Execution Semantics

A Claim establishes exclusive execution responsibility. Submitting a result ends the Claim. If Review is requested, the Task waits without holding an agent alive:

```text
Working
  ├── submit ─────────────→ Completed
  ├── submit for Review ──→ InReview
  │                           ├── approve → Completed
  │                           └── reject  → Pending → Claim again
  └── fail
       ├── reopen Task
       └── fail WorkItem
```

Every submission and Review round is preserved. When an executor retries a failed or rejected Task, it receives the earlier results, all Review feedback, and any retry prompt as shared context.

## Human Interaction

The operations console currently provides a WorkItem List, a human-attention queue, and WorkItem detail views. Inside a WorkItem:

- Workflow is shown as a flow graph with execution history.
- Blackboard is shown as a dynamic checklist with hierarchy and suggested relations.

Kanban remains planned as a view of complete WorkItems. It will not implement either coordination mode.

## Project Status

Kairos currently includes a Go core engine and a runnable HTTP service, but it is not yet a final end-user service.

Available in this repository:

- domain model and Application Services;
- Workflow and Blackboard runtime semantics;
- PostgreSQL and SQLite persistence;
- concurrency and idempotency protection;
- persisted single-role identities, Trusted / Authenticated Mode, and Token lifecycle management;
- stateless Streamable HTTP MCP execution tools and a repository-level Codex Skill;
- an operations console with WorkItem List, human attention, Workflow graph, Blackboard task map, and Definition editors;
- agent Claim leases with flexible durations, heartbeat, expiry recovery, and fencing;
- deterministic unit tests and randomized collaboration simulations.

Still to be built:

- a Bridge for automatic dispatch;
- the remaining Kanban and operational-console workflows.

For development, use Go 1.26.5 or later and run:

```bash
go test ./...
```

## HTTP API

The server uses Trusted Mode and SQLite by default:

```bash
KAIROS_SQLITE_PATH=kairos.db \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_AGENT_CLAIM_LEASE=60s \
go run ./cmd/kairos-server
```

Every request except `/healthz` requires identity headers:

```text
X-Kairos-Actor-Id: codex-backend
X-Kairos-Actor-Role: backend
```

The Actor ID is both the stable identity and its readable name, so it should not be casually renamed. `X-Kairos-Actor-Kind` defaults to `agent`; set it to `human` for human review decisions. Trusted Mode must only be used when callers and transport are inside the trust boundary because the service accepts these headers without authentication. Mutation requests can use `Idempotency-Key` for durable idempotency.

Shared environments should use Authenticated Mode. The Admin Token must contain at least 32 characters and protects identity management APIs:

```bash
KAIROS_AUTH_MODE=authenticated \
KAIROS_ADMIN_TOKEN='<high-entropy-admin-token>' \
KAIROS_SQLITE_PATH=kairos.db \
go run ./cmd/kairos-server
```

Administrators call these endpoints with `Authorization: Bearer <admin-token>`:

- `POST /api/v1/identities` creates a one-to-one `Token → Actor ID → Role`; plaintext Token is returned only once;
- `GET /api/v1/identities` and `GET /api/v1/identities/{kind}/{id}` return identity data without Token hashes;
- `POST /api/v1/identities/{kind}/{id}/token` rotates the Token and immediately invalidates the old one;
- `DELETE /api/v1/identities/{kind}/{id}/token` revokes the Token while retaining Actor history.

Work requests use the issued `Authorization: Bearer <identity-token>`. Authenticated Mode ignores all `X-Kairos-Actor-*` headers; ID, Kind, and Role come only from the server-side identity record. An Agent has exactly one required Role, while a Human has none. Role and Actor ID remain stable together; create a separate Actor Identity when another Role is needed.

`/api/v1` exposes Definition management, WorkItem creation, Blackboard planning, work discovery, Task and WorkItem execution context, Claims, submissions, failures, decomposition, skipping, and review decisions. A terminal WorkItem remains readable at `GET /api/v1/work-items/{work_item_id}/context`, including its aggregated result and Task summaries. Definition versions are immutable; clients explicitly provide `version` when creating one.

Definitions use separate resources for the two coordination modes:

| | Workflow | Blackboard |
| --- | --- | --- |
| Create and list | `/definitions/workflows` | `/definitions/blackboards` |
| Read a version | `/definitions/workflows/{id}/versions/{version}` | `/definitions/blackboards/{id}/versions/{version}` |
| Definition content | A published version requires a valid formal Task Graph | Shared metadata and collaboration guidance only |
| After WorkItem creation | Start Tasks are instantiated immediately from the Graph | Remains empty and is discovered as a planning candidate |
| Further planning | Runtime follows formal relations and choice groups | Collaborators dynamically add Tasks, hierarchy, and suggested relations |

The real-SQLite Trusted and Authenticated HTTP flows are covered by `internal/httpapi/httpapi_test.go` and `internal/httpapi/authenticated_test.go`.

## MCP and Codex Skill

The server exposes a stateless Streamable HTTP MCP endpoint at `/mcp`. It uses the same identity resolver as `/api/v1`: Trusted Mode accepts `X-Kairos-Actor-*` transport headers, while Authenticated Mode requires the issued `Authorization: Bearer <identity-token>`. Identity values never appear in tool arguments.

The execution surface contains fourteen tools with compact `snake_case` structured results:

- `find_work`, `get_task_context`, and `get_work_item_context` for discovery and inspection, including terminal WorkItems;
- `claim_task`, `heartbeat_claim`, `release_claim`, `submit_task`, and `fail_task` for the Claim lifecycle;
- `create_blackboard_task` for planning an empty or open Blackboard.
- `add_blackboard_relation`, `decompose_blackboard_task`, `add_blackboard_child_task`, `skip_blackboard_task`, and `complete_blackboard` for dynamic Blackboard planning.

Definition management, Identity management, and human Review decisions intentionally remain HTTP-only. Every MCP mutation requires a caller-generated `operation_id`; retry the same logical call with the same ID and use a new ID when any argument changes.

The repository-level Codex Skill is available at `.agents/skills/kairos-agent`. Project-level Codex MCP configuration lives in `.codex/config.toml` and points to the default `http://127.0.0.1:8080/mcp` endpoint. Configure one transport identity before starting Codex from this trusted repository:

```bash
# Trusted Mode
export KAIROS_ACTOR_ID=codex-backend
export KAIROS_ACTOR_KIND=agent
export KAIROS_ACTOR_ROLE=backend

# Or Authenticated Mode
export KAIROS_IDENTITY_TOKEN='<issued-identity-token>'
```

Agent Claims use leases; human Claims do not. `claim_task` and `heartbeat_claim` accept an optional `lease_seconds`, which the server clamps to 15 seconds through 30 minutes. An omitted value uses `KAIROS_AGENT_CLAIM_LEASE` (60 seconds by default). The background reaper returns expired Agent Claims to Pending, and an expired Claim cannot be revived or used to submit a result.

Restart the Codex task after changing project MCP configuration or these environment variables. The Skill guides an agent through discover → inspect → claim → execute with heartbeat → submit/fail/release → verify. Real-SQLite Trusted and Authenticated MCP flows are covered by `internal/mcpapi/mcpapi_test.go`.

## Design Whitepapers

1. [Core Work Model](docs/whitepapers/01-core-work-model.md)
2. [Execution Collaboration Model](docs/whitepapers/02-execution-collaboration-model.md)
3. [Coordination Semantics](docs/whitepapers/03-coordination-semantics.md)
4. [Workflow Mode](docs/whitepapers/04-workflow.md)
5. [Blackboard Mode](docs/whitepapers/05-blackboard.md)
6. [Human Interaction Model](docs/whitepapers/06-human-interaction-model.md)
7. [Agent Interaction Model](docs/whitepapers/07-agent-interaction-model.md)
8. [Agent Identity Model](docs/whitepapers/08-agent-identity-model.md)
