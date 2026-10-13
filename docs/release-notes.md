# Release notes

[简体中文](release-notes.zh-CN.md)

## Unreleased

### Breaking execution-context change

HTTP Task Context and MCP `get_task_context` replace the flat `artifacts` field with `staged_artifacts`, `input_artifacts`, and `shared_artifacts`. Update clients and Agent instructions together with the server; the updated console and repository Skill already use these fields. Input/shared groups carry exact Task/Submission provenance and Review status. See the [API reference](api-reference.md#artifacts).

This change assumes no formally released API version. It requires no database migration: stored Artifacts, Submissions, and Workflow input receipts are unchanged. WorkItem Context, artifact history listing, and viewer Task Detail retain their `artifacts` collections.

WorkItem artifact history is collapsed as a whole by default to keep work maps and acceptance controls accessible; source titles containing paths wrap on narrow screens. The console groups Workflow artifact history by node → execution instance → Submission, and Blackboard history by independent Task → Submission. Task Detail directly displays submission history. Review status and feedback remain visible, with older executions and submissions collapsed.
