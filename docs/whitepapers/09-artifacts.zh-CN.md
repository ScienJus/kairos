# Kairos Artifact 模型

“实现已经完成”这样的结果有价值，但它并不是实现本身。审核者和后续执行者还需要一种持久方式，找到真正产出的 Commit、文档、报告、归档包或上传文件。

Kairos 把这种具体交付物称为 Artifact。Submission 解释结果，Artifact 则让结果在原会话消失后仍然可以被检查。

## 声明预期交付物

Workflow Task Definition 可以声明具名 Artifact 要求。名称是稳定契约标识，描述用于说明应交付什么。成功 Submission 必须为每个声明名称提交一次 Artifact，也可以附加额外交付物。

Blackboard Task 通过描述说明预期交付物，不使用结构化 Artifact 契约。

Kairos 不在 Task Definition 中编码媒体类型、文件格式、数量范围或存储策略；这些内容属于工作说明或部署配置。

## 从创建到提交

执行者只有在持有 Active Task Claim 时才能创建 Artifact，可以：

- 登记位于其他系统中的绝对 URI；或
- 把小体积内容上传到当前部署的托管 Artifact Store。

Artifact 在 `submit_task` 携带其 ID 前只暂存于创建它的 Claim。提交会原子校验 Claim 归属和 Workflow 要求、创建不可变 Submission、绑定 Artifact，并结束执行责任。

## 后续协作者能看到什么

暂存 Artifact 只在创建它的 Claim 内可见。提交后 Artifact 对整个 WorkItem 可见；即使 Review 驳回结果，它仍保留在对应 Submission 历史中。

Context 只返回 Artifact 元数据，不直接注入文件内容。托管内容通过独立的认证端点下载。

重试不会覆盖已提交 Artifact。新的执行尝试会创建新的 Artifact 和 Submission，保留之前的证据。

如果一份报告被 Review 驳回，原文件仍留在对应 Submission 下。下一位执行者上传修订版并单独提交，审核者可以比较两次尝试，而不是只看到一份历史已经消失的可变文件。

## Artifact 存储与保留

部署方拥有一个托管 Store，并决定上传、保留和垃圾回收策略。托管 Artifact 使用稳定的 `kairos://` URI，并保存 Digest 与大小。大体积交付物应放在持久外部存储中，再登记绝对 URI。

Active Claim 会保护其暂存 Artifact。Claim 结束后，旧的未提交 Artifact 和未完成上传记录可以被回收；已提交 Artifact 作为工作历史保留。

精确上传方式、上限、重放语义、权限和默认值见 [API 参考](../api-reference.zh-CN.md)与 [OpenAPI](../openapi.yaml)。

## Artifact 不变量

- Artifact 只属于一个 WorkItem，并来源于一个 Task Claim。
- 其他 Claim 不能提交当前 Claim 的暂存内容。
- Submission 不可变地绑定 Artifact，Review 不会删除它们。
- Artifact 内容不会复制进普通 Context 或 Result 字段。
- 存储清理不得改变已接受的业务历史。
