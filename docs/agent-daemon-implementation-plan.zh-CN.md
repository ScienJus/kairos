# Agent Daemon 分阶段实现规划

依据：[Agent Daemon 白皮书](whitepapers/agent-daemon.zh-CN.md)。本规划描述实现顺序与验收条件，
不新增领域设计。阶段 1–4 已合入；阶段 5 已完成实现、本地验收与全量 review。CI 与发布状态以对应 PR/Release 为准。

## 目标与边界

首个可用版本支持单一 Agent Identity、一个本地 Codex Adapter，以及 Workflow Task 和三类
Blackboard Coordination Dispatch。Daemon 独立运行，Core 继续管理持久状态。

MVP 保持轻量：不引入 Dispatch 持久化、supervisor、跨进程重连、全局公平调度或任务优先级。
现有 Claim/Coordination Claim、生命周期 API 和幂等机制优先复用；不新增统一的
apply_coordination_decision 或批量 Blackboard 规划 API。

实现按五个可独立验收的阶段推进：

| 阶段 | 交付结果 | 依赖 |
| --- | --- | --- |
| 1. Core 执行凭据（已验收） | Claim Token、principal 和服务端授权完整闭环 | 当前 Core |
| 2. 单次 Dispatch 引擎（已合入） | fake Adapter 可完成两类 Dispatch 的生命周期 | 阶段 1 |
| 3. 连续调度与故障抑制（已合入） | slots、Probe、cooldown、预算和 quarantine | 阶段 2 |
| 4. 本地 Codex Adapter（已合入） | CLI 进程、MCP 指令与 outcome schema、隔离 workspace；macOS 真实模型 smoke 已通过 | 阶段 3 |
| 5. 集成与交付（初版已实现） | 二进制 E2E、隔离示例、双二进制发布与 SBOM/Notices 接入 | 阶段 4 |

阶段 1 不依赖 Daemon；阶段 2 不接真实模型；阶段 3 的重试边界通过后，才开放真实 Harness 的
连续领取循环。这样每阶段都有明确成果，也避免开发过程中反复消耗 Claim 历史。

## 当前基础与代码落点

当前已有 Identity Token 管理、Role 校验、Task/Coordination Claim、Claim-bound Executor Token、
heartbeat、reaper、Artifact、HTTP/MCP 和 SQLite/PostgreSQL。`internal/daemon` 已包含单次
Dispatch 引擎、HTTP client 与连续调度器；`cmd/kairos-daemon` 提供诊断模式与显式启用的本地 Codex Adapter。

主要复用位置：

- `internal/identity`：Identity Token 认证与 Actor/Role。
- `internal/application`：Claim、生命周期、上下文、Artifact 与幂等规则。
- `internal/repository`：两类 Claim 的持久化、事务和双数据库 migration。
- `internal/httpapi`、`internal/mcpapi`：认证入口、请求/响应和工具映射。
- `docs/openapi.yaml`、API reference、Agent Skill 和 examples：对外契约。

代码位于 `cmd/kairos-daemon` 和 `internal/daemon`；后者组织 HTTP Core client、Dispatch 引擎、
调度器、Adapter 契约和本地 Codex Adapter。包拆分以依赖方向为准，不先建立插件框架或公共 SDK。

## 阶段 1：Core Executor Credential 与授权（已验收）

2026-09-04 已在 SQLite 与本地 Docker PostgreSQL 17.11 上通过验收：配置 PostgreSQL DSN 后，
`make go-test` 和 `go test -race ./internal/repository -count=3` 均通过，迁移执行到
`004_executor_credentials`。幂等 Artifact/规划写入覆盖 release、reaper、cancel、fail 和完成路径
的两种提交顺序及写入回滚；Blackboard 规划先提交时，新 Task 会阻止 WorkItem 提前完成。

### 实现范围

- 在 Task Claim 和 Coordination Claim 领取请求中增加可选 Executor Token。
  Core 校验前缀与格式，在领取事务内保存 hash；省略时维持原有 Agent 行为。
- 两类 Claim 增加专用、可空的 token hash 列及唯一索引，通过联合查询解析 Claim 类型。
  hash 不进入公共响应、上下文或日志。
- 认证入口按完整 `krs_claim_` Token 格式区分凭证。引入可区分 Identity credential 与 Claim credential
  的 principal；普通 Identity 的 Role 规则保留，Executor principal 不伪造或查询 Role。
- 在 application operation 层落实 `scoped_read`、`task_artifact_write` 和
  `blackboard_planning_write`，其他操作默认拒绝。HTTP/MCP 共用授权规则。
- 检查返回的嵌套数据及 Artifact 内容路径，防止仅校验顶层 ID，却暴露 scope 外或未授权的
  未提交 Artifact。不能用“Actor 相同”替代“绑定 Claim 相同”。
- Executor mutation 在业务事务内重新校验 Claim Active，并确定 SQLite/PostgreSQL 的锁定
  与并发顺序；不能只依赖传输层的首次认证。
- Claim 幂等请求必须区分不同 Token，但请求指纹、响应和持久记录不能保存明文。
  幂等重放也不得绕过凭据有效性及 scope 校验。

本阶段将凭据创建、认证和授权作为同一个交付单元，未完成授权前不开放 Executor Token。

### 验收条件

- 普通 Agent 的 HTTP/MCP 流程、Role 过滤和 Claim 二次校验不回归；Trusted Mode 保持原行为。
- 两类 Claim Token 可读取绑定 WorkItem 内的 Task/WorkItem context 和已提交 Artifact 内容。
- Task Executor 可执行约定的非终态写入；Coordination Executor、跨 scope 请求和生命周期
  直接调用均被拒绝，包括使用同一 Actor 的另一个 Claim。
- Identity Token 撤销/轮换不使 Active Executor Token 失效；Claim 实际结束后读写均被拒绝。
- 相同领取请求重放得到同一 Claim；更换 Token 的不兼容重放被拒绝；结束后的 Claim 即使被
  幂等返回，也不会重新获得执行权限。
- 真实 SQL 测试覆盖 mutation 与 release/reaper/WorkItem 终止的竞态、回滚和提交状态，
  以及异常多条 hash 命中时拒绝认证。

### 同步文档与兼容性

更新 OpenAPI、HTTP/MCP 请求与响应说明、API reference；如响应形状变化，同步 frontend types
和空集合回归测试。实现前核实当前 migration 的发布状态，默认新增迁移，不改写可能已部署的
历史 migration；旧 Claim 的新增字段为空，内置生成器的旧 43 字符 Token 继续走 Identity
认证，即使它恰好含有 `krs_claim_` 前缀。自定义旧 Token 若完整匹配新 Executor 格式，升级前
需要轮换；Claim 认证失败不回退 Identity 查询。

## 阶段 2：单次 Dispatch 执行引擎

初版代码位于 `internal/daemon`，包含 Adapter/outcome 契约、HTTP client、独立 heartbeat guard、
单次执行状态机与 fake Adapter/真实 HTTP + SQLite 测试。开发入口及恢复语义见
[包说明](../internal/daemon/README.md)。当前未接真实模型，不新增 Core API 或数据库迁移。

Dispatch 的公开推进入口收窄为 Run，拒绝并发驱动但允许前次返回后的顺序恢复；step 与
heartbeat 为内部方法。Scheduler 通过内部 claimOnce 完成首次领取后交接给 Run，两条入口
共用驱动占用标志。保留并发 RequestStop、Snapshot 和独立续租；启动前确认与后台续租
仍可能重叠，因此续租保留串行锁，不再为 step 和 Run 分别维护阻塞锁。
Snapshot 在读取时由 candidate、Core 确认的 Claim 与冻结 intent 派生，执行状态与
OutcomeApplied 独立保存。结果确认仍使用原有精确核对规则，不将“未确认”改判为成功。

`Run(ctx)` 被取消后进行一次有时限的清理；若 Core 状态仍未知，返回的 Dispatch 保持非终态，
调用方保留同一对象与 slot，并使用新 context 继续运行。不能因函数已返回就创建替代 Dispatch。
`OutcomeApplied` 只表示已确认落地；Claim 已结束但历史证据不足时，不报告业务成功。
输出本身的集合、human executor、transition 子集及历史文本长度约束在 Daemon 边界校验，
非法输出走 Harness 有限重试；出站 collection 统一编码为 `[]`。Completed 核对按 mode、
ReviewPolicy 与 RequestReview 推导精确 Claim 结束原因，保留 Workflow 强制 Review 语义。
创建根 Task 与分解子 Task 的 Role/Tag 必须无首尾空白；不规范值在 outcome 边界拒绝并重试，
避免持久化后无法按精确 Role/Tag 匹配发现和领取。
续租间隔与安全余量分别由 Core 确认的 Lease 的 1/5、1/10 推导，独立定时器在续租或安全截止点
唤醒；`PollInterval` 只控制 Harness 观察。停止信号立即唤醒控制循环，停止窗口已过也会尽力
调用一次 Stop。领取前的 context 请求失败不产生新的领取不确定状态；稳定幂等 Claim POST 的
明确 Core conflict 可解除此前不确定状态，后续鉴权或网络失败则不能据此推断旧 Claim 不存在。
首次 Observe 失败后，从该次失败响应起计算 `StopTimeout` 恢复窗口，重试等待和 Observe 请求
均不得越过其截止时间；窗口内成功则恢复正常轮询，到期或迟到响应均进入停止流程。

### 实现范围

- 定义 DispatchState、RunState、StartRequest、RunRef、HarnessOutcome，以及
  `Probe / Start / Observe / Stop` Adapter 接口。
- 提供 Core HTTP client、可控时钟与 fake Adapter，先执行显式选定的一个 Candidate，不启动
  持续发现调度，也不依赖真实模型。
- 同时实现 Task Dispatch 和三类 Coordination Dispatch，不能只做 Task 正常路径。
- Claim 成功后独立启动 heartbeat，覆盖启动、观察、重试和 finalizing；Claim 响应丢失时复用
  operation_id 与 Token，确认执行权仍有效后才能启动 Harness。
- 按 outcome 类型、WorkItem mode 和 Candidate kind 校验并执行唯一 Core 映射。
  在内存中固定终态意图，通过 Context、Claim 原因和相关业务历史 reconcile。
- 区分“请求 Stop”“确认停止”“RunState.lost”和 Dispatch 终态。只有 Run 已结束或 lost，
  且 Core 确认 Claim 结束，才能结束 Dispatch。
- 实现单 Claim 内的有限运行时重试及停止路径；Start 错误只有在 Adapter 原子契约下才允许
  重试，旧 Harness 未确认结束时不得重复启动。

### 验收条件

- fake Adapter 覆盖所有合法 TaskOutcome 与 CoordinationDecision，非法模式组合按协议错误
  处理，不生成业务 Failure。
- outcome_ready 后 heartbeat 继续；runtime_failed 可以在预算内重新 starting；lost 不能在
  同一 Claim 内启动替代 Harness。
- Task 与 Coordination 基础设施失败分别调用正确的 release API，不混用资源 ID。
- 测试 Claim/heartbeat/终态响应丢失、Core 暂时不可达、身份失效、WorkItem 取消、reaper 回收、
  Stop 超时与 Observe 调用错误。
- 终态成功响应丢失后不重复创建 Task、Submission 或 Failure；Core 状态未知时不假定完成。
- 不依赖墙上时钟同步判断安全停止点；测试尽量使用 fake clock，避免真实 sleep。

## 阶段 3：连续调度、Probe 与跨 Dispatch 抑制

初版已实现，使用方式与默认参数见 [Daemon README](../internal/daemon/README.md)。
`Discover` 从 Core 的每类限额快照取候选，并按 WorkItem 缓存读取执行上下文；fingerprint
包含 Definition、WorkItem 业务字段、Task 及其业务历史、Relations 和已提交 Artifacts，
排除 Claim 历史、ActiveClaimID、Version、UpdatedAt，并把 working 归一为 pending。
共享上下文发生实质变化也会形成新代次；有限快照之外的候选没有全局公平性保证。
发现阶段按单个 HTTP 请求计时；后续上下文请求失败时保留已解析候选，但取消或认证失败
禁止继续领取。明确未发送的领取重试重新经过健康门控；已有不确定领取结果的幂等核对
不受暂停阻断，后续单次未发送也不清除此前的不确定性。Probe 与领取调用使用独立超时预算。
系统故障由 Adapter 的 `SystemError` 或 `RunObservation.SystemFailure` 标记；普通异常按候选处理。
Start、Observe、Stop 返回的系统错误都会暂停新领取。
Probe 串行执行，但排队等待响应取消和截止时间，不阻塞停机；取消的等待者不调用 Adapter，
也不修改健康状态或代次。
在途 Probe 因调用方取消或截止而中止，同样不视为健康故障；探测自身 RequestTimeout
到期或 Adapter 实际失败仍触发退避和健康恢复规则。
健康恢复仅解除全局暂停，不清空 cooldown、预算或 quarantine；quarantine 由业务代次变化
或新建 Scheduler（通常为重启）解除，部分候选可能需要人工重启。每个 Scheduler 只接受一次
Run，并发或再次调用均报错；取消表示停机。单次 Dispatch 的状态保留与核对能力不变。
配置通过重新创建 Scheduler/重启生效；状态不持久化。诊断 Adapter 默认不可用，
需显式选择 `fake-abandon` 才会领取并放弃候选，不调用真实模型。
未取得 Claim 且未启动 Harness 的 Dispatch 结束后统一清理空 workspace，包括延迟拒绝和
领取前取消；非空目录、已领取工作和结果仍未知的目录保留供检查。

### 实现范围

- 增加独立 Daemon 命令与最小配置：Core 地址、Agent Identity Token、Adapter 配置、
  workspace 根目录、slots、lease、Harness 观察间隔、停止时限、重试与 cooldown 参数；续租周期由 lease 推导。
  具体默认值在本阶段选择，并由测试固定。
- 共享 slots 驱动四类 Candidate 的本地轮询；成功 Claim 才推进游标，冲突后重新发现，
  cooldown/quarantine 中的候选不进入 Claim。
- 首次领取和健康暂停/cooldown 恢复前进行 Probe。系统性运行故障暂停整个 Daemon 的新 Claim，
  Candidate-specific 故障只抑制对应代次；Probe 不创建 Claim 或 Harness 运行。
- 将 cooldown、跨 Claim 预算和 quarantine 绑定 Candidate 代次。确定具体 fingerprint：
  业务上下文、Retry Prompt、Review、计划或结果变化能形成新代次，纯 Claim churn 不能。
- abandoned 在 release 后直接 quarantine；它不能作为成功结果清除失败状态。
- 成功写入非 abandoned 业务 outcome 后清除当前代次记录；业务代次变化或新建 Scheduler
  使适用的旧记录失效。健康恢复只解除全局暂停，保留所有候选抑制状态。
- Scheduler 的 Run 为一次性调用，拒绝并发调用、停机后的再次调用；运行期间保留未知
  Claim 核对和 slot，有界停机结束时不把未解决的 Dispatch 标记成已完成。
- 增加结构化日志及基础观测：运行数、Claim/heartbeat、Probe、重试、候选抑制、终态和耗时。
  敏感 Token、模型完整输出和 Artifact 内容不进入日志。

### 验收条件

- slot 满时不再 Claim；stopping 且 Claim 仍未知的 Dispatch 不释放 slot。
- Probe 持续失败不会增加 Claim；健康恢复后仅按原有预算与 cooldown 重试，quarantine 不解除。
- 验证 Run 并发调用、正常停机或未解决 Claim 停机后的再次调用，以及初次 context 已取消的情况。
- 对稳定 malformed output/context overflow/abandoned 的 fake Harness，长时间调度测试证明
  同一候选代次不会无限消耗 Claim。
- release、heartbeat、reaper 不清零预算；其他执行者带来的实质业务变化解除旧 quarantine。
- 多类候选、空类、冲突、受限候选快照和多个 Daemon 并存时不重复领取；不把测试结论扩张为
  全局 WorkItem fairness。
- 达到 Core 历史上限时接受其权威结果，不继续领取或试图绕过保护。

## 阶段 4：本地 Codex Adapter

实现位于 `internal/daemon/codexadapter`，支持 Linux/macOS 和 Codex CLI 0.146.x。
已核实本机 0.146.0 CLI 参数及 MCP 配置解析；使用 `codex exec`、结构化结果文件、独立
attempt workspace、环境变量 Executor Token、MCP 初始化指令与简短启动 prompt。Provider 登录
使用显式指定的专用 Codex home，模型由操作者指定。workspace-write 允许网络访问，以读取
托管 Artifact 和访问仓库；不继承 Daemon Identity Token，不宣称同用户进程间的 OS 级秘密隔离。

Probe 仅检查 CLI 版本与本地登录状态，不调用模型，也不证明 Provider 配额或模型可用。
Stop 先发送一次 SIGINT，由 Codex 清理其普通 shell 独立进程组；两秒内未退出则尝试强杀根
进程组，但只报告 lost，不把根进程消失当作整个执行停止。异常信号退出或残留进程组同样
报告 lost，可能需要人工清理；不引入进程树 supervisor。
可选 RunForgetter 在 Dispatch 终态或确认旧运行结束后释放 Adapter 内存记录；若请求早于
Adapter 清理完成，则保留到运行结束后自动回收，不删除活动运行或 workspace。
Codex 输出的 runtime_failure 信封仅转换为运行时失败，业务 outcome 沿用既有契约。

已通过假 CLI 真实进程、凭据分离、停止、非法输出及真实 Core HTTP/MCP + SQLite 测试，
覆盖两类 Profile 的上下文/已提交 Artifact 内容读取、Workflow 提交/Review/transition、
Blackboard 全流程与分解。真实 CLI + 本地模拟 Provider 还覆盖普通长命令的中断、正常完成
与异常强杀，不调用真实模型。2026-09-06 已另行通过 macOS + Codex CLI 0.146.0 +
`gpt-5.6-sol` 的真实模型 smoke：独立临时 Core/SQLite 中，Task 上传托管 Artifact，随后
Coordination 通过 HTTP 读取已提交内容并提交完成；两次 Dispatch 均一次成功，Claim 已结束、
WorkItem 已完成、运行记录已回收。Linux 实际 CLI 及其他真实模型场景尚未验收。
使用方式与边界见 [Adapter 说明](../internal/daemon/codexadapter/README.md)。

### 实现范围

- 核实实际 Codex CLI 的非交互运行、结构化结果、MCP 配置、凭据注入和取消机制，明确支持的
  CLI 版本与平台；不预先依赖未经验证的命令参数。
- 实现 Probe 和原子的 Start：优先在创建进程前完成配置检查；失败路径确认进程及受控
  子进程退出、资源清理后才返回可重试错误。
- 实现 Observe、结果解析和尽力 Stop；Harness 异常与业务 outcome 分离。
- 为每次 Dispatch 分配隔离 workspace，通过受保护的 secret 通道注入 Executor Token，
  不向 Harness 提供 Agent Identity Token。
- 复用按 Token Profile 返回的 MCP 工具与初始化指令；启动 prompt 和 outcome schema
  只补充当前执行及结果约定，不维护独立 Managed Skill。
- 验证两类 Profile 的上下文与托管 Artifact 内容实际读取路径。现有 HTTP 内容接口可复用，
  不因缺少对称 MCP 工具而假定 Harness 已能取得文件内容。

### 验收条件

- 先以脚本式假 Harness 验证真实进程启动、取消、快速退出、非法输出和清理，再进行用户配置
  模型的 smoke test。
- Start 错误无遗留运行；Stop 只表示请求，Observe 确认后才能按状态机继续。
- Workflow Task 可正常提交、请求 Review 与携带合法 transition；Blackboard 可完成初始规划、
  Task 执行/分解、completion 和 acceptance。
- Harness 只能看到 Executor Token；日志、进程参数及临时配置不意外暴露长期 Identity 凭证。
- Daemon 崩溃测试只验证约定边界：heartbeat 停止后由 Core 回收 Claim，不宣称自动重连或
  清理所有外部副作用。

## 阶段 5：集成、验证与交付

独立示例位于 `examples/daemon`，启动 Authenticated Core/SQLite 并为 Workflow 和 Blackboard
创建示例；原生 MCP quickstart 保持独立。`make daemon-e2e` 显式编译并运行真实 Core 和
Daemon，以脚本式假 Codex 执行两种流程，并覆盖两类 Claim 的失败抑制、取消/停止超时和
Daemon 崩溃后的实际 reaper 回收；不调用模型。CI 单独启用该套测试及 race 检查。
示例通过 Perl/POSIX 启动包装将 Core 放入独立进程组，回归测试向整个前台组发送 SIGINT/
SIGTERM 并重复发送，确认 Core 在 Daemon 收尾期间存活、随后 Claim 正常释放。
`make build` 构建两个二进制；GoReleaser 按四个平台目标打包两个二进制、示例与使用说明，
Notices 覆盖两个程序的依赖并集，发布 workflow 将八份后端 SBOM 与前端清单合并。
实际发布、Linux 上真实 Codex 和更多 Provider 故障场景仍是独立的验证边界。

- 新增独立的 Agent Daemon Workflow/Blackboard 示例，保留现有原生 MCP quickstart。
- 自动化 E2E 默认使用 fake/script Harness，不依赖付费模型；真实 Codex smoke test 单独启用。
- 覆盖真实 Core HTTP/MCP、凭据认证、托管 Artifact、取消、超时和 reaper 的完整组合。
- 在 Makefile、CI 和适用的发布配置中接入独立 `kairos-daemon` 二进制及支持平台。
- 更新 README、Roadmap、站点状态、API 文档、MCP 指令、示例与安装/启动说明。
  仅在对应路径实际通过验收后，将 planned 改为 implemented。
- 记录配置、Probe 的可检测范围、quarantine 解除规则、Token 撤销语义、优雅停机和崩溃限制。

最终验收：新用户可在独立本地环境中启动 Core 与 Daemon，完成一个 Workflow 和一个 Blackboard
协作流程；基础设施故障不会被 Daemon 伪造为业务 Failure，连续运行不会对同代次候选无限 Claim，
所有权限与终态边界都能由自动化测试验证。

## 每阶段的验证与交接

每阶段交付代码、聚焦测试、对应文档及已知限制，不将关键安全测试留到阶段 5。

- 后端或共享契约修改：`gofmt`、`make go-test`、`make go-vet`。
- 涉及持久化与事务：运行真实 SQLite/PostgreSQL 回归。PostgreSQL 使用现有
  `KAIROS_TEST_POSTGRES_DSN` 测试配置；未配置时不得将跳过报告为已覆盖。
- 并发执行引擎与调度：补充适用包的 race 检查和确定性状态机测试。
- 涉及 frontend types/行为：在 `web/` 运行测试、`npm run build` 和 `npm run lint`。
- 交接前运行 `git diff --check`，并再次扫描旧术语、遗漏状态与中英文契约差异。

阶段 5 已通过全量 review 与本地归档验收；合入前须通过 CI，创建 Release 仍需单独授权。
