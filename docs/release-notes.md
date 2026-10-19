# Release notes

[简体中文](release-notes.zh-CN.md)

## Unreleased

### Artifact preview

Task and WorkItem artifact history can preview small managed text deliverables as Markdown or source while retaining download access. Preview uses the existing content authorization and needs no API or database migration. See the [Artifact preview boundaries](api-reference.md#artifacts).

### Workflow recovery in Human attention

Human attention now exposes `workflow_recovery` entries with failure reasons and links to WorkItem recovery controls. Existing clients must recognize the new kind; Failed Tasks previously returned as `human_task` are grouped by WorkItem. Recovery selection and removal rules are documented in the [API reference](api-reference.md#human-attention). This follows `v0.1.0-alpha.1` and requires no database migration or change to recovery commands.

Recovery discovery scans Start Over sources once, avoiding repeated full-history scans as WorkItems accumulate.

### First alpha candidate

The target is `v0.1.0-alpha.1` for early evaluation, not production use. It includes Workflow/Blackboard coordination, authenticated HTTP/MCP, the operations console, managed Artifacts, Core/Daemon binaries and isolated examples. See [installation](installation.md). This is not a release announcement; snapshots remain unpublished candidates.

The release pipeline checks packaged native execution on Linux/macOS without a model. Actual Provider/CLI combinations have narrower validation. API/schema compatibility, cross-version upgrades and production backup/recovery are not guaranteed; Daemon crash cleanup may require operator intervention. macOS packages have no Developer ID signature or notarization.

### Breaking execution-context change

HTTP Task Context and MCP `get_task_context` replace the flat `artifacts` field with `staged_artifacts`, `input_artifacts`, and `shared_artifacts`. Update clients and Agent instructions together with the server; the updated console and repository Skill already use these fields. Input/shared groups carry exact Task/Submission provenance and Review status. See the [API reference](api-reference.md#artifacts).

This change assumes no formally released API version. It requires no database migration: stored Artifacts, Submissions, and Workflow input receipts are unchanged. WorkItem Context, artifact history listing, and viewer Task Detail retain their `artifacts` collections.

WorkItem artifact history is collapsed as a whole by default to keep work maps and acceptance controls accessible; source titles containing paths wrap on narrow screens. The console groups Workflow artifact history by node → execution instance → Submission, and Blackboard history by independent Task → Submission. Task Detail directly displays submission history. Review status and feedback remain visible, with older executions and submissions collapsed.
