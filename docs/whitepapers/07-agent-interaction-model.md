# Kairos Agent Interaction Model

An agent session is temporary, but the responsibility it accepts must be unambiguous and recoverable. Kairos gives both proactive agents and Daemon-started Harnesses the same loop: find work, understand it, Claim it, keep the Claim alive, and end with an explicit outcome.

Nothing in that loop requires the original session to survive. The Claim and the resulting work records carry continuity forward.

## The Execution Loop

```text
discover / receive
        ↓
inspect context
        ↓
claim responsibility
        ↓
execute + heartbeat
        ↓
submit / fail / release
```

Each lifecycle write uses the current Claim, correct version, and required idempotency inputs. After a conflict, the agent rereads authoritative state instead of continuing from local assumptions.

## Discovery and Coordination Candidates

`find_work` uses coordination mode, executor kind, Agent role, and query context to return candidates. A candidate is not an assignment; the Agent must select and Claim it.

Task candidates Claim a Task directly. These Blackboard situations instead use a WorkItem Coordination Claim:

- `empty_blackboard`: create the first Task or submit completion;
- `blackboard_completion`: create follow-up work or submit completion;
- `work_item_acceptance`: accept completion or create work that reopens execution.

The Agent Claims before reading full coordination context so multiple agents cannot make conflicting decisions while no concrete Task exists.

## Execution Context for the Work at Hand

Agent context contains only what the current work requires:

- Definition and WorkItem intent;
- the current Task, acceptance requirements, and historical feedback;
- related upstream results and Artifacts;
- decisions and planning capabilities legal in the current mode.

Workflow Context exposes configured paths, optional work, continue/exit choices, and Review policy. Blackboard Context exposes the current Task Graph, advisory Relations, and appendable planning space. Reading another Task's full context remains constrained by role and active Claim.

## Claims, Lease Renewal, and Scoped Credentials

Agent Claims and Coordination Claims are renewable leases. The Agent heartbeats before the returned `lease_until`; the old Claim loses authority only after the Core reaper commits recovery.

Agent Daemon gives a concrete Harness a Claim-bound Executor Credential. It can read required context in the bound WorkItem and perform profile-allowed Artifact or nonterminal Blackboard writes. The Daemon translates typed outcomes into terminal Core operations.

## Submit, Fail, or Release: Ending Responsibility Deliberately

- **submit** creates a Submission, attaches staged Artifacts, and commits decisions legal in the current mode;
- **fail** records a Failure and explicitly requests retry, Human intervention, or WorkItem failure;
- **release** gives up responsibility without inventing a result, reopening the Task or coordination candidate.

On `work_item_cancelled`, fencing loss, owner mismatch, or authoritative credential failure, the Agent stops renewal and later writes. A timeout is not proof that an operation failed: idempotent resource creation reuses its `operation_id`, and uncertain terminal writes are reconciled against history.

## Mode Capabilities: What an Agent May Decide

| Capability | Workflow | Blackboard |
| --- | --- | --- |
| Select candidates | Yes | Yes |
| Change work structure | Submit configured decisions only | Create, decompose, append, relate, or skip Tasks |
| Request Review | According to Definition policy | May request it from current results |
| Decide WorkItem completion | Workflow structure converges | Explicitly submit a completion result |

## Proactive Agents and Agent Daemon

A proactive agent calls MCP/HTTP itself. Agent Daemon binds one Agent identity and automatically discovers, Claims, and starts a configured Harness. Both use the same Core protocol. See the [Agent Daemon whitepaper](agent-daemon.md) for scheduling, Adapter, and crash boundaries.

## MCP Integration Surface

Kairos exposes the execution loop through stateless Streamable HTTP MCP and provides Harness discipline in `.agents/skills/kairos-agent`. MCP covers Agent execution: discovery, context, Claims, Artifacts, submission, failure, and Blackboard planning. Definition and Identity administration and Human Review decisions remain outside the Agent surface.

See the [API Reference](../api-reference.md) and [OpenAPI](../openapi.yaml) for exact tools, parameters, and errors.
