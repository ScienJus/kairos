# 前端开发手册

本文记录 Kairos 前端的工程边界。视觉与交互目标见[页面设计基准](page-design-baseline.zh-CN.md)；领域语义与字段契约分别以白皮书和 [OpenAPI](openapi.yaml) 为准。

## 1. 从页面需求出发

Kairos 只有少量页面，不按大型管理后台建立全局数据层。

- 页面或紧邻页面的 hook 拥有自己的 Query 和局部状态。
- 进入页面时加载它需要的数据，操作后只刷新当前可见且受影响的数据。
- 离开页面后停止它的 Query；缓存只减少重复请求，不承担业务正确性。
- 默认不轮询、不乐观更新、不维护跨页面即时一致。Daemon 观测页是明确例外，只在可见时每 15 秒刷新。
- 根组件只协调路由和顶层弹层，不集中管理全部页面 Query。

抽象应减少调用方需要理解的细节。只移动 JSX、但不改变数据和状态所有权，不算有效重构。

## 2. 后端裁定能力，前端呈现能力

依赖领域不变量或权限的判断必须由后端计算：

- capability 与对应命令复用同一校验函数；
- 前端只按 capability 显示操作，不从 Task 状态、历史、Claim 和身份重复推导；
- 命令端始终重新校验，capability 不是授权凭据；
- 操作后重新读取，不将 capability 长期缓存为本地事实。

纯展示判断可留在前端，例如默认展开哪个操作或是否折叠已完成历史。

## 3. 先定义操作矩阵

不按按钮逐个追加 Task 操作。实现前列出：

| 维度 | 最小覆盖 |
| --- | --- |
| Task 状态 | pending、working、waiting_children、in_review、终态 |
| 执行者 | human、agent、either |
| 当前身份 | 负责人、非负责人、reviewer |
| Claim | 无、自己持有、他人持有、已结束 |
| 请求 | 加载、成功、领域冲突、网络失败 |
| 导航 | 切换 Task、关闭重开、URL 刷新 |

每个合法组合必须明确展示什么、提交什么、成功后刷新什么。

## 4. 统一操作容器

Task 的 Claim、Submit、Release、Fail、Review、Decompose、Append Child 和 Skip 共用一个 `OperationPanel` 模式：

- 一次最多展开一项，也可全部收起；
- 标题、一句说明、表单/确认动作、错误和提交状态使用同一布局；
- 切换同一 Task 内的操作时可保留草稿；切换 Task ID 时必须隔离；
- Mutation 失败不关闭面板或清空输入，成功只清理对应表单。

Artifact 上传和外部 URI 登记先取得 Artifact ID，再随 Submission 提交。响应丢失后复用同一幂等 key；文件、URI 或 Claim 改变后生成新 key。

## 5. 组件与请求状态

- URL、Query key 和组件 key 使用同一组业务 ID。
- 从 Task A 切到 B 时，A 的草稿不得出现在 B。需完全隔离时用 Task ID 重新挂载。
- 加载显示稳定占位；请求失败、无权限和无操作必须有不同呈现。
- Detail 与 Execution Context 独立加载和报错，执行数据失败不覆盖基本详情。
- 不用本地猜测复杂领域变化；Mutation 成功后以后端响应或重新读取为准。

## 6. API 类型与空值

- JSON 集合类型是 `T[]`，空集合是 `[]`，不使用 `T[] | null`。
- 可选单对象可以为 `null`，例如 Active Claim、Parent Task 或模式专属 Context。
- Mutation 输入使用明确类型，不用 `object` 或 `Record<string, unknown>` 绕过编译检查。
- `FormData` 在组件边界转为明确字符串、枚举和数组。
- API 形状变更同步更新前端类型和空集合回归测试。

## 7. Query 与刷新

| 操作层级 | 成功后刷新 |
| --- | --- |
| Task | Task Detail/Context 与当前 WorkItem |
| WorkItem | 当前 WorkItem |
| Workspace | WorkItem 列表与 Human Attention |
| Daemon | 当前实例或列表，且只在页面可见时轮询 |

Query key 必须包含身份与业务 ID，身份变化后不复用旧权限上下文。不可见页面或未打开功能的 Query 保持 disabled。跨 WorkItem 聚合信息由后端提供聚合 API，不用 N+1 Context 请求拼装。

## 8. 身份、会话与 Token 管理

Kairos Web UI 服务 Human；Agent 使用 MCP/Skill，前端不保留伪装 Agent transport 的调试分支。

Authenticated 登录完全信任 `/session` 返回的身份和 `can_manage_identities`，不从 ID、Role、前缀或显示名称推导权限。Identity Token 与 Admin Token 只保存在当前标签页 `sessionStorage`；退出或当前凭据收到 `401` 时清除凭据、Query 缓存和在途会话。存储不可用时明确报错。

Admin 使用同一登录会话进入 Token 管理页。新签发 Token 只保存在页面内存，不进入 URL、浏览器存储或通用 Query/Mutation 缓存；离开、刷新或开始下一次凭据操作前必须复制保存。身份列表和详情不返回明文 Token。凭据 Mutation 不自动重试，因为响应丢失时首次请求可能已经提交；先刷新元数据，再决定是否轮换替代 Token。

从浏览器 back/forward cache 返回时重新加载身份元数据，但不恢复 Token 或重放写请求。Actor ID 在 URL 中编码为单一路径参数。前端与 Go `strings.TrimSpace` 使用一致的 Unicode White_Space 规则，不能直接以 JS `trim()` 代替；Actor ID 必须包含非空白字符，且不能等于 `.` 或 `..`。

Human Attention 使用后端聚合结果，不在前端重新计算 owner、Review、验收或 Workflow 恢复资格。

## 9. 特殊流程

- Workflow 单节点实例上限显示有效值；未加载 Definition 时不假定默认值。
- `fail_task` 使用 `retry`、`await_human` 和 `fail_work_item`；`retry_prompt` 只随 `retry` 提交。
- Workflow 恢复提供“继续执行”和“从头执行”，失败时保留输入。继续执行的建议上限使用后端 `recovery_task_ids` 计算，不复制 Claim 终止原因逻辑。
- 受限执行者不能读取来源 WorkItem 历史，恢复表单应提示 Human 在当前说明中写出需复用的外部成果。
- Daemon 页的在线判定只使用 Core 返回的 `connectivity` 和 `as_of`，不使用浏览器时钟。

## 10. 验证

交互变更至少覆盖：

1. 正常路径：操作可见、请求正确、成功后状态更新；
2. 身份路径：owner、非 owner、Human、Agent 和 reviewer 能力正确；
3. 负向路径：冲突与网络失败可恢复，且不丢输入；
4. 导航路径：切换对象、URL 刷新、前进后退时状态不串联；
5. 契约路径：空集合为 `[]`，可选对象才为 `null`；
6. 一致性路径：capability 与后端命令接受条件一致。

完成后运行：

```bash
make go-test
cd web && npm test && npm run build && npm run lint
git diff --check
```

视觉截图只验证一个时刻的样式，不能取代状态转换和负向路径验证。
