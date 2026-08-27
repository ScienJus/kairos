---
name: kairos-agent
description: Execute work coordinated by Kairos through its MCP tools. Use when asked to find available Kairos work, plan an empty Blackboard, claim or release a Task, create Artifacts, read Task or WorkItem execution context, submit a result, or report a Task failure. Do not use for Definition or Identity administration.
---

# Kairos Agent

Use the Kairos MCP server as the durable coordination layer. Perform the actual task with the normal tools available in the current environment, then record the outcome in Kairos.

## Execution loop

1. Call `find_work` with a reasonable per-kind limit. Candidates are returned in lifecycle-decision, executable Task, then empty-Blackboard order; the limit applies independently to `work_item_acceptance`, `blackboard_completion`, `task`, and `empty_blackboard`. Omission or zero uses five candidates per kind. In Workflow mode, role and graph eligibility determine candidates; tags are descriptive metadata and do not filter executable Workflow Tasks. In Blackboard mode, tags may narrow discovery.
2. Select one eligible candidate at a time unless the user explicitly requests parallel execution.
3. Handle Blackboard lifecycle candidates explicitly:
   - For `empty_blackboard`, read the WorkItem goal, context, constraints, acceptance criteria, and Definition instructions. Create a concrete Task when work is needed; if the goal already requires no execution, call `submit_blackboard_completion` with the durable result.
   - For `blackboard_completion`, inspect the converged WorkItem. Create another Task when more work is needed; otherwise call `submit_blackboard_completion`. Task convergence alone never completes a Blackboard or starts acceptance.
   - For `work_item_acceptance`, review the submitted completion result. Create another Task if the proposal should not be accepted; otherwise call `accept_blackboard_completion`.
   Call `find_work` again after every lifecycle decision.
4. For a Task candidate, call `get_task_context` before claiming. Read the complete Task, WorkItem, Definition instructions, previous failures, upstream Workflow results, and current Blackboard state.
5. Call `claim_task` immediately before beginning execution. Do not work on a Task whose Claim was not acquired successfully.
6. Perform the requested work outside Kairos using the available repository, browser, API, or other tools. Keep the Claim ID.
   While working, run a background heartbeat loop. Call `heartbeat_claim` before the reported `lease_until` (normally around one third of the remaining lease), and always renew before starting a long-running shell command. Pass `lease_seconds` when the expected next interval changes; the default Agent lease is five minutes. Reaching `lease_until` makes the Claim eligible for the background reaper but does not itself revoke ownership, so a heartbeat may still renew it before reaping. Stop work immediately if heartbeat reports that the Claim is no longer active or owned by this agent, or that the WorkItem is terminal.
7. Read `expected_artifacts` from Task context. For each required deliverable, create an external Artifact with `create_artifact` or Base64-encode the file bytes and call `upload_artifact` for a small managed file. The Agent never selects an Artifact Store. Base64 expands content and is not a large-file transport; publish large deliverables to durable storage such as S3 and register their absolute URI with `create_artifact`.
8. If the Claim remains active and Kairos has not reported a terminal WorkItem, finish with exactly one lifecycle action:
   - Call `submit_task` with a durable result and every staged Artifact ID in `artifact_ids` when acceptance criteria are met. Workflow submissions must include every declared Artifact name; Blackboard submissions may include any useful Artifacts.
   - Call `fail_task` with `reopen` and a useful retry prompt when another attempt can succeed.
   - Call `fail_task` with `fail_work_item` only when the whole WorkItem cannot continue.
   - Call `release_claim` when stopping without a result or failure decision.
9. When the final WorkItem status or Blackboard completion result matters, call `get_work_item_context` after the lifecycle action. This query works for open, acceptance-pending, and terminal WorkItems. Workflow WorkItems keep their result empty; read their durable outcomes from Task Submissions and Artifacts. After the last Task ends, call `find_work` again and handle the resulting Blackboard completion candidate.

## Mutation discipline

- Supply a stable `operation_id` to `claim_task`, `create_artifact`, `upload_artifact`, `create_blackboard_task`, `decompose_blackboard_task`, and `add_blackboard_child_task`.
- Reuse that ID only when retrying the exact same resource-creating call with identical arguments; changed arguments require a new ID.
- Lifecycle tools have no `operation_id`. If their response is lost, read current context before deciding whether another transition is still valid.
- Treat any response reporting that the WorkItem is `completed`, `failed`, or `cancelled` as terminal. Stop work and heartbeat immediately; do not call `submit_task`, `fail_task`, `release_claim`, create an Artifact, or perform any other mutation for that Task.
- If a conflict does not make the WorkItem status clear, call `get_work_item_context` before another mutation. Stop if the WorkItem is terminal; only treat the conflict as recoverable concurrency when it remains open.
- Never place Bearer tokens or trusted identity headers in tool arguments. MCP transport authentication supplies the actor identity.

## Workflow submissions

For a non-terminal Workflow Task, use the choice groups returned by `get_task_context`. Read optional target `relation_guidance` when interpreting the already-legal choices; guidance never creates a branch that is absent from the choice groups. Pass exactly one legal `transition.choice_group_id` to `submit_task` and only skip IDs listed as skippable. Do not invent runtime Workflow edges. The current Workflow context includes controlled summaries of upstream Tasks and their durable results; use those summaries instead of opening arbitrary upstream Task contexts. Direct `get_task_context` calls remain restricted by the target Task's role and active Claim.

For Blackboard Tasks, omit `transition`. Use `create_blackboard_task` only for open or Agent-acceptance Blackboard WorkItems returned by Kairos or already present in Task context. `submit_blackboard_completion` declares that the goal is achieved; `accept_blackboard_completion` is a separate acceptance action.

Use `decompose_blackboard_task` when a claimed Task must become an aggregate of concrete children. While that aggregate remains open, use `add_blackboard_child_task` for newly discovered work. Use `add_blackboard_relation` for suggested ordering, and `skip_blackboard_task` with a durable reason when an unclaimed pending Task has lost value.

After `decompose_blackboard_task` succeeds, the aggregate parent Task's Claim is automatically closed and the parent moves to `waiting_children`. Do not heartbeat, submit, fail, or release that parent Claim afterward. Claim and execute the returned child Tasks as needed; the parent completes through child aggregation.

In Blackboard task context, `task` is the current Task and `blackboard.tasks` intentionally excludes it. Use `blackboard.current_task_id` to correlate the two views.

## Boundaries

The MCP surface is execution-only. Do not attempt to create or modify Definitions, manage identities or tokens, or make human review decisions through this skill. Use the Kairos HTTP management API for those operations when the user explicitly requests them.

If the Kairos tools are unavailable, do not replace this protocol with ad-hoc HTTP calls. The repository configures the local server in `.codex/config.toml`; start Kairos, set the Trusted or Authenticated identity environment variables, and restart the Codex task so project MCP configuration is loaded.
