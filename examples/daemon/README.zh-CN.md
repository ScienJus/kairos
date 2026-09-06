# Agent Daemon 示例

[English](README.md)

此示例启动独立的 Authenticated Core/SQLite，创建 Workflow 和 Blackboard WorkItem，
再启动单 slot 的 Codex Daemon。原生 MCP [quickstart](https://github.com/ScienJus/kairos/tree/main/examples/quickstart) 保持独立。

需要 Linux/macOS、curl、jq、openssl、Perl（自带 POSIX 模块）、Codex CLI 0.146.x 和专用的 Codex 登录目录。
源码构建还需要 Go/Node。发布包同时提供 `kairos-server`、`kairos-daemon`，不打包 Codex
或模型凭据。先按正常 Codex 登录流程配置专用目录，然后：

```sh
KAIROS_DAEMON_CODEX_HOME=/absolute/path/codex-auth \
KAIROS_DAEMON_MODEL=your-model make daemon-example
```

此命令会启动真实模型并消耗额度。解压发布包后，使用相同环境变量执行
`sh examples/daemon/run.sh`，无需 Make。Workflow 上传一份托管文本 Artifact；Blackboard
完成单 Task 的规划、执行、completion 和 acceptance。Harness 只使用 Executor Token，
Claim 生命周期与结果提交由 Daemon 管理。示例无需读取仓库，每个候选代次只允许一次失败
Dispatch，每个 Dispatch 只启动一次 Harness。

默认绑定 `127.0.0.1:18082`，可用 `KAIROS_DAEMON_EXAMPLE_ADDR` 修改端口；继承的
PostgreSQL DSN 会被清空。凭据不会打印或出现在命令参数中。程序打印独立的私有临时目录，
保留 Core 数据及 workspace；Core 位于独立进程组，终端 SIGINT/SIGTERM 不会绕过 launcher
的清理顺序。Ctrl-C 先停止 Daemon 再停止 Core，清理期间重复信号不会中断等待，之后可手动
检查并删除目录。

## 不调用模型的验证

```sh
make daemon-e2e
```

该命令编译并运行真实 Core/Daemon 二进制，以脚本式假 Codex 进程执行两种示例，并验证
取消、失败抑制、崩溃后 lease/reaper 回收，以及向示例整个前台进程组发送信号时的停机顺序：
Daemon 清理期间 Core 仍可用，退出后 Claim 已释放。不调用模型。普通 `make go-test` 跳过该套
显式启用的测试，CI 单独执行。`KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh`
只检查示例初始化，之后退出，不启动 Harness。

运行边界见 [Adapter 指南](../../internal/daemon/codexadapter/README.md) 和
[Scheduler 指南](../../internal/daemon/README.md)。Probe 不验证模型配额；健康恢复不清空
quarantine，候选代次变化或重启才解除。撤销 Agent Identity Token 不立即撤销 Active
Executor Token，其有效性由 Claim 生命周期控制。Stop 是尽力请求；Daemon 崩溃后 Core
只回收 Claim，不清理外部进程。不可信工作应使用独立 OS 用户或容器隔离。
