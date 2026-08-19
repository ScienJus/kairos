---
name: kairos-agent
description: Execute work coordinated by Kairos through its MCP tools. Use when asked to find available Kairos work, plan an empty Blackboard, claim or release a Task, read Task or WorkItem execution context, submit a result, or report a Task failure. Do not use for Definition or Identity administration.
---

# Kairos Agent

Use the Kairos MCP server as the durable coordination layer. Perform the actual task with the normal tools available in the current environment, then record the outcome in Kairos.

## Execution loop

1. Call `find_work` with a reasonable limit. In Workflow mode, role and graph eligibility determine candidates; tags are descriptive metadata and do not filter executable Workflow Tasks. In Blackboard mode, tags may narrow discovery.
2. Select one eligible candidate at a time unless the user explicitly requests parallel execution.
3. If the candidate kind is `empty_blackboard`, read its WorkItem goal, context, constraints, acceptance criteria, and Definition instructions. Call `create_blackboard_task` to add a concrete executable Task, then call `find_work` again.
   If no Task is needed because the goal is already satisfied, call `complete_blackboard` with the durable result instead.
4. For a Task candidate, call `get_task_context` before claiming. Read the complete Task, WorkItem, Definition instructions, previous failures, upstream Workflow results, and current Blackboard state.
5. Call `claim_task` immediately before beginning execution. Do not work on a Task whose Claim was not acquired successfully.
6. Perform the requested work outside Kairos using the available repository, browser, API, or other tools. Keep the Claim ID.
   While working, run a background heartbeat loop. Call `heartbeat_claim` before the reported `lease_until` (normally around one third of the remaining lease), and always renew before starting a long-running shell command. Pass `lease_seconds` when the expected next interval changes; the default Agent lease is five minutes. Use a fresh `operation_id` for each new heartbeat request, and reuse it only for an identical retry. Stop work immediately if heartbeat reports an expired or conflicting Claim.
7. Finish with exactly one lifecycle action:
   - Call `submit_task` with a durable result when acceptance criteria are met.
   - Call `fail_task` with `reopen` and a useful retry prompt when another attempt can succeed.
   - Call `fail_task` with `fail_work_item` only when the whole WorkItem cannot continue.
   - Call `release_claim` when stopping without a result or failure decision.
8. When the final WorkItem status or aggregated result matters, call `get_work_item_context` after the lifecycle action. This query works for both open and terminal WorkItems.

## Mutation discipline

- Supply a new stable `operation_id` for every logical mutation.
- Reuse an `operation_id` only when retrying the exact same tool call with identical arguments.
- If arguments change, generate a different `operation_id`.
- Treat Claim conflicts as normal concurrency: stop working on that Task and call `find_work` again.
- Never place Bearer tokens or trusted identity headers in tool arguments. MCP transport authentication supplies the actor identity.

## Workflow submissions

For a non-terminal Workflow Task, use the choice groups returned by `get_task_context`. Pass exactly one legal `transition.choice_group_id` to `submit_task` and only skip IDs listed as skippable. Do not invent runtime Workflow edges. The current Workflow context includes controlled summaries of upstream Tasks and their durable results; use those summaries instead of opening arbitrary upstream Task contexts. Direct `get_task_context` calls remain restricted by the target Task's role and active Claim.

For Blackboard Tasks, omit `transition`. Use `create_blackboard_task` only for open Blackboard WorkItems returned by Kairos or already present in Task context.

Use `decompose_blackboard_task` when a claimed Task must become an aggregate of concrete children. While that aggregate remains open, use `add_blackboard_child_task` for newly discovered work. Use `add_blackboard_relation` for suggested ordering, and `skip_blackboard_task` with a durable reason when an unclaimed pending Task has lost value.

After `decompose_blackboard_task` succeeds, the aggregate parent Task's Claim is automatically closed and the parent moves to `waiting_children`. Do not heartbeat, submit, fail, or release that parent Claim afterward. Claim and execute the returned child Tasks as needed; the parent completes through child aggregation.

In Blackboard task context, `task` is the current Task and `blackboard.tasks` intentionally excludes it. Use `blackboard.current_task_id` to correlate the two views.

## Boundaries

The MCP surface is execution-only. Do not attempt to create or modify Definitions, manage identities or tokens, or make human review decisions through this skill. Use the Kairos HTTP management API for those operations when the user explicitly requests them.

If the Kairos tools are unavailable, do not replace this protocol with ad-hoc HTTP calls. The repository configures the local server in `.codex/config.toml`; start Kairos, set the Trusted or Authenticated identity environment variables, and restart the Codex task so project MCP configuration is loaded.
