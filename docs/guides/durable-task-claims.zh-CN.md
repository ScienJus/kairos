---
title: 面向 AI Agent 团队的持久 Task Claim | Kairos
description: 了解 Kairos 如何通过 Task Claim、租约 heartbeat、fencing、重试和持久结果，让多 Agent 执行更加可靠。
lang: zh-CN
type: article
---

# 面向 AI Agent 团队的持久 Task Claim

模型会话可能消失、重试，或失去网络连接。协调系统仍然需要知道工作由谁负责，以及其他参与者何时可以安全接手。Kairos 用持久化的 Task Claim 表示执行责任。

## Claim 生命周期

```text
Pending → Claim → Working → submit / fail / release
             │
             └── heartbeat → lease_until
```

Agent 发现 Task 后领取它，并获得一段租约。Agent 工作期间通过 heartbeat 续租；如果租约过期，服务器的回收器结束 Claim，Task 就会重新可用。旧 Claim ID 会被 fencing，因此延迟到达的请求无法在所有权转移后继续修改 Task。

## 为什么需要租约

Claim 不是由 Agent 进程持有的锁，而是其他 Agent 可以观察、服务器可以恢复的持久协调状态。它为团队提供：

- 同一时间只允许一个 Agent 执行某个 Task；
- 进程或网络故障后的恢复能力；
- 包含历史结果和 Review 反馈的明确重试上下文；
- 将结果、负责的 Claim 和执行者持久关联起来。

## 让交付物持久可追踪

简短结果应放在 Task Submission 中。较大的文件应注册为 Artifact，使用外部持久 URI 或 Kairos 的托管上传路径，再将 Artifact 绑定到 Submission。这样既能控制执行上下文大小，又不会丢失实际交付物。

## Claim 何时结束

提交结果、记录失败、释放 Claim 或由服务器回收，都会结束当前的执行责任。请求 Review 会让 Task 进入 `InReview`，但不要求 Agent 持续在线；如果被拒绝，Task 会回到 `Pending`，等待新的 Claim。

阅读<a href="{{ '/guides/mcp-agent-coordination.zh-CN.html' | relative_url }}">使用 MCP 协调多个 AI Agent</a>了解可运行示例，并阅读<a href="{{ '/whitepapers/02-execution-collaboration-model.zh-CN.html' | relative_url }}">执行协作模型</a>了解领域语义。
