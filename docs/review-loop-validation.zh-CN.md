# Writer/Checker 修订循环验收记录

[English](review-loop-validation.md)

本记录说明 `examples/review-loop` 的实际验收范围；运行方式见[示例指南](../examples/review-loop/README.zh-CN.md)，部署操作见[部署与恢复指南](guides/deployment-recovery.zh-CN.md)。不把脚本 fixture、模型的自述或操作员模拟混称为真人审核。

## 真实模型试跑

- 日期：2026-10-14；平台：Darwin；Codex CLI：0.159.2；模型：`gpt-6.1-sol`。
- Core 基线：`faf6967`（PR #70 已合并），加上本次新的示例与恢复演练脚本。
- 独立 Authenticated Core/SQLite，两个不同 Agent Identity 分别绑定 `writer`、`checker`，各一个 slot；Codex 使用专用登录目录和 Claim-bound Executor Credential。
- 目标：根据获准的只读仓库参考和实测恢复证据，交付中文部署与备份恢复指南。没有操作线上服务、读取 `.env`、运行桌面浏览器或打包模型凭据。

实际推进：

1. Writer 上传初稿 `guide`。Checker 读取该精确输入，在 `review` 内记录它的 Artifact ID，给出 `revise`：备份代码的 `copytree` 默认跟随链接，与操作说明的安全边界不一致。
2. 新 Draft 实例使用那份 review 指定的旧 guide 修订。第二次 Checker 给出 `ready`，明确引用新 guide；没有把历史中的同名初稿当成当前输入。
3. 发布者在自己的 Claim 下重新上传最终 guide，进入 Human Review。验收操作员根据源码驳回：停机应明确先停止并等待全部 Daemon、确认已知 Harness 和外部操作，再停止 Core；不能把不存在的控制台“暂停调度”当成入口。
4. 同一个发布 Task 通过新 Claim 获得驳回反馈，提交修订后的新 Artifact。验收操作员核对修订后批准，WorkItem 完成。

这里的 Human Role 决定由本次验收控制脚本在隔离服务中执行，不代表用户本人在控制台审核。两个模型角色没有 Human Review 权限。

最终记录：5 个 Task 实例、6 次 Claim、6 个已提交 Artifact；Writer 4 次 Harness、Checker 2 次 Harness；Checker verdict 为 `revise → ready`；发布 Task 两次 Submission 和 `rejected → approved` Review；没有 Active Claim，也没有业务失败。Workflow 的 `work_item.result` 保持空字符串，交付物位于 Task Submission/Artifact。

最终原始 guide 字节 SHA-256：`f31e03be36d623dc70a5a24b6f1728adf782edd08b28b3994fab198bcbf92eba`。仓库中文指南取其已批准操作章节，移除本地运行专属 ID 追踪节并补充导航；英文版是编辑后的对应操作说明，不宣称与原始 Artifact 字节相同。

## 确定性二进制回归

`make daemon-e2e` 使用脚本 Harness 和真实 Core/Daemon HTTP/MCP、SQLite及受限上传，不调用模型。新增案例故意覆盖：

- Checker `human_intervention_required`，断言具体 `await_human` 原因，再由 Human Continue 创建替代实例；其 review 仍引用原输入 guide ID。
- 两轮同名 guide、精确 review 引用及最终发布新 Claim 的上传归属。
- Human Review 驳回后，同一个 Task 新 Claim 接手，保存两次提交、旧反馈和新成果。
- Checker Daemon 被强杀，Claim 到期而未伪造业务失败；fixture 操作员确认并清理已知孤儿 Harness 后，新 Checker Identity 接手，再完成恢复与交付。

真实模型运行没有自然触发失败或崩溃。这些故障语义由上述确定性案例验证，不冒充真人或模型自然发生的故障。Core reaper 只回收 Claim，测试不宣称它停止了进程。

## 独立恢复与脚本复验

`verify-sqlite-restore.py` 创建临时 loopback Trusted 实例，提交一个托管 Artifact 并完成 WorkItem，干净停止 Core 后复制完整数据库目录和匹配 store，再用同版本二进制启动独立恢复副本。

结果：SQLite `quick_check=ok`、WorkItem 状态/result 保留、Artifact ID 保留、下载字节完全一致；内容 SHA-256：`f4c971f17be63da606673faba8291f31e01796eafe765b2840365e3a2e9056b6`。

另外独立执行指南中的复制代码：普通数据库/WAL/SHM/store 文件集合通过；符号链接、FIFO 在创建副本前被拒绝。这是操作员复验，不仅是 Checker 的自述。

## 仍未覆盖

- Linux 上同一真实模型/CLI组合及其他 Provider；
- PostgreSQL、在线备份、跨版本升级、生产认证恢复；
- systemd/Kubernetes 的进程清理保证、外部 URI 和 Provider 登录/workspace 的恢复；
- 真人使用控制台进行审核的用户研究，以及真实数据规模下的恢复时间目标。

这些边界不影响本次限定范围的交付；后续验证应单独记录平台、CLI、模型、数据和实际操作。
