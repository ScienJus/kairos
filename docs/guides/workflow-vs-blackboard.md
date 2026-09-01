---
title: Workflow DAG vs Blackboard Planning for AI Agents | Kairos
description: Compare Kairos Workflow and Blackboard coordination modes for AI agent teams and choose between fixed dependencies and evolving plans.
type: article
---

# Workflow DAG vs Blackboard Planning for AI Agents

Kairos has two coordination modes because agent work is not always planned the same way. Both modes share durable Tasks, Claims, submissions, Reviews, and Artifacts; they differ in where the next legal work comes from.

## Choose Workflow when the path is known

Workflow definitions describe a graph before execution begins. Use Workflow for release checklists, staged research, CI-style pipelines, and other processes where dependencies and required steps should be enforced.

Workflow supports parallel branches, joins, role constraints, optional decisions, Review policies, and bounded cycles. A downstream Task becomes eligible only when its prerequisites and progression rules allow it.

## Choose Blackboard when the plan must evolve

Blackboard starts with a WorkItem objective and can begin with no Tasks. People and agents create, split, relate, skip, and extend Tasks as they learn more. Relations provide shared guidance without turning every suggestion into a blocking dependency.

Blackboard is useful for open-ended investigation, incident response, product discovery, and work where the decomposition is part of the execution.

## A practical decision

| Question | Workflow | Blackboard |
| --- | --- | --- |
| Are dependencies known before work starts? | Yes | Not necessarily |
| Can collaborators create new Tasks during execution? | Through configured progression | Yes, continuously |
| Does a relation block eligibility? | It can define progression | It is a suggestion |
| How does completion happen? | Selected paths close structurally | A collaborator submits an explicit completion result |

The modes use the same MCP execution loop, so an agent can work with either after learning the shared Claim and heartbeat contract. See the <a href="{{ '/whitepapers/05-blackboard.html' | relative_url }}">Blackboard whitepaper</a> and <a href="{{ '/whitepapers/04-workflow.html' | relative_url }}">Workflow whitepaper</a> for detailed semantics.
