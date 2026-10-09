# Kairos Agent Identity Model

Identity and responsibility are easy to conflate. Knowing which actor sent a request does not say which piece of work it currently owns, and holding a Claim should not require giving one short-lived Harness a long-lived credential.

Kairos separates the questions. Identity says who is acting, a Claim says what that actor is responsible for, and a Claim-bound Executor Credential limits one concrete Harness to that execution.

## Actor and Identity

```text
Identity
├── actor_id   stable identifier
├── kind       human | agent
└── role       one Agent role; empty for Humans
```

Actor ID records the source of an action; it grants no permission by itself. Human permissions come from the operation's rules. Agents must also pass the Task's `executor` and `allowed_roles` checks.

Actor IDs are stable historical references. They must contain a non-whitespace character and cannot be `.` or `..`; transport-specific preservation, trimming, and URL encoding rules belong to the [API Reference](../api-reference.md#identity-administration-and-console).

Identity is not an Agent Harness. Several sessions or Daemon instances may use one Identity; Claims still provide exclusive execution responsibility.

Two Daemon processes using the same Agent Identity may therefore discover the same candidate. They are still the same actor for audit purposes, but only the process that successfully creates the Claim owns the Task. Identity explains attribution; it does not serialize execution.

## Two Ways to Prove Identity

### Trusted Mode

Use Trusted Mode for local development or a network where the runtime already guarantees identity. Requests directly supply actor ID, kind, and role, and Kairos trusts those values.

### Authenticated Mode

Use Authenticated Mode for one trusted collaboration group. Kairos persists Identities, resolves Actor and Role from Bearer Tokens, and supports issuance, rotation, and revocation. Request parameters cannot override the credential's identity.

Business rules are identical in both modes; only identity proof differs. Authenticated Mode does not provide tenant, team, project, or object-level isolation. Mutually untrusted groups require separate deployments.

## Role Narrows Eligibility

An Agent Identity has one role. Workflow and Blackboard use exact role matching for Agent eligibility. Humans ignore `allowed_roles` but still must satisfy `executor` kind and operation permissions.

Role determines candidate visibility and Claim eligibility, not responsibility. A Claim still prevents two same-role agents from executing one Task simultaneously.

Blackboard tags provide discovery context and never replace role authorization. Workflow candidates come from graph and role state, not tags.

## Executor Credential: Access for One Execution

Agent Daemon never gives a long-lived Identity Token to a concrete Harness. When creating a Task or Coordination Claim, it generates a one-use Executor Token:

- the Token binds Claim, Actor, and permission profile;
- Core stores only its hash and returns plaintext once in the Claim response;
- reads and writes remain inside the bound WorkItem and allowed operations;
- it fails immediately when the Claim ends, the WorkItem is terminal, or scope does not match;
- later Identity Token rotation does not rewrite the already-issued Token's Claim lifecycle.

An Executor Credential is an execution scope, not another Identity. It carries no role and cannot discover new work. Tokens belong in protected runtime configuration, never in WorkItems, Tasks, or project documentation.

## Identity Does Not Replace Workspace Guidance

`AGENTS.md` defines working rules for a repository or directory; Agent Identity defines the Actor making Kairos operations:

```text
Identity  → who executes
AGENTS.md → how to execute in this workspace
```

Role should not encode code style, test commands, or directory rules; those belong to project guidance.

## The Deployment Admin Token

In Authenticated Mode, the Admin Token maps to one stable, database-bound ordinary Human actor. Its business permissions follow Human rules and it gains no Agent discovery, role, or Executor privilege. Only the configured Admin credential can administer Identities; an ordinary Human Identity cannot.

Rotation preserves the actor but requires restarting every server instance. The console may present this actor as `system admin` without changing its ID. See the [API Reference](../api-reference.md#admin-token-business-identity) for configuration, persistence, session, Identity management, and compatibility details.

## Identity Invariants

- Identity proof and execution responsibility remain separate; Identity never replaces a Claim.
- The server resolves the Actor from authentication rather than self-reported request parameters.
- Role constrains Agent eligibility only; explicit business rules govern Humans.
- An Executor Credential never exceeds its Claim scope.
- Token separation in Authenticated Mode is not multi-tenant data isolation.
