# Kairos Roadmap

[简体中文](ROADMAP.zh-CN.md)

Kairos is under active development. This roadmap communicates direction rather than delivery dates. Current implementation status remains documented in the [README](README.md#project-status).

## Current foundation

- Workflow and Blackboard coordination semantics;
- durable Claims, submissions, Reviews, failures, and execution context;
- HTTP and MCP execution surfaces;
- SQLite and PostgreSQL persistence;
- Trusted and Authenticated identity modes;
- managed and external Artifacts;
- an operations console for WorkItems, attention, Blackboard Task hierarchies, Workflow graphs, and Definition editing.

## Near-term priorities

1. **Reliable releases**: reproducible binaries and container images, checksums, migration guidance, and upgrade verification.
2. **Bridge integration**: dispatch eligible Tasks to external agent harnesses while keeping Kairos independent of model and sandbox management.
3. **Operational workflows**: complete the remaining console actions, improve failure recovery visibility, and add practical backup and restore guidance.
4. **Integration examples**: document real multi-agent workflows and provide reusable Workflow and Blackboard templates.
5. **Observability**: expose useful structured logs and runtime metrics without making telemetry a coordination dependency.

## Later exploration

- richer workspace organization and operational views for complete WorkItems;
- richer Artifact Store adapters;
- deployment profiles for shared teams;
- compatibility tooling for Definition and API evolution.

## Non-goals

Kairos does not aim to choose models, run agent sandboxes, replace an agent harness, or introduce a third coordination mode through an operational view. New work should preserve the boundary between durable coordination and executor runtime management.

Before `v1.0.0`, APIs and schemas may change as these boundaries are validated. Breaking changes must be called out in release notes and accompanied by migration guidance where persisted data is affected.
