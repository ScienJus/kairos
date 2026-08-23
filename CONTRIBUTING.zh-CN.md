# 为 Kairos 贡献

[English](CONTRIBUTING.md)

欢迎改进 Kairos。运行时、API、控制台、集成、测试与文档方面的贡献都可以提交。

## 开始之前

- 提交前先搜索已有 Issue 和 Pull Request，避免重复讨论。
- 对于范围明确的 Bug 修复，可以直接提交 Pull Request。
- 对于较大的功能、Schema 变更、新依赖或公共 API 变更，请先创建 Issue，以便提前确认行为和范围。
- 怀疑存在安全漏洞时，不要创建公开 Issue，请按照 [SECURITY.md](SECURITY.zh-CN.md)报告。

## 开发环境

环境要求：

- Go 1.26.6 或更高版本；
- Node.js 20 LTS 或更高的 LTS 版本及 npm；
- quickstart 需要 curl。

安装前端依赖并运行全部测试：

```bash
cd web && npm ci && cd ..
make test
```

运行隔离的 Agent 协作示例：

```bash
make quickstart
```

## 修改代码

- 每次修改聚焦一个问题，并保持现有 Package 边界。
- 行为变更需要添加单元测试，并覆盖重要边界情况。
- JSON 空集合应编码为 `[]` 而不是 `null`，除非“缺失”具有独立且明确记录的语义。响应结构变化时，同时更新前端类型和回归测试。
- 用于筛选、排序、唯一性、所有权、生命周期判断、授权或约束的字段应存放在独立类型化数据库列中，不要通过查询 JSON Payload 实现。
- 使用新 Migration，不要重写可能已经执行过的 Migration。
- 行为、API 路由、MCP 工具、配置、Schema 语义、UI 能力或实现状态变化时，同时更新中英文文档。

## 验证

提交 Pull Request 前，运行与修改相关的检查。后端或共享契约变更需要执行完整 Go 测试；前端变更需要执行全部前端检查。

```bash
gofmt -w path/to/changed.go
go test ./...
go vet ./...

cd web
npm test
npm run lint
npm run build
cd ..

git diff --check
```

Pull Request 应说明行为变化、文档或 Migration 影响，以及已经执行的检查。一个 Pull Request 包含不相关修改时，维护者可能要求拆分。

## Commit 与 Review

- Commit 标题使用清晰的祈使语气。
- 不要提交前端构建产物、本地数据库、Artifact 内容、Token 或编辑器文件。
- 明确说明兼容性。`v1.0.0` 之前可能存在破坏性变更，但必须在文档和 Release Notes 中明确指出。
- 所有贡献均按 [Apache License 2.0](LICENSE)授权。
