# Agent Daemon 平台可观测性设计

> 状态：MVP 已实现。精确 HTTP Schema 以 [OpenAPI](openapi.yaml) 为准。

本能力用于观察 Daemon 运行，不参与 Task 调度、Claim fencing 或业务终态判定。MVP 包含实例注册、快照和事件上报、Human 读取 API、控制台与保留期清理。

## 1. 对象模型

```text
Agent Identity（稳定 Actor）
  └── Daemon Instance（每次进程启动生成）
        └── Dispatch（本次候选处理）
              └── Task Claim 或 Coordination Claim（Core 责任）
```

- Identity 不标记在线/离线；同一 Identity 也可由普通 MCP Agent 使用。
- Instance ID 是每次启动的随机标识，不表示机器，也不是 secret。
- 可选 instance name 只用于展示，不授权、去重或合并记录。
- Dispatch ID 不复用 Claim operation ID、Executor Token 或 Adapter RunRef。

## 2. 独立状态维度

| 维度 | 值 | 含义 |
| --- | --- | --- |
| 连接 | `reporting` / `stale` / `stopped` | 45 秒内收到上报、上报超时、明确退出 |
| 调度健康 | `unknown` / `healthy` / `paused` | Probe 未完成、可接单、暂停新 Claim |
| 工作量 | `active_count` / `slots` | 占用 slot 的 Dispatch 数与容量 |

Core 使用服务端接收时间计算连接状态。`reporting` 只表示最近有联系，`stale` 只表示上报中断，两者都不证明进程、Claim 或 Harness 的实际状态。

Claim heartbeat 与 Daemon 上报完全独立。失联不会释放 Claim，恢复上报也不会复活 Claim。

## 3. 数据流与上报

```text
Scheduler / Dispatch
  ├── Core 业务 API：发现、Claim、heartbeat、outcome
  └── Reporter：快照 + 有类型事件
                     ↓
                  Core 遥测存储 → Human API → Daemon 页
```

Reporter 使用同一 Core URL 和 Agent Identity Token，但拥有独立超时、退避和非阻塞事件队列。上报失败不改变 Dispatch 返回值或生命周期。

每次报告包含：

- 单调递增 `revision`；
- instance lifecycle、health、`active_count` 和有限的 Active Dispatch 详情；
- 累计丢弃事件数和未附带详情的 Active 数量；
- 按 instance `sequence` 递增的关键事件。

空集合为 `[]`，不存在的单值为 `null`。`claim_status=uncertain` 明确区分“领取响应未知”与“尚未尝试 Claim”。

## 4. 传输与幂等

- Daemon 约每 15 秒上报，单次超时 3 秒，失败退避最高 60 秒并带 jitter。
- 单批最多 50 个事件、100 个 Active Dispatch 详情，请求体最多 64 KiB。超限时保留总数并减少详情，不截断 JSON。
- 只有更大 revision 才覆盖快照并刷新 `last_report_at`；旧 revision 可补交新事件。
- 事件以 `(instance_id, sequence)` 去重，响应丢失后可安全重送。
- 队列溢出只丢弃最早的遥测事件并增加 `dropped_events`，不影响业务操作。

退出时先报告 `stopping`，停止活跃 Dispatch 后在有界窗口内尽力发送 `stopped`。最终上报不承诺排空内存事件；失败时实例最终进入 `stale`。

## 5. 事件边界

只记录意义明确的边缘，不为每次发现、Observe 或 Claim heartbeat 建事件。事件包括：

- admission 暂停/恢复；
- Claim 确认取得；
- Harness 启动/重试；
- outcome 已应用；
- Dispatch 结束；
- candidate quarantine；
- 停机开始/实例已停止。

`occurred_at` 来自 Daemon 时钟，Core 另存 `received_at`。跨实例不根据客户端时间推导严格因果。事件 details 只接受固定短字段和枚举，不接受任意日志、模型输出或 Artifact 内容。

## 6. API、存储与保留

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/daemon-instances` | Agent 注册实例；相同配置可幂等重放 |
| `POST /api/v1/daemon-instances/{id}/reports` | 事务写入快照与事件 |
| `GET /api/v1/daemon-instances` | Human 分页查看实例摘要 |
| `GET /api/v1/daemon-instances/{id}` | Human 查看配置与最后快照 |
| `GET /api/v1/daemon-instances/{id}/events` | Human 按 sequence 倒序查看事件 |

`daemon_instances` 保存可查询的身份、接收时间和状态列，并将已校验快照作为整体 JSON 保存。`daemon_events` 以 `(instance_id, sequence)` 为主键，保存事件类型、时间和用于导航的对象引用。

事件保留 30 天；实例停止上报 90 天后可连同剩余事件清理。清理是限量、尽力而为的后台工作，不参与 WorkItem/Claim 事务。

## 7. 授权与隐私

- 只允许普通 Agent Identity 注册和上报自己的实例；Human、Admin Human 和 Executor Credential 不能代报。
- 只允许 Human 读取 Daemon 观测。
- WorkItem、Task 和 Claim 引用只用于导航，上报事务不以此判定业务真相。
- 不上报 Token、请求体、模型输出、Artifact 内容、Provider 原始异常、本地路径、环境变量、主机名或 RunRef。

实例 ID 是区分运行记录的随机标识，不提供比 Agent Identity Token 更强的进程认证。

## 8. 控制台

Daemon 列表区分当前/近期实例和已停止历史。详情页展示：

1. 连接新鲜度、最后上报和调度健康；
2. Active Dispatch 的候选类型、阶段、Claim 确定性和业务对象链接；
3. 事件时间线与丢弃事件缺口提示；
4. 按需展开的实例 ID、版本、Adapter 和 slots。

只在 Daemon 页可见时每 15 秒轮询。过期快照明确标记为“上次报告”，请求失败保留最后成功读取时间。界面不提供停止、重启或清空 quarantine 的远程控制。

## 9. 故障语义

| 场景 | 平台表现 | 业务行为 |
| --- | --- | --- |
| 空闲但正常运行 | `reporting + healthy + 0/slots` | 无需 Claim 也可显示在线 |
| Probe 失败但旧 Dispatch 仍运行 | `reporting + paused + active>0` | 只暂停新 Claim |
| 遥测端点不可达 | 最后报告过期后 `stale` | 调度和 Claim heartbeat 继续 |
| Claim 结果未知 | `claim_status=uncertain` | 保留 slot 和 operation ID，不重领 |
| 提交响应丢失 | finalizing / 待核对 | 以 Core 历史判定 outcome |
| Daemon 崩溃 | 45 秒后 `stale`，事件可能有缺口 | Core reaper 回收 Claim；不承诺终止遗留 Harness |
| 上报乱序/响应丢失 | 旧 revision 不覆盖新快照，事件去重 | 不改变业务记录 |

## 10. 当前边界

MVP 不流式上传日志或模型思考过程，不提供秒级进度，不推断机器存活，也不从遥测反向建立 Claim 归属。

尚未实现的增强包括 `outcome_ready` 事件、跨实例聚合图表和可配置遥测参数。如需强保证 Claim 与 Daemon 实例归因，必须在 Claim 模型中增加显式、可查询的实例引用，不得从事件推断。
