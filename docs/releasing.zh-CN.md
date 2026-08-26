# 发布 Kairos

[English](releasing.md)

本清单将仓库准备与不可轻易撤回的公开操作分开。只有在干净环境中验证发布产物和安装路径后，发布才算完成。

## 仓库一次性设置

- 配置 GitHub Description、Homepage 和 Topics。
- 在 Security 页面启用 Private Vulnerability Reporting，确保 [SECURITY.md](../SECURITY.zh-CN.md)中的私密报告渠道可用。
- 保护 `main` Branch；要求 CI workflow 通过，并在有更多维护者后要求至少一次 Review 通过。
- 限制 Actions 权限。Release 的 Build Job 只读；独立的构建后 Job 才获得 OIDC 和 Attestation 权限来证明最终产物；只有不安装依赖的 Publish Job 获得 `contents: write` 来创建 GitHub Release。

建议的仓库元数据：

- Description：`Durable coordination for work shared by people and AI agents.`
- Topics：`ai-agents`、`mcp`、`workflow`、`multi-agent`、`golang`、`coordination`

## 验证发布候选版本

1. 确认 README 的实现状态与 Roadmap 仍然符合代码现状。
2. 搜索中英文文档中的旧工具数量、生命周期状态、配置和计划功能。
3. 执行完整仓库检查：

   ```bash
   make test
   make go-vet
   git diff --check
   ```

4. 在干净 Checkout 中运行 quickstart，并通过 MCP 完成至少一个 Task。
5. 安装 GoReleaser 2.17.1 后构建本地 Release Snapshot。使用与 Release workflow 相同的禁用 Lifecycle Script 的依赖安装方式，并在打包前生成 Notices：

   ```bash
   cd web
   npm ci --ignore-scripts
   npm run build
   cd ..
   make release-notices
   goreleaser release --snapshot --clean --skip=publish
   ```

6. 解压一个 Archive，确认 `kairos-server --version` 输出预期版本、控制台可以加载、包含 `THIRD_PARTY_NOTICES.txt`，并且 `checksums.txt` 与 Archive 匹配。对于 Release workflow 产物，还需确认经过校验的合并 SBOM 已列入 `checksums.txt`。
7. 检查完整 Git 历史中的凭据，并确认作者姓名和邮箱适合公开。

## 公开发布

公开发布会改变外部状态。只有 Release Candidate Commit 合并，且 CI 和 Security workflow 全部通过后，才执行以下步骤。

1. 将仓库改为 Public，确认 License、贡献指南、安全策略、Issues 和 Actions 均可见。
2. 创建并推送带说明的语义化版本 Tag：

   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

3. Release workflow 会在不持有 Write Token 的情况下构建 macOS 与 Linux 的 amd64、arm64 Archive，打包四个构建目标实际链接的 Go Module 并集与 npm 生产依赖 Notices，将已构建服务端和控制台的依赖清单合并为一个经过校验的 CycloneDX SBOM。独立的构建后 Job 随后为最终 Archive、SBOM 和 Checksum 创建 Attestation，再将产物交给最小 `contents: write` Publish Job。使用 Release 时应通过 GitHub Attestation 工具验证 Archive 来源。
4. 对外发布前检查自动生成的 Release Notes。破坏性 API 或 Schema 变更及其 Migration 要求必须明确说明。
5. 从 GitHub 下载一个发布产物到干净环境，重新验证版本输出、启动、健康检查和 Checksum。

不得移动或替换已经发布的 Tag。错误发布应使用新的 Patch 版本修正。如果 Migration 缺陷可能破坏持久化数据，应先明确标记受影响版本并发布恢复指南，再发布替代版本。
