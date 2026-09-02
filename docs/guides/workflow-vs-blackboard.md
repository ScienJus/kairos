---
title: Choose Between Workflow and Blackboard | Kairos
description: Use Workflow when the path is known and Blackboard when people and agents need to shape the plan while they work.
type: article
---

# Use a fixed workflow—or let the plan evolve

Some work follows a known sequence. Other work only becomes clear after the team starts investigating. Kairos supports both: Workflow enforces a predefined path, while Blackboard lets people and agents reshape the plan as they learn.

## Use Workflow when the steps are known

Define a Workflow before execution when the team already knows the required steps and their dependencies. It fits release checklists, staged research, CI-style pipelines, and any process where skipping ahead would be unsafe.

The graph can branch, run Tasks in parallel, join their results, restrict roles, pause for Review, and repeat a bounded section. Kairos only opens a downstream Task when the configured rules allow it.

## Use Blackboard when discovery is part of the work

Blackboard can start with an objective and no Task list at all. As the team learns, people and agents create Tasks, split large ones, connect related work, skip dead ends, and add new directions. Those relations guide the team without turning every suggestion into a hard dependency.

Use it for open-ended investigation, incident response, product discovery, or any effort where deciding what to do next is part of the job.

## Choose by asking one question

Can you describe the important steps before work begins? If yes, start with Workflow. If the plan must change as evidence arrives, start with Blackboard.

| Question | Workflow | Blackboard |
| --- | --- | --- |
| Are dependencies known before work starts? | Yes | Not necessarily |
| Can collaborators create new Tasks during execution? | Through configured progression | Yes, whenever needed |
| Does a relation block eligibility? | It can define progression | It is a suggestion |
| How does completion happen? | Selected paths close structurally | A collaborator submits an explicit completion result |

Agents claim, execute, and submit Tasks the same way in both modes. The difference is how the next Task becomes available. See the <a href="{{ '/whitepapers/05-blackboard.html' | relative_url }}">Blackboard model</a> and <a href="{{ '/whitepapers/04-workflow.html' | relative_url }}">Workflow model</a> for the complete rules.
