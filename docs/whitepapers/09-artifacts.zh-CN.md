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

执行者只有在持有 Active Claim 时才能创建 Artifact。外部 Artifact 保存绝对 URI；托管 Artifact 通过 HTTP multipart 或 MCP `upload_artifact` Base64 传输把内容上传到配置好的 Store。向 Store 写入托管内容前，Kairos 会先按 operation key 把稳定上传 URI 和 pending 状态持久化到数据库。Store 流式写入并返回 Digest 和大小；Kairos 随后把这些值更新到 pending 记录，再通过一个数据库事务登记 Blob 元数据、创建暂存 Artifact，并把上传更新为 completed。Pending 重试会覆盖该 URI，并校验已记录的 Digest 和大小。Store 写入失败时，pending 状态仍能定位文件并由 GC 清理。外部和托管 Artifact 在 `submit_task` 携带其 ID 前都处于暂存状态。提交操作在一个事务中校验归属和 Workflow 要求、创建不可变 Submission、绑定 Artifact 并结束 Claim。

暂存 Artifact 只对创建它的 Claim 可用。提交后 Artifact 对整个 WorkItem 可见；被驳回 Submission 下的 Artifact 也作为历史保留。上下文返回 Artifact Manifest，不直接注入文件内容。

Active Claim 会保护其全部暂存 Artifact，不受创建时间影响。Claim 结束后，未提交 Artifact 在自身创建时间超过配置的保留期时进入后台 GC；如果结束 Claim 时 Artifact 已超过保留期，它可能在下一轮立即被回收。已提交 Artifact 不参与这种生命周期回收。托管 Blob 只有在其 URI 不再被任何 Artifact 引用时才会删除；过期 pending 上传按登记的 URI 清理。

Pending 托管上传记录，以及外部登记与托管上传的 completed 重放记录，都使用同一保留时间。窗口内 completed 记录可以在响应丢失后重放暂存 Artifact 结果；窗口过期后，后续请求以当前 Artifact 与 Claim 状态为准。

## 存储

Artifact 记录来源链路、名称和 URI；托管 Blob 元数据记录 URI、Digest 和大小。内置 Store 在数据库先登记稳定上传 URI 后再写入文件，并在数据库把上传标记为 completed 前同步文件及其目录链。Digest 只作为完整性元数据，不参与 URI 生成，相同内容可以占用不同的托管位置。

Agent 不能选择 Store。服务端在本次部署中只使用一个配置好的托管 Store。内置实现支持 `kairos://`；大文件应使用 `create_artifact` 登记其持久化外部 URI。

部署方可以配置 HTTP 与 MCP 共用的 Artifact 上传上限、暂存 Artifact 保留时间和 GC 周期；内置默认值依次为 16 MiB、24 小时和 15 分钟。内置托管上传有意只面向小文件；大文件应放在 S3 等持久外部存储中，并登记为外部 Artifact URI。MCP Base64 传输会增加约三分之一内容体积，并在内存中完整缓冲请求。
