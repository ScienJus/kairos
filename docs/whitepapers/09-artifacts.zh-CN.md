# Artifact 模型与存储

## 目的

Result 解释执行者完成了什么，Artifact 标识执行者实际交付了什么。Git commit、分支、文档、报告、归档包和托管文件在执行会话结束后仍可寻址，并能被同一 WorkItem 的所有 Task 使用。

## 交付契约

Workflow Task Definition 可以声明具名 Artifact：

```json
{
  "artifacts": [
    { "name": "commit", "description": "提交包含实现和测试的不可变 Git commit。" },
    { "name": "branch", "description": "提交包含该 commit 的远程集成分支。" }
  ]
}
```

`name` 同时是稳定契约标识和展示名称，`description` 用于指导执行者。Kairos 不在契约中定义媒体类型、文件种类、数量范围或 Store 策略。Workflow 声明的每个名称都必须提交一次，同时允许额外交付物。Blackboard 依赖 Task 提示词，不使用结构化 Artifact 契约。

## 生命周期

执行者只有在持有 Active Claim 时才能创建 Artifact。外部 Artifact 保存绝对 URI；托管 Artifact 把内容上传到配置好的 Store。两者在 `submit_task` 携带其 ID 前都处于暂存状态。提交操作在一个事务中校验归属和 Workflow 要求、创建不可变 Submission、绑定 Artifact 并结束 Claim。

暂存 Artifact 只对创建它的 Claim 可用。提交后 Artifact 对整个 WorkItem 可见；被驳回 Submission 下的 Artifact 也作为历史保留。上下文返回 Artifact Manifest，不直接注入文件内容。

## 存储

Artifact 记录来源链路、名称和 URI；托管 Blob 元数据记录 URI、Digest 和大小。内置 Store 以流式方式写入内容寻址文件，并生成 `kairos://blobs/sha256/...` URI；相同内容复用同一个物理 Blob。

Agent 不能选择 Store。服务端只向配置好的默认 Store 写入。读取器根据 URI Scheme 注册，因此迁移期间可以把新 Artifact 写入新 Store，同时继续解析旧 Scheme。首个内置实现支持 `kairos://`，后续可以实现 `s3://` 等 Store。
