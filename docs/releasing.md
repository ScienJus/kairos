# Releasing Kairos

[简体中文](releasing.zh-CN.md)

This checklist separates repository preparation from irreversible publication. A release is not complete until its assets and installation path have been verified from a clean environment.

## One-time repository setup

- Set the GitHub description, homepage, and topics.
- Enable Private Vulnerability Reporting on the Security page so [SECURITY.md](../SECURITY.md) has a working private channel.
- Protect `main`; require the CI workflow and at least one approving review when more maintainers join.
- Keep Actions permissions restricted. The Release build job is read-only; a separate post-build job gets OIDC and attestation permissions to prove the final assets; only the dependency-free publish job gets `contents: write` to create a GitHub Release.

Suggested GitHub metadata:

- Description: `Open-source coordination server for human and AI agent teams. Durable tasks, claims, reviews, artifacts, workflows, and blackboard planning over MCP.`
- Topics: `ai-agents`, `multi-agent`, `agent-coordination`, `agent-workflow`, `mcp`, `mcp-server`, `human-in-the-loop`, `workflow-engine`, `task-orchestration`, `codex`, `claude-code`, `self-hosted`, `golang`
- Social preview: upload `docs/assets/kairos-social-preview.png` (1280x640) in the repository's Social preview settings.

## Validate a release candidate

1. Confirm the README implementation status and Roadmap still match the code.
2. Search English and Chinese documentation for obsolete tool counts, lifecycle states, configuration, and planned features.
3. Run the complete repository checks:

   ```bash
   make test
   make go-vet
   git diff --check
   ```

4. Run the quickstart from a clean checkout and complete at least one Task through MCP.
5. Build a local release snapshot with GoReleaser 2.17.1 installed. Use the same lifecycle-script-free dependency install as the Release workflow and generate notices before packaging:

   ```bash
   cd web
   npm ci --ignore-scripts
   npm run build
   cd ..
   make release-notices
   goreleaser release --snapshot --clean --skip=publish
   ```

6. Extract one archive and verify that `kairos-server --version` reports the intended version, the console loads, `THIRD_PARTY_NOTICES.txt` is present, and `checksums.txt` matches the archive. In the Release workflow output, also verify that the validated combined SBOM is listed in `checksums.txt`.
7. Review the full Git history for credentials and confirm that author names and email addresses are suitable for publication.

## Publish

Publication changes external state. Perform these steps only after the release candidate commit is merged and CI and Security workflows are green.

1. Change repository visibility to public and verify that the License, contribution guide, security policy, Issues, and Actions are visible.
2. Create and push an annotated semantic-version tag:

   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

3. The Release workflow builds macOS and Linux archives for amd64 and arm64 without a write token, packages notices for the union of modules linked by all four targets and npm production dependencies, and merges the built server and console dependency inventories into one validated CycloneDX SBOM. A separate post-build job then attests the final archives, SBOM, and checksums before passing assets to the minimal `contents: write` publish job. Verify an archive's provenance with GitHub's attestation tooling when consuming a release.
4. Review generated release notes before announcing the release. Explicitly call out breaking API or schema changes and migration requirements.
5. Download an asset from GitHub into a clean environment and repeat the version, startup, health, and checksum checks.

Never move or replace a published tag. Correct a bad release with a new patch version. If a migration defect can damage persisted data, mark the affected release clearly and publish recovery guidance before issuing the replacement.
