---
title: Human Review in Multi-Agent Workflows | Kairos
description: Add durable human review, feedback, retry context, and artifact delivery to multi-agent workflows with Kairos.
---

# Human Review in Multi-Agent Workflows

Autonomous execution is useful until a result needs a judgment that belongs to a person. Kairos makes Review part of the Task lifecycle, so approval, rejection, feedback, and retry context survive the agent session that produced them.

## Request review with a result

An executor claims a Task and submits a result. When the Task's Review policy requires it, or the executor asks for Review, the Task moves to `InReview` and the Claim ends. The agent does not need to stay alive while a person decides.

```text
Working → submit for Review → InReview
                              ├── approve → Completed
                              └── reject  → Pending → Claim again
```

The operations console exposes pending human attention. A reviewer can inspect the Task, prior submissions, expected Artifacts, and the current work context before approving or rejecting.

## Rejection is actionable context

Every Review round is retained. When an agent retries a rejected Task, Kairos provides earlier submissions, Review feedback, and any retry prompt as shared context. The next executor can correct the result without reconstructing the previous conversation.

## Keep review focused

Use concise Task results and attach larger evidence as Artifacts. Define acceptance criteria that a reviewer can verify, and require a Review policy only where human judgment changes the outcome. Other Tasks in a Blackboard can continue while one Task waits for Review.

Kairos currently provides the underlying Review and attention flows in the operations console; a complete WorkItem event timeline remains planned. See the [Human Interaction Model]({{ '/whitepapers/06-human-interaction-model.html' | relative_url }}) and [API Reference]({{ '/api-reference.html' | relative_url }}) for the current behavior.
