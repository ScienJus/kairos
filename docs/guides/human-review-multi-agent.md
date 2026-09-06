---
title: Add Human Review to Multi-Agent Work | Kairos
description: Preserve approval, rejection, feedback, evidence, and retry context as part of the Task lifecycle across agent sessions.
type: article
---

# Add human Review to multi-agent work

Agents can work autonomously, but some results still need a person to decide whether they are good enough. That decision may happen long after the agent session ends. Kairos keeps the result, Review decision, and feedback with the Task so the original agent—or a new one—can continue later.

## Submit the result, then let the agent exit

The executor claims a Task and submits its result. If the Task requires Review, or the executor asks for it, the Task moves to `InReview` and the Claim ends. The agent is free to exit while a person inspects the work.

```text
Working → submit for Review → InReview
                              ├── approve → Completed
                              └── reject  → Pending → Claim again
```

The operations console lists work waiting for a person. Before approving or rejecting it, the reviewer can inspect the Task, earlier submissions, expected Artifacts, and the context in which the result was produced.

## Turn rejection into the next attempt's brief

Kairos keeps every Review round. When the Task is claimed again, the next executor receives the earlier submission, the reviewer's feedback, and any retry instructions. It can fix the result without reconstructing a lost conversation.

## Ask for Review where judgment matters

Keep the submitted result concise and attach larger evidence as Artifacts. Write acceptance criteria that a reviewer can verify. Require Review only where a person's judgment can change the outcome; other Blackboard Tasks can continue while one Task waits.

The operations console supports the current Review and human-attention flows. A complete WorkItem event timeline remains planned. See the <a href="{{ '/whitepapers/06-human-interaction-model.html' | relative_url }}">Human Interaction Model</a> for the current interface and the <a href="{{ '/api-reference.html' | relative_url }}">API Reference</a> for integration details.
