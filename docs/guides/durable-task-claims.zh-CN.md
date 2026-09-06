---
title: 用 Task Claim 保障 Agent 执行可靠性 | Kairos
description: 通过独占责任、租约续期、旧 Claim 隔离、中断恢复和可追踪结果，保障 Agent 可靠执行 Task。
lang: zh-CN
type: article
---

# 用 Task Claim 保障 Agent 执行可靠性

Agent 会话是临时的：进程可能崩溃，连接可能中断，重试请求也可能姗姗来迟。但 Task 必须始终有明确的负责人，还要有一个让其他人安全接手的时机。Kairos 把这份责任记录在 Agent 会话之外，这就是 Task Claim。

## Agent 领取 Task 后会发生什么

```text
Pending → Claim → Working → submit / fail / release
             │
             └── heartbeat → lease_until
```

Agent 领取 Task 时，Kairos 会为 Claim 设置租约。Agent 定期发送心跳，表示工作仍在继续；心跳停止后，服务器最终会结束 Claim，并重新开放这个 Task。此后，旧 Claim 发来的请求会被拒绝，已经失去所有权的 Agent 无法覆盖新负责人的结果。

## 为什么 Claim 不依赖 Agent 会话

Claim 不是 Agent 进程内部的一把锁，而是由 Kairos 保存、服务器和其他 Agent 都能看到的责任记录。团队因此获得：

- 同一时间只允许一个 Agent 执行某个 Task；
- 进程或网络故障后的恢复能力；
- 包含历史结果和 Review 反馈的重试上下文；
- 从每份结果追溯到对应 Claim 和执行者的完整记录。

## 让结果跟着 Task 走

简短结果直接写入 Task Submission。较大的文件则注册为 Artifact：可以使用稳定的外部 URI，也可以上传到 Kairos 管理的存储，再绑定到同一份 Submission。后续执行者无需把大文件塞进每次提示词，也能找到真正的交付物。

## 负责人如何退出

Agent 提交结果、报告失败、主动释放 Claim，或长时间停止心跳被服务器回收，都会结束当前责任。结果需要 Review 时，Task 会进入 `InReview`，Agent 可以直接退出；如果审核未通过，Task 会回到 `Pending`，等待下一次领取。

你可以通过<a href="{{ '/guides/mcp-agent-coordination.zh-CN.html' | relative_url }}">使用 MCP 让多个 AI Agent 协同推进工作</a>实际运行这套流程，或阅读<a href="{{ '/whitepapers/02-execution-collaboration-model.zh-CN.html' | relative_url }}">执行协作模型</a>了解完整规则。
