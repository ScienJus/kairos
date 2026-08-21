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

An executor creates an Artifact only while owning an active Claim. An external Artifact records an absolute URI. A managed Artifact uploads content to the configured Store. Both remain staged until `submit_task` supplies their IDs. Submission validates ownership and Workflow requirements, creates the immutable Submission, binds the Artifacts, and ends the Claim in one transaction.

Staged Artifacts are available only to their creating Claim. Submitted Artifacts are visible throughout the WorkItem, including Artifacts retained under rejected Submission history. Context responses expose Artifact manifests, not file content.

## Storage

The Artifact row contains provenance, name, and URI. Managed Blob metadata contains URI, digest, and size. The built-in Store streams uploads into content-addressed files and emits `kairos://blobs/sha256/...` URIs. Duplicate content reuses the same physical Blob.

Agents cannot select a Store. The server chooses one configured write Store. Readers are registered by URI scheme, allowing a deployment to write new Artifacts to a new Store while it continues resolving older schemes during migration. The first bundled implementation supports `kairos://`; the abstraction permits later implementations such as `s3://`.
