# 安全策略

[English](SECURITY.md)

## 支持版本

在 `v1.0.0` 之前，安全修复会应用到 `main` 和最新发布的 Minor 版本。更早的 `0.x` 版本可能不会获得回移修复。

## 报告漏洞

请勿通过公开 Issue、Discussion 或 Pull Request 报告疑似漏洞。请在仓库 Security 页面使用 GitHub Private Vulnerability Reporting，并提供：

- 受影响的版本或 Commit；
- 部署方式和认证模式；
- 复现步骤或最小概念验证；
- 可能造成的影响；
- 已知的缓解建议（如有）。

请勿提交仍在使用的 Token、凭据、个人数据或生产 Artifact 内容。维护者会尽力在五个工作日内确认报告，私下协调验证与修复，并在报告者未要求匿名时给予致谢。

如果 Security 页面没有私密漏洞报告入口，请不要公开漏洞细节。可以创建一个不包含细节的 Issue，请维护者启用私密报告渠道。

## 部署边界

- Trusted Mode 从 HTTP Header 接受调用者身份，仅适用于本机回环地址，或能够认证并覆盖这些 Header 的可信网络边界。默认监听地址是 `127.0.0.1:8080`。
- 同一可信协作群体内的共享或可被远程访问的部署必须使用 Authenticated Mode、高熵 Admin Token，以及分别签发的 Identity Token。
- Authenticated Mode 会认证调用方并执行具体操作所需的执行权限校验，但所有已签发身份仍属于同一个全局信任域。系统不按租户、Team、项目或对象隔离数据；互不信任的群体应分别部署 Kairos 实例。
- Authenticated Mode 下，operations console 只在浏览器 `sessionStorage` 中保存 Identity Token；退出登录或收到 `401` 时会清除 Token 和缓存的 API 数据。应保护浏览器会话，避免在共享浏览器 Profile 中使用高权限身份。
- Kairos 不负责 TLS 终止。共享部署应位于正确配置的 TLS 反向代理之后。
- SQLite/PostgreSQL 数据库、Admin Token、Identity Token 和托管 Artifact 目录都属于敏感数据。应限制文件系统和数据库访问、统一备份，并且不得提交到仓库。
- 内置托管 Artifact Store 面向小文件。接受不可信上传前，应检查保留配置和存储权限。

认证配置和限制见 [API 参考](docs/api-reference.zh-CN.md)。
