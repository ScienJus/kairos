# Agent Daemon 平台可观测性详细设计

> 状态：MVP 已实现。本文同时保留下一阶段设计；当前精确 API 以 [OpenAPI](openapi.yaml) 为准。
> 日期：2026-09-20。

当前 MVP 包含实例注册、15 秒快照上报、45 秒失联判定、当前 Dispatch、去重事件、Human 只读 API、控制台页面，以及 30/90 天后台清理。尚未实现本设计中提到的 `outcome_ready` 事件、跨实例聚合图表和可配置的遥测参数。事件原因目前为短码/短文本，上报历史只用于观察，业务事实仍须以 Core 的 WorkItem/Task/Claim 记录核实。

## 1. 问题与现状

本地 `kairos-daemon` 已有 JSON 日志、`SchedulerStats` 和单个 Dispatch 的 `Snapshot`，能描述 Probe、Claim、运行、重试、结束等过程；这些信息只留在 Daemon 进程及其本地日志中。Core 持久化 WorkItem、Task、Claim 和业务事件，却不知道某个 Agent Identity 背后有几个 Daemon 进程，也不知道空闲进程是否仍在运行。现有 WorkItem 事件表也没有面向控制台的历史查询接口。

本功能让 Human 在平台上回答三个问题：

1. **在线吗？** 最近是否有某个 Daemon 实例成功联系 Core，调度是否因 Harness 故障暂停。
2. **正在做什么？** 当前占用哪些 execution slots，分别处理哪个候选，处于领取、运行、收尾还是核对阶段。
3. **做过什么？** 何时领取、启动、重试、结束或丢失了执行；产生的业务结果可跳转到 Core 的 WorkItem/Task 历史核实。

Daemon 上报是运行遥测。WorkItem、Task、Claim、Submission、Artifact 和 Review 仍以 Core 现有事务和记录为准。上报故障不得影响发现、Claim、Claim heartbeat、Harness 停止或结果提交。

## 2. 对象与展示语义

### 2.1 Identity、实例和 Dispatch

```text
Agent Identity（稳定的 Actor ID 和 Role）
  └── Daemon 实例（一次进程启动生成一个随机 ID，可有多个）
        └── Dispatch（实例内稳定 ID，可能尚未取得 Claim）
              └── Task Claim 或 Coordination Claim（Core 的执行责任）
```

- 不把 Agent Identity 标记为“在线/离线”。没有上报实例只表示“未见 Daemon 实例”；普通 MCP Agent 也可能使用同一 Identity。
- 实例 ID 在进程启动时生成 128-bit 以上随机值，重启产生新 ID；初版不推断机器身份，也不自动上传主机名、用户名或工作目录。
- 可选 `--instance-name` 是操作者配置的展示名，最多 128 UTF-8 字节。同名允许并存；界面同时显示实例 ID 短码。名称不用于授权或去重。
- Dispatch ID 是独立的非凭证随机 ID。不得复用 Claim operation ID、Executor Token 或 Adapter 内部 RunRef 作为对外标识。

### 2.2 三个独立状态

| 维度 | 值 | 判定与含义 |
| --- | --- | --- |
| 连接 | `reporting`、`stale`、`stopped` | Core 收到注册/上报后 45 秒内为 `reporting`；超过 45 秒为 `stale`；收到明确的退出上报为 `stopped`。`reporting` 只表示最近有联系。 |
| 调度健康 | `unknown`、`healthy`、`paused` | Daemon 报告的 Probe/系统故障状态。首次 Probe 完成前为 `unknown`；`paused` 只暂停新 Claim，已有 Dispatch 可能继续运行。 |
| 工作量 | `active_count`、`slots` | Daemon 报告当前占用的 slots。领取结果不确定、停止中、核对中的 Dispatch 都占用 slot。`active_count=0` 才显示空闲。 |

Core 使用**服务端接收时间**计算连接状态；不信任客户端时钟作在线判断。45 秒是 15 秒上报周期的三倍，界面同时显示精确的 `last_report_at`。`stale` 应写成“上报中断/失联”，不能断言进程已经退出。若控制台连 Core 都无法访问，页面显示“平台不可达”，保留最后一次成功读取的时间，不把所有实例改判为失联。

Claim heartbeat 与 Daemon 上报分别计时。Claim lease 到期也不会自动结束 Claim，只有 Core reaper 提交回收后才失去责任；Daemon 失联不能触发额外的 Claim 释放、fencing 或 WorkItem 状态变更。

## 3. 总体数据流

```text
本地 Scheduler / Dispatch
    ├── 原有 Core HTTP：发现、Claim、续租、业务结果（正确性路径）
    └── Reporter goroutine：快照 + 有类型的事件（尽力上报）
             ↓ Agent Identity Token，主动出站连接
          Core HTTP → 遥测存储 → Human 只读 API → Daemon 页面
```

Core 不连接本地 Daemon，不开放 Daemon 入站端口。Reporter 与原有调度共用 Core URL 和 Agent Identity Token，但使用独立的请求上下文、超时和退避。Harness 的 Executor Token 不参与上报。

Reporter 在 Scheduler 锁内读取短小的状态和活跃 Dispatch 快照，完成后立即释放；HTTP 请求和 JSON 编码均在锁外进行。生命周期边缘由有类型的观测钩子送入非阻塞队列；钩子不能等待网络或数据库，也不能改变 Dispatch 状态机的返回值。现有本地 JSON 日志继续存在，平台事件不是日志转储。

## 4. 上报契约

### 4.1 注册

`POST /api/v1/daemon-instances`，使用现有 Agent Identity Bearer Token。请求示例：

```json
{
  "id": "4d383a89-154a-4794-bac3-e61cb8b3a6e4",
  "name": "home-mac",
  "process_started_at": "2026-09-20T07:59:58Z",
  "daemon_version": "v0.4.0",
  "adapter": "codex",
  "slots": 2,
  "tags": []
}
```

Core 从认证结果写入 `agent_id`，请求不得自报或覆盖所属 Actor。`process_started_at` 是 Daemon 时钟记录的启动时间，仅供参考；Core 第一次注册时另记服务端 `registered_at` 和 `last_report_at`，健康态为 `unknown`，活跃集合为 `[]`。第一次创建返回 `201`；相同 ID、同一 Actor、相同不可变配置的重放返回 `200` 和同一实例，但**不刷新** `last_report_at`，也不重开已停止实例；所属 Actor 或配置不同返回 `409 conflict`。注册失败只让 Reporter 继续退避重试，Daemon 仍按原调度逻辑运行。

当前 CLI 只支持 Identity Token，因此此上报首先服务于现有 Authenticated Core/Daemon 部署。HTTP 路由仍使用 Core 的身份解析；将来若 Daemon CLI 支持 Trusted Mode 身份头，不另建一套遥测身份规则。

### 4.2 快照与事件批量上报

`POST /api/v1/daemon-instances/{id}/reports`。每个实例只有一个 Reporter 发送者；`revision` 从 1 递增，每次尝试发送新报告时分配一个新值。示例：

```json
{
  "revision": 12,
  "lifecycle": "running",
  "health": "healthy",
  "active_count": 1,
  "active_dispatches": [
    {
      "id": "b7dbb268-4f0b-4b2f-9c40-78b3c2649b09",
      "candidate_kind": "task",
      "work_item_id": "wi-123",
      "task_id": "task-456",
      "claim_id": "claim-789",
      "claim_status": "active",
      "state": "running",
      "attempts": 1,
      "started_at": "2026-09-20T08:00:00Z"
    }
  ],
  "omitted_active_count": 0,
  "dropped_events": 0,
  "events": [
    {
      "sequence": 21,
      "kind": "claim_acquired",
      "candidate_kind": "task",
      "occurred_at": "2026-09-20T08:00:04Z",
      "dispatch_id": "b7dbb268-4f0b-4b2f-9c40-78b3c2649b09",
      "work_item_id": "wi-123",
      "task_id": "task-456",
      "claim_id": "claim-789",
      "details": {}
    }
  ]
}
```

`claim_id`、`task_id`、非候选事件的 `candidate_kind` 和尚不存在的单值在 JSON 中为 `null`；集合始终为 `[]`。`claim_status` 取 `not_attempted`、`uncertain`、`active`、`ended`，特别要把 Claim POST 响应不确定与“没有 Claim”区分开。`state` 使用 Dispatch 的阶段枚举；`attempts` 为已启动的 Harness 尝试数。

每 15 秒上报一次。单次请求超时 3 秒，失败采用最高 60 秒的指数退避及 jitter。事件队列上限 1,000 条；每批最多 50 条，活跃详情最多 100 个，超过时按开始时间从早到晚保留 100 个并设置 `omitted_active_count`。`active_count = len(active_dispatches) + omitted_active_count`，且不能大于配置的 slots。单次请求体最多 64 KiB；超限时先减少批量事件数，再减少活跃详情数并增加 `omitted_active_count`，绝不截断 JSON 字符串或悄悄遗漏活跃总数。服务端也校验这些上限。报告发送期间新增或因批量上限未发送的事件留待后续周期；MVP 不为积压建立单独的快速排空调度。

服务器在一个短事务内插入事件并更新实例快照。只有 `revision` 大于已存值时才能覆盖快照并刷新 `last_report_at`；较旧报告仍可补交尚未收到的事件，但不能刷新在线时间。已停止实例拒绝新的运行态报告。事件以 `(instance_id, sequence)` 唯一去重；重复序号保留首次写入的数据且不产生第二行。成功响应为 `204`，表示整批请求已提交，Reporter 随后移除本批事件；响应丢失时保留并重送，由唯一键去重。报告发送期间新产生的事件不属于已确认批次。队列溢出时丢弃最早的**遥测事件**并增加累计 `dropped_events`；界面明确提示历史有缺口，不影响业务操作。

实例 ID 未注册时返回 `404`，Reporter 重新注册后再报；认证失效时上报失败但调度继续，不得为遥测另开高频认证重试循环。Reporter 在首次失败、错误类别变化、退避达到上限和恢复时写安全的本地类别日志，不记录请求 Body、凭证或原始 Provider 错误，也不在每次重试时刷屏。

### 4.3 事件种类

只记录有意义的边缘，不为每次发现、Observe 或 Claim heartbeat 建事件：

| 事件 | 时机和需要展示的事实 |
| --- | --- |
| `admission_paused`、`admission_resumed` | 新领取因 Probe 或系统性 Adapter 故障暂停、在健康 Probe 后恢复；只在状态切换时发出。 |
| `claim_acquired` | Core 确认取得 Task/Coordination Claim；含候选类型和 Claim ID。 |
| `harness_started`、`harness_retry` | Adapter 已确认启动；重试含尝试序号，不能把 Start 的未知结果记为已启动。 |
| `outcome_ready`、`outcome_applied` | 后者在 MVP 中已实现，只在 Core 确认或历史核对证明原意图已应用时发出；前者留待后续。 |
| `dispatch_ended` | 附带 `finished/lost`、stop reason、outcome kind、`outcome_applied`、尝试次数和耗时。`outcome_applied=false` 表示未确认，不证明业务操作没发生。 |
| `candidate_quarantined` | 本实例把该候选代次抑制；不是 Core 业务状态，也不影响其他 Daemon。 |
| `shutdown_started`、`daemon_stopped` | 收到退出信号及收尾完成；最后一次报告可保留未核对 Dispatch 的快照。 |

事件 `occurred_at` 是 Daemon 时钟，Core 另存 `received_at`。列表按实例内 `sequence` 展示，并在需要时显示接收时间；跨实例不以客户端时间推断严格因果。事件 `details` 只有固定的短字段和枚举值，不接受任意日志文本、SQL、模型输出或 Artifact 内容。Core 校验事件类型、非空引用、详情枚举和非负数值；候选引用只是导航提示，不承担业务约束。`false` 和 `0` 是有效事实，必须显式保存，不能通过 JSON 省略规则丢失。

收到退出信号后，Scheduler 先切换到 `lifecycle=stopping`，再请求活跃 Dispatch 停止；关闭窗口内的周期报告和 `shutdown_started` 事件都携带这一状态。收尾结束后最多额外等待 2 秒，尽力发送一次 `lifecycle=stopped` 报告。该报告仍受 50 条事件和 64 KiB 请求体上限约束，不承诺排空内存事件队列。若最终报告无法上报，实例保留最后一次 `stopping` 快照并在 45 秒后转为 `stale`；Core 不凭本地进程信号猜测已停止。即使报告为 `stopped`，未核对 Dispatch 的 Claim 仍由现有 lease/reaper 规则处理。

### 4.4 Human 读取 API

- `GET /api/v1/daemon-instances?limit=&cursor=&agent_id=&include_history=`：按最近上报时间降序分页，返回列表摘要与 Core 计算的连接状态；默认只含近 30 天，`include_history=true` 包含全部未清理实例。
- `GET /api/v1/daemon-instances/{id}`：返回配置、最后快照、Core 接收时间和连接状态。
- `GET /api/v1/daemon-instances/{id}/events?limit=&cursor=`：按事件序号倒序分页；空集合返回 `[]`。

所有读取响应使用现有 `{ "data": ... }` 和分页 `next_cursor` 约定，`Cache-Control: no-store`。列表与详情由服务端返回计算好的状态及 `as_of`；列表响应可在现有分页 envelope 上增加 `as_of`，不改变 `data: []` 的集合形状。前端不自行决定是否在线。历史视图通过 `include_history` 翻页；不按身份推断缺席实例为离线。没有新的 MCP 工具，Agent 工作流无需接收运行遥测。

## 5. 持久化

新增 SQLite/PostgreSQL 的增量迁移，不改写已应用的迁移。

| 表 | 专用列 | 完整聚合字段 |
| --- | --- | --- |
| `daemon_instances` | `id` 主键、`agent_id`、`registered_at`、`last_report_at`、可空 `stopped_at`、`last_revision`、`lifecycle`、`health`、`active_count` | 注册信息、`tags`、`active_dispatches`、`omitted_active_count`、`dropped_events` 保存在经过校验的完整快照 JSON 中。 |
| `daemon_events` | `(instance_id, sequence)` 主键、`kind`、`occurred_at`、`received_at`、可空的 `dispatch_id`、`work_item_id`、`task_id`、`claim_id` | 事件特有的短 `details` JSON。 |

`agent_id` 不设到 `identities` 的外键：Trusted Mode 的 Actor 不一定有持久 Identity 记录。`instance_id` 在事件表中引用实例并随实例删除。事件里的 WorkItem、Task、Claim 引用用于导航和核对，不作为授权或业务外键约束；特别是 Claim 分为两张表。需要按 Actor 列表、按最后上报时间排序、按实例分页事件和按接收时间清理，因此分别建对应索引；不为暂未提供的 WorkItem 反向查询预建索引。活跃 Dispatch 作为整体快照读写，可放 JSON；以后若需按 Task/Claim 反查活跃 Daemon，再增加专用表或列，不从 JSON 提取查询。

时间在应用边界统一为 UTC 微秒精度。事件保留 30 天；仅当实例最后上报已超过 90 天时清理其实例行及剩余事件。Core 每天以限量批次做一次尽力清理，避免一次大事务长期占用 SQLite 写锁。清理失败不影响业务事务。控制台必须标出事件保留窗口，不能把被清理的历史解释为“从未运行”。

## 6. 授权与可信度

- 注册/上报只允许普通 Agent Identity；Human、Admin Human 与 Claim 绑定的 Executor Credential 均不能代报。路径实例必须属于认证 Actor。事件和快照中的 WorkItem、Task、Claim ID 是导航提示，不在遥测事务中查询业务表；页面展示的业务状态仍以对应资源为准。不确定领取阶段允许 `claim_id=null`。Authenticated Mode 使用现有 Bearer Token；Trusted Mode 继续继承其受信网络边界。
- 读取只允许 Human Identity，沿用现有全局信任域；不额外引入团队隔离或把 Agent Token 暴露给浏览器。Admin Human 也按 Human 读取。
- 来自 Daemon 的阶段和事件均标记为“运行上报”。Claim 是否仍 Active、Task 是否完成、结果是否提交，须以 Core 既有记录为准。`outcome_applied` 是 Daemon 的核对结论，页面最终结果仍跳转到业务详情。
- 不上报 Token、请求 Body、模型输出、Artifact 内容、Provider 原始异常、本地路径、环境变量、主机名和 RunRef。`name`、版本、Adapter、ID、短原因码允许展示。服务器限制字段长度、数组长度和总请求字节数；详情和日志都不得回显敏感请求内容。

相同 Agent Identity Token 能代表该 Agent 的多个进程；实例 ID 区分运行记录，不提供比 Identity Token 更强的进程认证。随机 ID 防止偶然碰撞，不被当作秘密。

## 7. 控制台

新增顶层“Daemons”入口和独立 `/daemons`、`/daemons/{id}` 路由。遵循[页面设计基准](./page-design-baseline.zh-CN.md)：列表先用尺寸适中的独立实例卡片呈现仍在上报或等待联系的 Daemon；卡片显示名称、低强调的短实例编号、连接状态、当前或上次报告的工作概况与最后上报时间。小型节点图形与轻微的状态色提供识别线索，不使用发光或仪表盘装饰。卡片可并排但不压缩成密集表格；已停止的实例随后以低强调的时间线呈现。多实例同属一个 Agent 时分别列出，即使同名也能通过短编号区分。进入实例后，列表退出视野，页面自然滚动。详情展示：

1. **当前状态**：连接新鲜度与最后上报时间；新鲜报告说明是否在执行或暂停接单。过期快照明确标为“上次报告”，不推断此刻的工作状态。slots、健康细节与时间戳收在技术信息里。
2. **正在执行**：每个 Dispatch 的候选类型、阶段、尝试次数、Claim 确定性与 WorkItem/Task 链接；`uncertain` 和 `finalizing` 使用“领取待核对”“结果待核对”文案，不写成未领取或已完成。
3. **历史**：实例事件时间线，以事件名称、短原因和时间说明做过的事；丢弃事件时提示历史可能有缺口，业务结果仍通过相关 WorkItem/Task 链接核对。
4. **技术信息**：Agent 与实例 ID、版本、Adapter、slots、启动时间、服务端观测时间和本地丢弃事件数，放入按需展开区域。

仅在 Daemon 列表/详情可见时每 15 秒刷新；离开页面停止请求。该页面有实时状态需求，因此单独采用轮询，不让首页或全部 WorkItem 页面开始全局轮询。请求失败显示读取错误和最近成功读取时间，不覆盖已有快照。第一版只提供从 Daemon 到现有 WorkItem/Task 页的链接；WorkItem 页的“由哪个实例执行”反向关联，等明确持久归因需求后再设计。页面不提供停止、重启、清空 quarantine 等远程控制。

## 8. 故障与一致性矩阵

| 场景 | 平台表现 | 业务行为 |
| --- | --- | --- |
| Daemon 空闲但运行 | 上报新鲜、健康、`0/slots` | 不需要 Claim 才显示在线。 |
| Probe 失败，旧 Dispatch 仍在运行 | `reporting + paused + active>0` | 只暂停新 Claim，旧责任继续按原逻辑处理。 |
| Core 遥测端点不可达，其他 API 可用 | 最后上报超时后 `stale` | 调度和 Claim heartbeat 继续；事件在限量内缓存。 |
| Core 整体不可达 | 控制台显示平台读取失败；已有页面数据标旧 | Daemon 使用现有 lease 安全窗口和核对逻辑。 |
| Claim POST 结果未知 | 活跃 Dispatch 的 `claim_status=uncertain` | 保留 slot 和原 operation ID，不能据上报重领。 |
| 提交响应丢失 | `finalizing`/待核对；确认后才有 `outcome_applied` | 按现有 Core 历史核对，遥测不决定重试。 |
| Daemon 崩溃或强杀 | 45 秒后 `stale`；事件可能有缺口 | Core 最终回收未续租 Claim；不宣称终止遗留 Harness。 |
| 多实例使用同一 Agent Identity | 各自独立行与事件序号 | Core 仍由 Claim 规则防止重复领取。 |
| 上报乱序或响应丢失 | 旧 revision 不覆盖新快照，事件按序号去重 | 不改变业务记录。 |

## 9. 实现顺序与验收

1. **Core 存储与读写契约**：增量迁移、独立的遥测应用端口、注册与批量上报事务、Human 分页读取和服务端在线判定。遥测不写入 WorkItem/Claim 事务，也不附带业务 side effect。
2. **Daemon Reporter**：安全的实例/Dispatch ID、线程安全快照、状态边缘事件、有限队列、发送与退避、关闭中的 `stopping` 与退出尽力上报；补出首次 Probe 前的 `unknown` 与不确定 Claim 状态。
3. **控制台**：独立路由、列表与详情、只在可见页面轮询、过期/错误/空态、业务深链。同步更新前端类型、中英文文案、API 参考、OpenAPI、Daemon README 与部署示例，明确此功能实际发布后才修改项目状态描述。
4. **验证**：SQLite 与 PostgreSQL 的注册重放、并发 revision、事件幂等重送、分页与清理；HTTP 的 Actor/Executor 权限、空数组、字节上限；Reporter 的网络中断、响应丢失、队列溢出、退出超时、Claim 不确定；真实 Core/Daemon 二进制 E2E 验证“空闲在线 → 执行中 → 结束”和崩溃后 `stale`。实现阶段按仓库要求运行 `make go-test`、前端 build/lint 与 `git diff --check`。

验收的最低标准：Human 能区分空闲在线、暂停但仍在执行、上报中断和明确停止；能从一个活跃 Dispatch 到达对应业务页；能看到一次完整执行的关键事件，并在事件丢失时看到缺口提示；关闭 Core 的遥测写入路径不改变既有 Claim、续租和结果行为。

## 10. 初版边界

初版不流式上传本地日志或模型思考过程，不提供秒级进度百分比，不推断机器是否真正存活，也不将 Daemon 上报用作调度、fencing 或 Claim 归属依据。Daemon 若在领取成功后、上报前崩溃，Core 仍有 Agent Identity 与 Claim 历史，但该 Claim 未必能准确归到某个 Daemon 实例；若运营上必须保证逐 Claim 的实例归因，可后续在两种 Claim 创建请求中增加**可选**实例 ID 并存入专用列，且领取成功不依赖遥测注册是否可用。这是单独的跨 HTTP/MCP、领域、持久化和文档变更，不在第一版偷偷通过事件推断完成。
