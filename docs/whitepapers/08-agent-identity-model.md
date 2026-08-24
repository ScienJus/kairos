# Kairos Agent Identity Model

> Trusted identity, role, and the work an agent may participate in

## Abstract

Kairos provides agents with native identity. An Agent Identity contains a stable, readable identifier and one role, and is authenticated with a Token. An agent only needs to carry its Token; Kairos resolves its identity and returns the Tasks it is eligible to discover and claim for execution.

Kairos also supports Trusted Mode. In a trusted environment, an agent can declare its own id and role without a Token. Both modes use the same Task discovery and execution model but provide different levels of identity assurance.

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

Blackboard tags describe work categories and discovery hints; they do not automatically become access permissions. A Task that needs restriction should explicitly configure its allowed roles.

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

> Agent identity establishes who is acting; its role defines which work the agent may participate in.
