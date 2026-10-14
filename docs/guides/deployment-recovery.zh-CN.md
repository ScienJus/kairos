# Kairos 部署与备份恢复指南

[English](deployment-recovery.md)

适用源码基线：`faf6967`（合并 PR #70）。交付与验证记录见[修订循环验收记录](../review-loop-validation.zh-CN.md)。本指南依据下列仓库文件及 WorkItem 提供的演练证据编写；本次编写没有运行生产操作或重新执行演练。命令中的路径、端口和进程号均为操作员替换的示例。默认采用单机 SQLite、停服复制、独立恢复目录和 loopback 验证。

## 1. 准备与启动

取得与基线对应的已审核二进制，按 `docs/releasing.zh-CN.md` 核对发行包 checksum、版本输出和来源 Attestation（适用 Release workflow 产物）。记录版本、配置项名称、数据路径、运行用户及恢复集编号；不要记录真实 Token 或带密码的 DSN。不同版本可能在启动时自动迁移数据库，恢复验证首先使用备份时相同版本。

使用专用运行用户、固定工作目录和明确的绝对数据路径，避免相对默认路径随工作目录改变。示例预先建立专用空数据目录（不要对现有数据目录盲目改权限）：

```sh
umask 077
install -d -m 700 /srv/kairos/data /srv/kairos/data/artifacts
export KAIROS_SQLITE_PATH=/srv/kairos/data/kairos.db
export KAIROS_ARTIFACT_DIR=/srv/kairos/data/artifacts
export KAIROS_LISTEN_ADDR=127.0.0.1:8080
export KAIROS_POSTGRES_DSN=''
export KAIROS_AUTH_MODE=authenticated
# 通过受控秘密注入设施设置 KAIROS_ADMIN_TOKEN；不在命令历史中填写明文。
/opt/kairos/kairos-server
```

这些目录和二进制路径需按部署调整，创建目录的用户应与运行用户匹配。Authenticated 模式必须已有 Admin Token；至少 32 个可见 ASCII 字符，无空白或控制字符，并应具备高熵。空 Token 会使该模式启动失败。切勿把默认 trusted 模式暴露给不受信网络。

Core 默认监听 `127.0.0.1:8080`，默认 SQLite 为 `kairos.db`，Artifact 目录为 `artifacts`；非空 `KAIROS_POSTGRES_DSN` 优先于 SQLite 配置。启动时先验证数据库并应用内嵌 migration，再接收请求；连接、迁移或存储安全检查失败应修正原因，不能通过切换空库掩盖故障。

SQLite 使用 WAL，数据库及存在的 `-wal`、`-shm` 文件权限为 0600。Artifact 根目录必须为专用目录，不能是文件系统根或自身为符号链接，已存在目录必须为 0700；托管文件为 0600。父路径别名会被规范化。按 `internal/artifactstore/local.go` 的检查处理启动错误，不放宽权限。

启动后：

```sh
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/auth/config
```

两者无需认证；健康检查成功不代表身份权限、业务数据或 Artifact 内容已验证。用受控客户端调用 `GET /api/v1/session` 核对实际身份，再检查预期 WorkItem 和已提交 Artifact；不要把 Authorization header 放进调试日志。HTTP API 前缀为 `/api/v1`，MCP 为 `/mcp`。公网访问应经 TLS 反向代理并限制连接、速率、请求体；MCP 转发 loopback 时使用 loopback 上游 Host，并保留 Authorization、Origin。403 invalid Host header 应检查代理配置。

## 2. 认证与执行环境边界

Authenticated 模式忽略 Trusted Header。业务访问可使用 Identity Token、映射稳定 Human Actor 的 Admin Token、或绑定 Active Claim 的 Executor Token；身份管理只接受 Admin Token。Admin Token 轮换后重启所有服务实例，同一数据库保留原 Human Actor，不能手工删改 Actor 行。整个数据库都必须备份。

Trusted 模式仅限明确受信边界，由 Header 指定 Actor；Agent 必须有 role，Human 不提供 role。Authenticated 模式仍是单一全局信任域，不提供多租户隔离，互不信任群体应分别部署。

Core 保存 Executor Token 的 SHA-256 hash，该 Token 只在绑定 Claim Active 期间有效；恢复数据库不是重启旧执行者或复活旧 Claim 的承诺。Daemon 管理 Claim、续租和最终提交，维护人员不要直接修改这些记录。

数据库不能替代原始配置及秘密管理备份。单独保护并保留可恢复的运行配置、Admin/Identity 凭据供应方式、TLS/代理配置及带秘密的 PostgreSQL DSN；数据库中的 hash 不用于还原明文 Token。公开清单只记录秘密引用，秘密备份应加密、限权并与普通指南分开。

若使用 Codex Adapter，依据 `internal/daemon/codexadapter/README.md`：需要显式选择模型和专用登录目录，要求 Codex CLI 0.146.0 或更新且通过所需选项探测；`--codex-home`、`--codex-model` 必填。Daemon 默认 unavailable Adapter 不执行模型。Provider 登录目录与 Kairos Identity Token 存储、Daemon workspace 分离。探测通过不证明配额、模型、网络或运行兼容性。

Workspace 是任务文件和操作员检查资料，不是 managed Artifact store，Core 数据备份不自动覆盖它。按业务需求另行备份敏感 workspace；忘记运行元数据不会删除 workspace。Provider 登录和真实凭据不得打包到发行物或普通恢复证据。相同用户仍可能读取这些文件，非交互 sandbox 不等于操作系统隔离，不受信任务使用专用用户或容器。

## 3. 有序停机

1. 安排维护窗口，通知所有写入方停止提交新请求；在各运行主机核对属于该 Core 的全部 Daemon 实际 PID，并防止服务管理器自动重启它们。当前没有通过控制台远程暂停调度或停止 Daemon 的能力；以下操作由操作员在进程所在主机执行，不能把控制台观测状态当成停止操作。
2. **先向所有 Daemon 发送 SIGTERM 或 SIGINT，再等待所有 Daemon 退出。** 下例使用 SIGTERM；把示例 PID 替换为已核对的全部 Daemon PID，在每台相关主机执行。先完成所有发送，再逐个等待；Core 此时保持运行。

   ```sh
   # 示例 PID，必须替换；不要填写 Core PID。
   set -- 12345 12346
   kill -TERM "$@" || exit 1
   for daemon_pid in "$@"; do
     while kill -0 "$daemon_pid" 2>/dev/null; do sleep 1; done
   done
   ```

   使用拥有这些进程的运行用户执行。发信号失败时停止流程并调查；等待不设自动强杀期限，持续不退出时检查受控日志、进程身份和服务管理器状态，不继续停止 Core。`kill -0` 只辅助检查 PID 是否仍存在；必须结合进程/服务管理器确认原 Daemon 已退出、没有自动重启，检查可取得的退出状态和日志。Core 中的 `stale` 只是遥测失联，不能证明退出或 Claim 已结束。
3. **Daemon 退出后，确认已知 Harness 清理和在途外部操作状态，再停止 Core。** 核对各 Daemon 对应的已知执行进程、辅助进程和残留进程，检查清理日志；逐项确认在途外部操作已完成、已停止或已由操作员协调到不会继续写入的状态，记录需人工处置的 Claim 和外部结果。Codex Adapter 的 Stop 会先发 SIGINT，未在两秒内确认退出则尝试杀根进程组并标记 lost；正常退出后也会清理根进程组的辅助进程，但不保证清理脱离该进程组的进程。Daemon 崩溃会丢失内存运行注册表，Core 后续回收未续租 Claim，不代表外部操作或残留执行已停止。无法确认清理或副作用状态时，暂停备份流程并人工处置，不能依赖租约到期推定安全。
4. 确认上述条件满足，其他写入客户端也已停止，并防止 Core 自动重启后，才向实际 Core 发送 SIGTERM 或 SIGINT。例如在 Core 所在主机执行：

   ```sh
   core_pid=23456  # 替换为已核对的 Core PID
   kill -TERM "$core_pid" || exit 1
   while kill -0 "$core_pid" 2>/dev/null; do sleep 1; done
   ```

   检查原 Core 进程确已退出、退出状态及受控日志。源码给予 HTTP Shutdown 10 秒期限，退出时关闭数据库连接池并停止后台服务。不要把超时、失联或强杀当成干净停机；异常时先调查并保留现场。
5. 确认没有其他 Core 实例或工具访问同一数据库、没有进程写 Artifact store，且整个备份窗口保持无写入，再执行第 4 节。异常退出先保留原文件集合，完成故障诊断与隔离校验，勿删除 WAL。

## 4. 停服备份：数据库与内容作为一个恢复集

推荐把专用 data 目录只用于数据库文件集合和匹配的 Artifact store，停服后复制整个目录。数据库集合包括主文件及当时存在的 `kairos.db-wal`、`kairos.db-shm` 和其他关联 sidecar（如 journal）。即使某次干净退出后 sidecar 不存在，也不能把只复制主文件作为通用流程；不手工删除 sidecar、不在备份前假定 checkpoint 已完成。不要边运行边复制这些文件。

以下 Python 示例在操作员已确认停服后执行；目标必须是尚不存在的新目录。复制前以不跟随链接的遍历拒绝符号链接、FIFO、socket、设备等非普通文件/目录；复制后再次扫描源和副本，并比较包含目录、文件大小、权限和 SHA-256 的完整清单。若数据库与 store 分开部署，则在无写入窗口把两者完整复制到同一带编号的恢复集，并记录原路径映射。

```sh
python3 - <<'PY'
from pathlib import Path
import hashlib
import json
import os
import shutil
import stat

source = Path('/srv/kairos/data')
backup = Path('/srv/kairos-backups/set-YYYYMMDD-HHMMSS/data')
os.umask(0o077)

def inventory(root):
    result = {}
    def visit(path):
        info = path.lstat()  # 不跟随链接；包括根目录本身
        name = path.relative_to(root).as_posix()
        mode = stat.S_IMODE(info.st_mode)
        if stat.S_ISDIR(info.st_mode):
            if mode != 0o700:
                raise SystemExit('目录权限须为 0700：' + name)
            result[name] = {'kind': 'directory', 'mode': mode}
            with os.scandir(path) as entries:
                children = sorted(entry.name for entry in entries)
            for child in children:
                visit(path / child)
        elif stat.S_ISREG(info.st_mode):
            if mode != 0o600:
                raise SystemExit('文件权限须为 0600：' + name)
            digest = hashlib.sha256()
            with path.open('rb') as stream:
                for block in iter(lambda: stream.read(1024 * 1024), b''):
                    digest.update(block)
            result[name] = {'kind': 'file', 'mode': mode,
                            'size': info.st_size, 'sha256': digest.hexdigest()}
        else:
            raise SystemExit('拒绝符号链接或特殊文件：' + name)
    if not stat.S_ISDIR(root.lstat().st_mode):
        raise SystemExit('源或副本根必须是普通目录')
    visit(root)
    return result

# 操作员须先确认整个源目录无写入，且检查、复制和复核期间持续无写入。
# 源和备份父路径须由操作员核对并控制，不能让其他用户替换目录或链接。
before = inventory(source)
backup.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
if os.path.lexists(backup):  # 包括悬空链接
    raise SystemExit('拒绝覆盖已有备份')
shutil.copytree(source, backup, symlinks=True)  # 显式不递归跟随链接
if inventory(source) != before or inventory(backup) != before:
    raise SystemExit('源已变化或副本不匹配；该恢复集不得使用')
manifest = backup.parent / 'manifest.json'
with manifest.open('x', encoding='utf-8') as stream:
    json.dump(before, stream, ensure_ascii=False, indent=2)
    stream.write('\n')
print('复制及清单校验完成；仍需记录版本、停机确认和配置秘密引用')
PY
```

上述检查依赖源目录在整个窗口内保持静止及目录访问受控；它不是对抗并发恶意替换的文件系统快照。任何检查、复制或清单写入失败，都把目标视为不完整恢复集，保留现场排查后使用新的目标重做，不能仅凭目录存在宣告成功。隔离恢复时也采用同样的拒绝链接和复制后复核策略。

运行用户必须有读取源和写入备份权限；备份父目录及副本限权，检查目录 0700、数据库和内容文件 0600，不递归跟随未知链接。核对复制前后文件清单、大小和 SHA-256，写入私有清单并保留二进制版本、备份时间、停机确认及配置秘密引用。检查复制完整后再恢复原服务；维护期间持续保持无写入才能取得匹配的一组数据。

Artifact 元数据和托管上传恢复状态在数据库，`kairos://` 托管字节在配置的 local store；缺任一部分都不是完整恢复。不要手工重命名 blob 或重建 Artifact ID。已提交 Artifact 不由未提交生命周期 GC 删除；未提交产物、pending 上传和幂等重放记录会按 retention 回收（默认 retention 24h，GC 间隔 15m），因此不能依赖暂存产物长期保留。

外部 URI 只登记引用，复制数据库和 local store 不会备份外部服务中的内容、权限或保留策略。另行安排外部持久存储快照和访问恢复，并将其版本与恢复集关联；不把 Executor/Admin Token 发给外部 Artifact 主机。

## 5. 隔离恢复与验收

始终先恢复到全新的私有目录；不得覆盖运行中的数据库或 store。保持原恢复集只读，校验清单后从其复制出工作副本，保留完整数据库文件集合和同批 store。使用备份时相同二进制，明确设置新 SQLite 路径、新 Artifact 路径、空 PostgreSQL DSN 和独立 loopback 端口。禁止接入生产 Daemon、自动化或外部副作用，防止恢复的业务状态重新触发工作。

启动前可在恢复工作副本上执行（此检查可能操作 SQLite sidecar，因此不针对唯一备份原件）：

```sh
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/srv/kairos-restore/set-YYYYMMDD/data/kairos.db') as db:
    result = db.execute('PRAGMA quick_check').fetchall()
if result != [('ok',)]:
    raise SystemExit('SQLite quick_check 未通过')
print('SQLite quick_check: ok')
PY
```

然后启动隔离 Core 并依次确认健康、认证模式、session 身份、已知 WorkItem 状态和 result、Submission/Review 关联及精确 Artifact ID。使用授权客户端从 `GET /api/v1/artifacts/{id}/content` 下载已提交托管内容，比较备份前记录的字节和 SHA-256。不能只比较同名 Artifact，也不能把 healthz 或 quick_check 单项成功当成恢复完成。

验证普通部署时保持原 authenticated 模式并从安全秘密设施注入有效凭据，另测权限边界；若专门使用 trusted 隔离 fixture，它只能证明该 fixture 的数据恢复，不证明生产认证恢复。维护期间 Claim 可能到期并被 reaper 回收，启动后后台 GC 也会工作，应按业务状态检查，不能要求过期执行自动继续。

所有检查通过后停止隔离 Core、记录结果、人工决定切换。实际切换前再次停服和排除写入，保留当前现场的独立恢复集，按批准方案恢复匹配数据与内容、秘密配置和同版本程序；验收后逐步开放入口、Daemon。失败时停机并按原现场恢复集回退，不能混搭两批数据库和 store。跨版本升级要另行审核 migration 和回退策略，不属于本指南已验证范围。

仓库的独立 fixture 可在获授权的非生产环境运行：

```sh
python3 examples/review-loop/verify-sqlite-restore.py \
  --server /opt/kairos/kairos-server \
  --output /private/restore-evidence.json
```

脚本创建临时 trusted、loopback SQLite 实例，生成并提交 fixture Artifact、完成 WorkItem，停服复制整个 source 目录，再恢复到新目录；检查 quick_check、WorkItem 状态/result、Artifact ID 和下载字节，最后停止服务并清理临时目录。它不备份实际生产数据，也不测 authenticated Token 恢复；输出目录应提前建立并限权。本次没有重新运行该命令。

## 6. 已提供的证据及限制

WorkItem 提供的实际隔离演练结果（不是本次编写者的实测）：

| 项目 | 提供的结果 |
| --- | --- |
| 平台和范围 | Darwin，isolated stopped-Core SQLite and local managed Artifact store |
| SQLite 检查 | ok（脚本使用 PRAGMA quick_check，并非 full integrity_check） |
| WorkItem | completed |
| Artifact ID | preserved = true |
| 数据库与 store | restored together = true |
| 下载内容 SHA-256 | `f4c971f17be63da606673faba8291f31e01796eafe765b2840365e3a2e9056b6` |

证据支持该 fixture 的同组停服复制和隔离恢复。未验证 PostgreSQL、在线备份、systemd、Kubernetes、跨版本升级；发行包支持的平台列表也不等于这些恢复场景均通过。生产认证恢复、实际外部 URI 可用性、workspace/Provider 登录恢复和真实规模下的恢复时间亦无此演练证明。它们需要单独计划和测量，但不影响交付这份明确限定范围的指南。

## 7. 来源索引

- `docs/api-reference.zh-CN.md`：启动、权限、认证、Artifact 与 API 边界。
- `cmd/kairos-server/main.go`：配置默认值、后端优先级、启动顺序及信号停机。
- `internal/repository/repository.go`：SQLite WAL、文件权限、连接验证、migration 和 Close。
- `internal/artifactstore/local.go`：local store 路径检查、权限与托管字节。
- `internal/daemon/codexadapter/README.md`：登录/环境/workspace 隔离、探测限制、进程清理。
- `docs/releasing.zh-CN.md`：发行验证、checksum、Attestation 与 migration 发布要求。
- `examples/review-loop/verify-sqlite-restore.py` 及本 WorkItem 提供的 JSON：fixture 方法和上述限定实测结果。
