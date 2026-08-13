# Kairos

Kairos 是一个面向人类与 Agent 协作的任务管理层。

它提供统一的工作模型、任务发现与责任协作接口，让不同执行者能够发现、领取、推进和完成工作。Kairos 不控制 Agent runtime、模型、沙箱或生命周期，这些能力由外部 Agent Harness 或 Bridge 负责。

## 协作模式

Kairos 在统一的 `WorkItem` 与 `Task` 模型上支持两种独立的组织方式：

- **Workflow**：由预先定义的任务图和流程规则确定当前可执行的 Task。
- **Blackboard**：由执行者围绕目标动态规划 Task，任务关系用于提供协作上下文和建议。

两种模式共享执行、Claim、进展记录、身份和人类交互模型，但保留各自的任务发现与推进语义。

面向人类时，WorkItem 可以通过 List 或 Kanban 统一浏览；进入详情后，Workflow 渲染为流程图，Blackboard 渲染为动态 Checklist。

## 白皮书

1. [核心工作模型](docs/whitepapers/01-core-work-model.md)
2. [执行协作模型](docs/whitepapers/02-execution-collaboration-model.md)
3. [Coordination Semantics](docs/whitepapers/03-coordination-semantics.md)
4. [Workflow](docs/whitepapers/04-workflow.md)
5. [Blackboard](docs/whitepapers/05-blackboard.md)
6. [Human Interaction Model](docs/whitepapers/06-human-interaction-model.md)
7. [Agent Interaction Model](docs/whitepapers/07-agent-interaction-model.md)
8. [Agent Identity Model](docs/whitepapers/08-agent-identity-model.md)

## 项目状态

Kairos 目前处于设计阶段，本仓库用于沉淀核心概念与后续实现基础。
