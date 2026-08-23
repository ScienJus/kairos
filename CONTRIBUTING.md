# Contributing to Kairos

[简体中文](CONTRIBUTING.zh-CN.md)

Thanks for helping improve Kairos. Contributions to the runtime, APIs, console, integrations, tests, and documentation are welcome.

## Before you start

- Search existing Issues and pull requests before opening a duplicate.
- For a focused bug fix, a pull request can be the first discussion.
- For a substantial feature, schema change, new dependency, or public API change, open an Issue first so the behavior and scope can be agreed on.
- Do not open a public Issue for a suspected vulnerability. Follow [SECURITY.md](SECURITY.md) instead.

## Development setup

Prerequisites:

- Go 1.26.6 or later;
- Node.js 20 LTS or a later LTS release and npm;
- curl for the quickstart.

Install frontend dependencies and run all tests:

```bash
cd web && npm ci && cd ..
make test
```

Run the isolated agent collaboration example with:

```bash
make quickstart
```

## Making changes

- Keep changes focused on one problem and preserve existing package boundaries.
- Add unit tests for behavior changes and important edge cases.
- Encode empty JSON collections as `[]`, not `null`, unless absence has a distinct documented meaning. Update the corresponding frontend type and regression test when a response shape changes.
- Store fields used for filtering, ordering, uniqueness, ownership, lifecycle decisions, authorization, or constraints in typed database columns rather than querying JSON payloads.
- Add migrations instead of rewriting migrations that may already have been applied.
- Update English and Chinese documentation when behavior, API routes, MCP tools, configuration, schema semantics, UI capabilities, or implementation status changes.

## Verification

Before submitting a pull request, run the checks relevant to the change. Backend or shared-contract changes require the complete Go suite; frontend changes require all frontend checks.

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

The pull request should explain the behavior change, identify documentation or migration effects, and list the checks that were run. Maintainers may ask for a smaller change when a pull request combines unrelated work.

## Commit and review expectations

- Use clear, imperative commit subjects.
- Do not include generated frontend output, local databases, Artifact content, tokens, or editor files.
- Keep compatibility explicit. Before `v1.0.0`, breaking changes are possible, but they must be documented and called out in release notes.
- Contributions are accepted under the [Apache License 2.0](LICENSE).
