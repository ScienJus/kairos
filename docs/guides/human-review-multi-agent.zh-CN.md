---
title: 多 Agent Workflow 中的人工 Review | Kairos
description: 在多 Agent Workflow 中加入持久的人工 Review、反馈、重试上下文和 Artifact 交付。
lang: zh-CN
type: article
---

# 多 Agent Workflow 中的人工 Review

自动执行很有用，但有些结果需要由人做出判断。Kairos 将 Review 纳入 Task 生命周期，让批准、拒绝、反馈和重试上下文在产生结果的 Agent 会话结束后仍然保留。

## 带结果请求 Review

执行者领取 Task 并提交结果。当 Task 的 Review policy 要求 Review，或执行者主动请求 Review 时，Task 会进入 `InReview`，Claim 也随之结束。审核者做决定时，Agent 不需要继续在线。

```text
Working → submit for Review → InReview
                              ├── approve → Completed
                              └── reject  → Pending → Claim again
```

操作控制台会展示待处理的人工事项。审核者可以在批准或拒绝前检查 Task、历史提交、预期 Artifact 以及当前工作上下文。

## 拒绝会形成可执行的上下文

每一轮 Review 都会被保留。当 Agent 重试一个被拒绝的 Task 时，Kairos 会将历史提交、Review 反馈和重试提示作为共享上下文提供给它。下一个执行者无需重新拼接之前的对话，就能修正结果。

## 让 Review 保持聚焦

使用简洁的 Task 结果，并将较大的证据作为 Artifact 附加。定义审核者可以验证的验收标准，只在人工判断会改变结果时要求 Review policy。Blackboard 中的其他 Task 可以在某个 Task 等待 Review 时继续推进。

Kairos 当前已在操作控制台提供 Review 和待处理事项流程，但完整的 WorkItem 事件时间线仍在计划中。当前行为请参阅<a href="{{ '/whitepapers/06-human-interaction-model.zh-CN.html' | relative_url }}">人机交互模型</a>和 <a href="{{ '/api-reference.zh-CN.html' | relative_url }}">API 参考</a>。
