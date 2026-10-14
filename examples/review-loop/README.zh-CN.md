# Writer/Checker 交付与修订循环

[English](README.md)

两个 Agent Identity 在独立的 Authenticated Core 上分工：

```text
起草 → 检查 ── 需修订 → 起草
         └── 就绪 → 发布 → Human Review
```

Writer 上传托管 `guide`，Checker 读取本次直接输入，在托管 JSON `review` 中保留准确的 `guide_artifact_id`，再选择合法的修订或发布路径。发布者创建当前 Claim 拥有的新 guide，不能把上游 Artifact ID 直接提交为自己的成果。Human 可以驳回修订或批准完成。

## 真实模型运行

需要 Linux/macOS、curl、jq、openssl、Perl/POSIX、已构建的 Core/Daemon，以及专用的 Codex CLI 0.146.0+ 登录目录。模型必须明确选择；此命令会调用真实模型并消耗额度。

```sh
KAIROS_DAEMON_CODEX_HOME=/absolute/path/dedicated-codex-auth \
KAIROS_DAEMON_MODEL=your-model make review-loop-example
```

默认绑定 `127.0.0.1:18083`，用 `KAIROS_REVIEW_LOOP_ADDR` 修改。Launcher 清空继承的 PostgreSQL DSN，创建独立私有数据目录，打印 WorkItem URL 和 `operator.token` 文件路径，不打印 Token。用该 Human Token 在控制台普通登录框登录；勿分享目录或把凭据写入 Artifact。

通过 `KAIROS_REVIEW_LOOP_WORK_ITEM=/absolute/path/work-item.json` 指定包含获准仓库参考路径和证据的真实目标。默认目标引用公开的 Kairos 文档，没有提供恢复证明，不代表已经执行部署或恢复。参考资料应显式列出；线上变更不属于这个示例。

两个 Daemon 分别绑定 `writer`、`checker` Role，各一个 slot。基础设施失败按候选代次最多一次 Harness/Dispatch，重启角色前检查私有 outcome/log；Probe 通过不代表模型配额或任务适配。Ctrl-C 先停止两个 Daemon，再停止 Core，私有数据保留供检查。

解压发布包后可直接执行 `sh examples/review-loop/run.sh`，使用相同环境变量，二进制位于归档根目录。

## 人工介入与审核

- Checker 返回 `human_intervention_required` 后，Task Failed，Workflow 可能仍 Open。检查原因，在控制台选择**继续执行**并补充决定或缺失证据；替代实例保留原输入提交。
- Checker 的 `revise` 使循环生成新的 Draft 实例。Review 及其准确引用的 guide 都保留在历史中。
- Human Review 驳回后，同一个发布 Task 重新开放。新 Claim 带着反馈执行并提交新 Artifact，旧提交仍可检查。
- 批准发布后 Workflow 完成。成果以 Task Submission/Artifact 为准，Workflow 的 `work_item.result` 仍为空。

选择组、恢复、凭据授权及暂存/输入/历史语义见 [API 参考](../../docs/api-reference.zh-CN.md)。进程清理边界由 [Adapter 指南](../../internal/daemon/codexadapter/README.md)定义：Daemon 崩溃后 Core 只回收 lease，不清理孤儿 Harness；替代不确定执行前须确认停止或完成清理。

## 不调用模型的可复现验证

```sh
make build
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
make daemon-e2e
```

Smoke 只创建 fixture。二进制 E2E 使用脚本 Harness、真实 HTTP/MCP、两个 Daemon、受限上传和 SQLite，明确覆盖：

1. Checker 请求操作员介入后，由 Human Continue 恢复。
2. 两轮同名 guide 与指向准确输入 ID 的 review。
3. 发布审核驳回、新 Claim 修订和反馈保留。
4. Checker Daemon 崩溃、Claim 到期、fixture 操作员清理已知孤儿，以及新执行接手。

确定性 fixture 故意触发这些分支；真实模型依据实际发现决定是否修订。测试不会把 reaper 回收 Claim 描述为停止了进程。

可另外运行停服 SQLite/托管 store 恢复演练：

```sh
python3 examples/review-loop/verify-sqlite-restore.py \
  --server /absolute/path/kairos-server --output /private/restore-evidence.json
```

脚本创建自己的 loopback Trusted fixture，停服后复制整个数据库目录与匹配 store，恢复新副本，并核对 `quick_check`、WorkItem 状态、Artifact ID 和下载字节。不会操作现有数据库，也不证明 PostgreSQL、在线备份、认证恢复、部署平台或跨版本兼容性。
