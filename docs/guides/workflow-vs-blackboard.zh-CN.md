---
title: 面向 AI Agent 的 Workflow DAG 与 Blackboard 规划 | Kairos
description: 比较 Kairos 的 Workflow 和 Blackboard 两种协调模式，根据固定依赖或持续演化的计划选择合适的方式。
lang: zh-CN
type: article
---

# 面向 AI Agent 的 Workflow DAG 与 Blackboard 规划

Kairos 提供两种协调模式，因为 Agent 工作并不总是按照同一种方式规划。两种模式都共享持久 Task、Claim、Submission、Review 和 Artifact；区别在于下一项合法工作从哪里产生。

## 路径已知时选择 Workflow

Workflow definition 在执行开始前描述一张图。它适合发布检查清单、分阶段研究、类似 CI 的流水线，以及需要强制执行依赖和步骤的其他流程。

Workflow 支持并行分支、汇合、角色限制、可选决策、Review policy 和有界循环。只有当前置条件和推进规则允许时，下游 Task 才会变为可执行。

## 计划需要演化时选择 Blackboard

Blackboard 从一个 WorkItem 目标开始，也可以不预先创建任何 Task。人和 Agent 会随着了解更多信息而创建、拆分、关联、跳过和扩展 Task。关系提供共享指导，但不会把每个建议都变成阻塞依赖。

Blackboard 适合开放式调查、事故响应、产品发现，以及分解本身就是执行过程一部分的工作。

## 一个实用判断

| 问题 | Workflow | Blackboard |
| --- | --- | --- |
| 工作开始前是否已知依赖？ | 是 | 不一定 |
| 协作者能否在执行期间创建新 Task？ | 通过已配置的推进规则 | 可以持续创建 |
| 关系会阻塞可执行性吗？ | 可以定义推进规则 | 只是建议 |
| 如何完成？ | 选定路径以结构化方式关闭 | 协作者提交明确的完成结果 |

两种模式使用同一套 MCP 执行循环；掌握共享的 Claim 和 heartbeat 契约后，Agent 就可以使用任意一种模式。详细语义请参阅 <a href="{{ '/whitepapers/05-blackboard.zh-CN.html' | relative_url }}">Blackboard 白皮书</a>和 <a href="{{ '/whitepapers/04-workflow.zh-CN.html' | relative_url }}">Workflow 白皮书</a>。
