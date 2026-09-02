---
title: 在多 Agent 协作中加入人工 Review | Kairos
description: 把批准、拒绝、反馈、证据和重试上下文纳入 Task 生命周期，并跨 Agent 会话保留。
lang: zh-CN
type: article
---

# 在多 Agent 协作中加入人工 Review

Agent 可以自主完成很多工作，但有些结果仍需要人来判断是否合格，而这个决定可能发生在 Agent 会话结束很久之后。Kairos 会把结果、Review 决定和修改意见一起留在 Task 上，让原来的 Agent 或新的 Agent 都能继续处理。

## 提交结果后，Agent 就可以退出

执行者领取 Task 并提交结果。如果 Task 要求 Review，或执行者主动发起 Review，Task 会进入 `InReview`，Claim 也随之结束。人在检查结果期间，Agent 无需保持在线。

```text
Working → submit for Review → InReview
                              ├── approve → Completed
                              └── reject  → Pending → Claim again
```

操作控制台会列出正在等待人工处理的工作。审核者可以在批准或拒绝前查看 Task、历史提交、预期 Artifact，以及产生这份结果时使用的上下文。

## 把拒绝意见变成下一次执行的任务说明

Kairos 会保留每一轮 Review。Task 再次被领取时，下一个执行者会拿到之前的提交、审核意见和重试要求，无需重新拼接已经丢失的对话，就能直接修改结果。

## 只在真正需要判断的地方安排 Review

Task 结果应保持简洁，较大的证据则作为 Artifact 附加。验收标准要让审核者能够直接验证。只有人的判断会影响结果时才要求 Review；某个 Task 等待审核期间，Blackboard 中的其他工作仍可继续。

操作控制台支持当前的 Review 和人工关注流程，完整的 WorkItem 事件时间线仍在规划中。当前界面请参阅<a href="{{ '/whitepapers/06-human-interaction-model.zh-CN.html' | relative_url }}">人机交互模型</a>，接入细节请参阅 <a href="{{ '/api-reference.zh-CN.html' | relative_url }}">API 参考</a>。
