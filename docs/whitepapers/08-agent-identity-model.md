# Kairos Agent Identity Model

> How Kairos knows which agent is acting and which Tasks it is allowed to take

## Abstract

Kairos needs to answer two questions for every agent request: who is acting, and which work may that agent take? An Agent Identity provides a stable identifier and one role. In Authenticated Mode, a Token proves that identity and Kairos returns only the Tasks it is eligible to discover and claim.

Local and already trusted environments can use Trusted Mode, where the runtime supplies the id and role directly. Task discovery and execution behave the same in both modes; only the strength and source of the identity proof changes.

## 1. Agent Identity

Agent Identity represents a stable agent identity inside Kairos:

```text
Agent Identity
├── id
├── role
└── credentials
```

- `id` is stable and readable, and is also used in presentation and collaboration records;
- `role` expresses the kind of work the agent can perform;
- `credentials` prove the identity.

The `id` should not be casually renamed because Claim ownership, idempotency records, and collaboration history all reference it. A future Agent Profile can add a mutable display label without changing identity.

An Agent Identity has exactly one role:

```text
id: codex-backend
role: backend
```

Roles remain simple and explicit and are defined by each project or team according to its own division of work. Create another Agent Identity when another role is required so one Token never implies multiple grants. Kairos does not require capability scoring or automatic matching for roles.

## 2. Token

A Token authenticates an Agent Identity:

```text
Token
  ↓ authenticate
Agent Identity
  ↓ resolve
id + role
```

An agent calling Kairos carries only the Token and does not repeatedly declare its id and role. The service stores only the Token hash; plaintext is returned only on issuance or rotation. A Token can be rotated or revoked without changing the Agent Identity or its collaboration history.

Tokens belong in the agent execution environment, not in WorkItems, Tasks, or project documentation.

## 3. Authenticated Mode

In Authenticated Mode, Kairos manages Agent Identities and issues Tokens:

```text
Agent supplies Token
        ↓
Kairos authenticates identity
        ↓
Use configured id and role
```

An agent cannot temporarily change its role through a request. Task discovery and claiming both use the identity information granted in Kairos. Authenticated Mode ignores Trusted Mode identity headers and accepts only a Bearer Token.

This mode is suited to one trusted collaboration group that requires explicit identity attribution and operation-specific execution constraints. It does not provide tenant, team, project, or object-level data isolation: all issued identities belong to one global trust domain. Mutually untrusted groups require separate Kairos instances. A future Team model may introduce an isolation boundary, but it is not part of the current identity contract.

In Authenticated Mode, sign in with the deployment `KAIROS_ADMIN_TOKEN` using the existing login form, then open the single **Token management** entry in the account menu (`/admin/identities`). Create a Human (no role) or an Agent (one required role, such as `developer`), inspect identity metadata, and rotate or revoke issued Tokens on this page. Rotation and revocation require confirmation and invalidate the previous Token immediately. The deployment-managed Admin credential is read-only here; change it through deployment configuration. Ordinary Identity Tokens, including `initial-human.token`, cannot access management. The server returns `can_manage_identities` on `/session`, true only for the configured Admin credential when identity management is available; the UI never derives access from an ID, role or display name. Every management endpoint still checks the credential.

Management uses the existing login credential in current-tab sessionStorage, with no second administrator session. Sign-out and a current-credential 401 clear login and cached workspace state. Newly issued Tokens stay only in page memory and are never placed in URLs, browser storage or Query/Mutation caches. Copy and save them before dismissing the result, starting another operation, navigating away or refreshing. Clipboard failure allows manual copying. List and detail responses never return plaintext Tokens. Failed write requests are not automatically retried because the operation may already have succeeded; refresh metadata before deciding whether to rotate a replacement. Trusted Mode retains local identity settings and does not expose management.

## 4. Trusted Mode

Trusted Mode is suited to local development, trusted networks, and other environments where identity is already guaranteed by the runtime:

```text
id: local-codex
role: backend
```

The agent declares its id and role without a Token. Kairos trusts the declaration and uses it for Task discovery and collaboration records.

The trust boundary of Trusted Mode is the runtime environment. It provides identity labels and role semantics but not the authentication guarantees of Authenticated Mode.

## 5. Roles and Workflow

In Workflow, a role is a formal constraint. A Task can configure the roles allowed to execute it:

```text
Task: Implement login API
executor: agent
roles: [backend]
```

For a Task to be visible to an agent, all of the following must hold:

```text
Workflow prerequisites satisfied
+ Task permits agent execution
+ Agent role matches
+ Task has no current Claim
```

Role is also validated when claiming. An agent with the `backend` role can claim the Task above; another agent cannot bypass the restriction by changing its query.

Workflow therefore uses the identity role to define the agent’s legal work space.

## 6. Roles and Blackboard

In Blackboard, the identity role primarily helps an agent discover relevant work:

```text
Agent role: backend
Task tags: [backend, auth]
```

Kairos can derive default tags or query scope from the agent role, after which the agent chooses using the Task description and current context.

Blackboard tags describe work categories and discovery hints; they do not automatically become access permissions. A Task that needs to restrict Agent execution should explicitly configure its allowed roles. Human execution is governed by the executor type and is not filtered by Agent roles.

Both Workflow Definition and Blackboard Definition can provide Suggested Tags such as `module:*`. Agents add concrete tags to Tasks from the actual work. A Definition supplies only the recommended vocabulary and does not require people to maintain every Task label continuously.

Blackboard therefore uses the identity role for discovery by default while allowing explicit constraints when needed.

## 7. AGENTS.md

`AGENTS.md` describes the work rules an agent must follow in a repository or directory. It solves a different problem from Agent Identity:

```text
Agent Identity → who the agent is and which role it has
AGENTS.md       → how work should be performed in this project
```

AGENTS.md belongs in the project repository:

- it changes with the code version;
- it can inherit and override rules by directory;
- it corresponds to the current checkout or worktree;
- the Agent Harness reads it during execution.

Kairos can provide repository and working-directory information on a Task so an agent can locate the applicable AGENTS.md. The platform does not need to copy or replace repository rules.

Kairos can host a lightweight Agent Profile with fields such as role, a display label, and description, while project execution rules remain in the repository.

> Identity tells Kairos who is acting. Role narrows the work that agent is allowed to take.

## Deployment administrator as Human

The deployment Admin Token also authenticates a stable, database-bound ordinary Human with an empty role over HTTP, MCP and the console. Its business permissions follow Human rules; identity administration remains exclusive to the configured credential. Rotation preserves the actor and requires restarting every instance. This does not grant Agent discovery or Executor privileges. See the [API reference](../api-reference.md#admin-token-business-identity) for persistence, collisions, migration and session semantics. The console shows `system admin` via optional session presentation metadata, keeping the actor ID unchanged. Admin configuration requires at least 32 visible ASCII characters (0x21–0x7E), excluding whitespace and controls.
