# Kairos Artifact Model

A result such as “implementation completed” is useful, but it is not the implementation. Reviewers and later executors also need a durable way to find the commit, document, report, archive, or uploaded file that was actually produced.

Kairos calls that concrete deliverable an Artifact. A Submission explains the outcome; its Artifacts make the outcome inspectable after the original session is gone.

## Declaring the Expected Deliverable

A Workflow Task Definition may declare named Artifact requirements. The name is the stable contract key and the description tells the executor what to deliver. Every declared name must appear once in a successful Submission; additional Artifacts are allowed.

Blackboard Tasks express expected deliverables in their description rather than a structured Artifact contract.

Kairos deliberately does not encode media type, file format, count range, or storage policy in a Task Definition. Those details belong to the work instructions or deployment.

## From Creation to Submission

An executor may create an Artifact only under an active Task Claim. It can either:

- register an absolute URI for a deliverable stored elsewhere; or
- upload small content to the deployment's managed Artifact Store.

The Artifact remains staged to its creating Claim until `submit_task` includes its ID. Submission atomically validates Claim ownership and Workflow requirements, creates the immutable Submission, binds the Artifacts, and ends responsibility.

## What Later Collaborators Can See

A staged Artifact is visible only inside its creating Claim. A submitted Artifact is visible throughout the WorkItem and remains attached to its Submission even when Review rejects that result.

Context responses expose Artifact metadata, not file bytes. Managed content is downloaded through its dedicated authenticated endpoint.

Retries never overwrite submitted Artifacts. A later attempt creates new Artifacts and a new Submission, preserving the earlier evidence.

If Review rejects a report, the rejected file remains attached to that Submission. The next executor uploads a revised report and submits it separately, allowing the reviewer to compare attempts instead of seeing one mutable file whose history has disappeared.

## Artifact Storage and Retention

The deployment owns one managed Store and the upload, retention, and garbage-collection policy. Managed Artifacts use stable `kairos://` URIs plus digest and size metadata. Large deliverables should live in durable external storage and be registered by absolute URI.

An active Claim protects its staged Artifacts. After the Claim ends, old unsubmitted Artifacts and incomplete upload records may be collected. Submitted Artifacts are retained as work history.

Exact upload transports, limits, replay behavior, permissions, and defaults are defined in the [API Reference](../api-reference.md) and [OpenAPI](../openapi.yaml).

## Artifact Invariants

- Artifacts belong to one WorkItem and originate from one Task Claim.
- Staged content cannot be submitted by another Claim.
- Submission binds Artifacts immutably; Review does not erase them.
- Artifact content is not copied into ordinary context or result fields.
- Storage cleanup never changes accepted business history.
