# Kairos Agent Identity Model

> Trusted identity, roles, and the work an agent may participate in

## Abstract

Kairos provides agents with native identity. An Agent Identity contains a stable identifier, name, and one or more roles, and is authenticated with a Token. An agent only needs to carry its Token; Kairos resolves its identity and returns the Tasks it can see and claim.

Kairos also supports Trusted Mode. In a trusted environment, an agent can declare its own name and roles without a Token. Both modes use the same Task discovery and execution model but provide different levels of identity assurance.

## 1. Agent Identity

Agent Identity represents a stable agent identity inside Kairos:

```text
Agent Identity
├── id
├── name
├── roles
└── credentials
```

- `id` is the stable identifier;
- `name` is used in presentation and collaboration records;
- `roles` express the kinds of work the agent can perform;
- `credentials` prove the identity.

An agent can have multiple roles:

```text
name: codex-backend
roles: [backend, database]
```

Roles remain simple and explicit and are defined by each project or team according to its own division of work. Kairos does not require capability scoring or automatic matching for roles.

## 2. Token

A Token authenticates an Agent Identity:

```text
Token
  ↓ authenticate
Agent Identity
  ↓ resolve
name + roles
```

An agent calling Kairos carries only the Token and does not repeatedly declare its name and roles. A Token can be rotated or revoked without changing the Agent Identity or its collaboration history.

Tokens belong in the agent execution environment, not in WorkItems, Tasks, or project documentation.

## 3. Authenticated Mode

In Authenticated Mode, Kairos manages Agent Identities and issues Tokens:

```text
Agent supplies Token
        ↓
Kairos authenticates identity
        ↓
Use configured name and roles
```

An agent cannot temporarily expand its roles through a request. Task discovery and claiming both use the identity information granted in Kairos.

This mode is suited to shared environments requiring explicit identity attribution and access control.

## 4. Trusted Mode

Trusted Mode is suited to local development, trusted networks, and other environments where identity is already guaranteed by the runtime:

```text
name: local-codex
roles: [backend]
```

The agent declares its name and roles without a Token. Kairos trusts the declaration and uses it for Task discovery and collaboration records.

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

Workflow therefore uses roles to define the agent’s legal work space.

## 6. Roles and Blackboard

In Blackboard, roles primarily help agents discover relevant work:

```text
Agent roles: [backend]
Task tags: [backend, auth]
```

Kairos can derive default tags or query scope from agent roles, after which the agent chooses using the Task description and current context.

Blackboard tags describe work categories and discovery hints; they do not automatically become access permissions. A Task that needs restriction should explicitly configure its allowed roles.

Both Workflow Definition and Blackboard Definition can provide Suggested Tags such as `module:*`. Agents add concrete tags to Tasks from the actual work. A Definition supplies only the recommended vocabulary and does not require people to maintain every Task label continuously.

Blackboard therefore uses roles for discovery by default while allowing explicit constraints when needed.

## 7. AGENTS.md

`AGENTS.md` describes the work rules an agent must follow in a repository or directory. It solves a different problem from Agent Identity:

```text
Agent Identity → who the agent is and which roles it has
AGENTS.md       → how work should be performed in this project
```

AGENTS.md belongs in the project repository:

- it changes with the code version;
- it can inherit and override rules by directory;
- it corresponds to the current checkout or worktree;
- the Agent Harness reads it during execution.

Kairos can provide repository and working-directory information on a Task so an agent can locate the applicable AGENTS.md. The platform does not need to copy or replace repository rules.

Kairos can host a lightweight Agent Profile with fields such as name, roles, and description, while project execution rules remain in the repository.

> Agent identity establishes who is acting; roles define which work the agent may participate in.
