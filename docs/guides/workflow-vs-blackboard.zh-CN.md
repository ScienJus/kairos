---
title: 选择 Workflow 还是 Blackboard | Kairos
description: 步骤已知时使用 Workflow；需要人和 Agent 边做边调整计划时使用 Blackboard。
lang: zh-CN
type: article
---

# 使用固定流程，还是让计划边做边变

有些工作遵循明确的步骤，有些工作只有开始调查后才知道下一步。Kairos 同时支持这两种情况：Workflow 执行预先定义的路径，Blackboard 则允许人和 Agent 随着新发现不断调整计划。

## 步骤已知时使用 Workflow

如果重要步骤和依赖在开始前已经确定，就先定义 Workflow。它适合发布检查清单、分阶段研究、CI 流水线，以及任何不能随意跳过步骤的流程。

流程图可以分支、并行执行、汇合结果、限制角色、等待 Review，也可以在限定次数内循环。只有配置的条件满足后，Kairos 才会开放下游 Task。

## 探索本身就是工作时使用 Blackboard

Blackboard 可以只从一个目标开始，甚至不预先列出任何 Task。随着了解加深，人和 Agent 可以创建 Task、拆分大任务、连接相关工作、跳过死路，并增加新的方向。这些关系为团队提供参考，但不会把每个建议都变成硬性依赖。

它适合开放式调查、事故响应、产品探索，以及任何需要在执行过程中决定“接下来做什么”的工作。

## 先问一个问题

工作开始前，能否说清主要步骤？可以，就先用 Workflow；如果计划必须随着证据和新发现变化，就用 Blackboard。

| 问题 | Workflow | Blackboard |
| --- | --- | --- |
| 工作开始前是否已知依赖？ | 是 | 不一定 |
| 协作者能否在执行期间创建新 Task？ | 通过已配置的推进规则 | 需要时随时创建 |
| 关系会阻塞可执行性吗？ | 可以定义推进规则 | 只是建议 |
| 如何完成？ | 选定路径以结构化方式关闭 | 协作者提交明确的完成结果 |

在两种模式下，Agent 领取、执行和提交 Task 的方式完全相同；真正不同的是下一项工作如何开放。完整规则请参阅 <a href="{{ '/whitepapers/05-blackboard.zh-CN.html' | relative_url }}">Blackboard 模型</a>和 <a href="{{ '/whitepapers/04-workflow.zh-CN.html' | relative_url }}">Workflow 模型</a>。
