---
name: kairos-agent
description: Execute work coordinated by Kairos through its MCP tools. Use when asked to find available Kairos work, plan an empty Blackboard, claim or release a Task, read Task execution context, submit a result, or report a Task failure. Do not use for Definition or Identity administration.
---

# Kairos Agent

Use the Kairos MCP server as the durable coordination layer. Perform the actual task with the normal tools available in the current environment, then record the outcome in Kairos.

## Execution loop

1. Call `find_work` with relevant tags and a reasonable limit.
2. Select one eligible candidate at a time unless the user explicitly requests parallel execution.
3. If the candidate kind is `empty_blackboard`, read its WorkItem goal, context, constraints, acceptance criteria, and Definition instructions. Call `create_blackboard_task` to add a concrete executable Task, then call `find_work` again.
4. For a Task candidate, call `get_task_context` before claiming. Read the complete Task, WorkItem, Definition instructions, previous failures, upstream Workflow results, and current Blackboard state.
5. Call `claim_task` immediately before beginning execution. Do not work on a Task whose Claim was not acquired successfully.
6. Perform the requested work outside Kairos using the available repository, browser, API, or other tools. Keep the Claim ID.
7. Finish with exactly one lifecycle action:
   - Call `submit_task` with a durable result when acceptance criteria are met.
   - Call `fail_task` with `reopen` and a useful retry prompt when another attempt can succeed.
   - Call `fail_task` with `fail_work_item` only when the whole WorkItem cannot continue.
   - Call `release_claim` when stopping without a result or failure decision.

## Mutation discipline

- Supply a new stable `operation_id` for every logical mutation.
- Reuse an `operation_id` only when retrying the exact same tool call with identical arguments.
- If arguments change, generate a different `operation_id`.
- Treat Claim conflicts as normal concurrency: stop working on that Task and call `find_work` again.
- Never place Bearer tokens or trusted identity headers in tool arguments. MCP transport authentication supplies the actor identity.

## Workflow submissions

For a non-terminal Workflow Task, use the choice groups returned by `get_task_context`. Pass exactly one legal `transition.choice_group_id` to `submit_task` and only skip IDs listed as skippable. Do not invent runtime Workflow edges.

For Blackboard Tasks, omit `transition`. Use `create_blackboard_task` only for open Blackboard WorkItems returned by Kairos or already present in Task context.

## Boundaries

The MCP surface is execution-only. Do not attempt to create or modify Definitions, manage identities or tokens, or make human review decisions through this skill. Use the Kairos HTTP management API for those operations when the user explicitly requests them.
