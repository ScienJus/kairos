# Artifact Model and Store

## Purpose

A Result explains what an executor accomplished. An Artifact identifies what the executor actually delivered. Git commits, branches, documents, reports, archives, and managed files remain addressable after the executor session ends and can be consumed by every Task in the same WorkItem.

## Delivery contract

A Workflow Task Definition may declare named Artifacts:

```json
{
  "artifacts": [
    { "name": "commit", "description": "Provide the immutable Git commit containing the implementation and tests." },
    { "name": "branch", "description": "Provide the remote integration branch containing that commit." }
  ]
}
```

`name` is both the stable contract key and the displayed name. `description` guides the executor. Kairos does not define media types, file kinds, count ranges, or Store policies in the contract. Every declared Workflow name is required once; extra Artifacts are allowed. Blackboard relies on its Task prompt and has no structured Artifact contract.

## Lifecycle

An executor creates an Artifact only while owning an active Claim. An external Artifact records an absolute URI. A managed Artifact uploads content to the configured Store through HTTP multipart or the MCP `upload_artifact` Base64 transport. Before writing managed content to the Store, Kairos persists an operation-keyed pending upload with its stable managed URI. The Store streams the bytes and returns the digest and size; Kairos records those values in the pending state before one database transaction records Blob metadata, creates the staged Artifact, and marks the upload completed. A pending retry rewrites that URI and checks the recorded digest and size. A failed Store write leaves pending state that identifies the file for GC. Both external and managed Artifacts remain staged until `submit_task` supplies their IDs. Submission validates ownership and Workflow requirements, creates the immutable Submission, binds the Artifacts, and ends the Claim in one transaction.

Staged Artifacts are available only to their creating Claim. Submitted Artifacts are visible throughout the WorkItem, including Artifacts retained under rejected Submission history. Context responses expose Artifact manifests, not file content.

An active Claim protects all of its staged Artifacts regardless of age. After the Claim ends, an unsubmitted Artifact is eligible for background garbage collection once its age exceeds the configured retention period; an Artifact already older than that period may therefore be collected on the next pass. Submitted Artifacts are never collected by this lifecycle. GC removes managed Blob content only after no Artifact references its URI; stale pending uploads are removed by their registered URI.

Pending managed-upload records and completed replay records for both external registration and managed upload use the same retention window. Within that window, a completed record can replay the staged Artifact result after a lost response; after it expires, the current Artifact and Claim state governs any later request.

## Storage

The Artifact row contains provenance, name, and URI. Managed Blob metadata contains URI, digest, and size. The built-in Store writes to a stable upload URI registered before the write and flushes the file and its directory chain before the database marks the upload completed. The digest is retained as integrity metadata and is not used to choose the URI. Duplicate content may occupy separate managed locations.

Agents cannot select a Store. The server uses one configured managed Store for this deployment. The bundled implementation supports `kairos://`; large external deliverables should use `create_artifact` with their durable URI.

Deployments configure the Artifact upload limit shared by HTTP and MCP, staged Artifact retention, and GC interval. The bundled defaults are 16 MiB, 24 hours, and 15 minutes respectively. Bundled managed uploads intentionally target small files; large content belongs in durable external storage such as S3 and is registered as an external Artifact URI. MCP Base64 transport expands content by roughly one third and buffers the request in memory.
