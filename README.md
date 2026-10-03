# Kairos: Durable Coordination for Human and AI Agent Teams

<p align="center">
  <img src="docs/assets/kairos-logo-wordmark.png" alt="Kairos" width="520">
</p>

English | [简体中文](README.zh-CN.md) | [Documentation](https://scienjus.github.io/kairos/)

[![CI](https://github.com/ScienJus/kairos/actions/workflows/ci.yml/badge.svg)](https://github.com/ScienJus/kairos/actions/workflows/ci.yml)
[![Security](https://github.com/ScienJus/kairos/actions/workflows/security.yml/badge.svg)](https://github.com/ScienJus/kairos/actions/workflows/security.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Kairos is an open-source coordination server for work that outlives any one human or AI agent session. Codex, Claude Code, other MCP clients, and Human collaborators see the same objectives, responsibilities, Reviews, and deliverables, so a handoff does not depend on reconstructing an old conversation.

It deliberately stops at coordination. Kairos does not choose models, provide sandboxes, or replace an Agent Harness. Agents may participate directly through MCP and Skills, or Agent Daemon may discover work and start a configured Harness. In either case, Core remains the durable source of truth.

<p align="center">
  <img src="docs/assets/kairos-workflow.jpg" alt="Kairos Workflow showing two parallel Tasks joining into a release plan" width="900">
</p>

## Quickstart

Start a local Workflow with two parallel Tasks and one join:

```bash
make quickstart
```

Follow the [quickstart guide](examples/quickstart/README.md) to connect several Codex sessions and see Claims prevent duplicate execution while upstream results flow into the join Task.

## How Work Moves

```text
WorkItem objective
  ↓
candidate Task → exclusive Claim → execute + heartbeat
  ↓                              ↓
next work ← Submission / Review / Failure / Artifact
```

A team advances a **WorkItem** by completing its **Tasks**. Each Task is one coherent delivery owned by one executor at a time. A **Claim** makes that responsibility explicit; for Agents, leases, heartbeat, reaping, and fencing make interruption recoverable. Submissions, Reviews, Failures, and Artifacts stay with the work instead of disappearing with the session that produced them.

## Coordination Modes

| | Workflow | Blackboard |
| --- | --- | --- |
| Use when | Main steps and dependencies are known | The objective is known but the path must evolve with evidence |
| Graph authority | A Definition constrains legal progression | A Task Graph shares guidance |
| Runtime planning | Decide only at configured optional, Review, and loop points | Create, decompose, append, relate, and skip Tasks |
| Completion | Complete when the selected path converges | Submit an explicit completion result, then apply acceptance policy |

Both modes share discovery, Claim, submission, Review, failure, and Artifact protocols. See [Workflow](docs/whitepapers/04-workflow.md) and [Blackboard](docs/whitepapers/05-blackboard.md) for detailed rules.

## One Model for People and Agents

The console provides WorkItem overview, Human attention, Workflow graph, Blackboard hierarchy, Task Detail, Definition editing, and Daemon observation. Humans can execute Tasks, review results, recover failed Workflows, and cancel WorkItems.

Agents use stateless Streamable HTTP MCP and `.agents/skills/kairos-agent` for the discover → Claim → heartbeat → submit loop. Agent Daemon can automate the same protocol and gives each concrete Harness a Claim-bound Executor Credential.

## Current Status

Implemented:

- Workflow and Blackboard semantics with SQLite and PostgreSQL persistence;
- Trusted and Authenticated Modes, Identity Tokens, Admin Human, and Executor Credentials;
- HTTP, MCP, idempotent resource creation, and managed/external-URI Artifacts;
- Human console with Identity Token management, Workflow recovery, Blackboard acceptance, and WorkItem cancellation;
- Agent Daemon continuous scheduling, local Codex Adapter, instance/Dispatch/event observation, and isolated E2E examples.

Current work focuses on more Provider/platform validation, hardened deployment, and broader operational views. See the [Roadmap](ROADMAP.md).

## Run

Development requires Go 1.26.6+. Building the console also requires Node.js 22.22.2+ (22.x), 24.15.0+ (24.x), or 26+, plus npm.

```bash
make build
./bin/kairos-server
```

The default uses SQLite and Trusted Mode. See the [API Reference](docs/api-reference.md) for PostgreSQL, Authenticated Mode, Admin Token, reverse proxy, Artifacts, and complete routes.

For managed execution, see the [Daemon example](examples/daemon/README.md). `make build` builds Core and Daemon; `make daemon-e2e` verifies real binaries with a scripted Harness and no model calls.

In Authenticated Mode, the configured Admin signs in through the ordinary login form and manages Human and Agent Identity Tokens from the account menu. Exact authorization, Actor ID, one-time Token, and compatibility rules live in the [API Reference](docs/api-reference.md).

## Documentation Map

| Document | Responsibility |
| --- | --- |
| README / [Roadmap](ROADMAP.md) | Current capabilities / future direction |
| [Whitepapers](docs/whitepapers/01-core-work-model.md) | Stable domain concepts, coordination semantics, and system boundaries |
| [API Reference](docs/api-reference.md) / [OpenAPI](docs/openapi.yaml) | Cross-interface behavior / exact HTTP contract |
| Detailed designs and decision records | Implementation tradeoffs, current status, and historical context |
| Package READMEs and examples | Component operation, verification, and failure boundaries |

When two documents touch the same subject, the narrower owner wins: OpenAPI for exact HTTP shape, the API Reference for cross-interface behavior, whitepapers for meaning, README for current status, and Roadmap for future direction.

Suggested whitepaper order:

1. [Core Work Model](docs/whitepapers/01-core-work-model.md)
2. [Execution Collaboration](docs/whitepapers/02-execution-collaboration-model.md) and [Coordination Semantics](docs/whitepapers/03-coordination-semantics.md)
3. [Workflow](docs/whitepapers/04-workflow.md) or [Blackboard](docs/whitepapers/05-blackboard.md)
4. [Human](docs/whitepapers/06-human-interaction-model.md), [Agent](docs/whitepapers/07-agent-interaction-model.md), [Identity](docs/whitepapers/08-agent-identity-model.md), and [Artifact](docs/whitepapers/09-artifacts.md)
5. [Agent Daemon](docs/whitepapers/agent-daemon.md)

Implementation records include the [Daemon acceptance record](docs/agent-daemon-implementation-plan.zh-CN.md), [Daemon decision summary](docs/whitepapers/agent-daemon-design-decisions.zh-CN.md), [observability design](docs/daemon-observability-design.zh-CN.md), [Task Detail architecture](docs/task-detail-architecture.zh-CN.md), [page design baseline](docs/page-design-baseline.zh-CN.md), and [frontend handbook](docs/frontend-development-handbook.zh-CN.md). These records are currently Chinese-only.

## Community

Read the [contribution guide](CONTRIBUTING.md) before contributing. Maintainers can follow the [release guide](docs/releasing.md). Report security issues privately according to the [security policy](SECURITY.md).

Kairos is licensed under the [Apache License 2.0](LICENSE).
