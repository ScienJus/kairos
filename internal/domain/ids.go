package domain

// WorkItemID identifies one concrete work item.
type WorkItemID string

// DefinitionID identifies a versioned coordination definition.
type DefinitionID string

// WorkflowTaskID identifies a task node in a workflow definition.
type WorkflowTaskID string

// WorkflowRelationID identifies a directed relation in a workflow definition.
type WorkflowRelationID string

// WorkflowChoiceGroupID identifies a derived group of mutually exclusive relations.
type WorkflowChoiceGroupID string

// WorkflowCorrelationID keeps parallel and repeated Workflow expansions separate.
type WorkflowCorrelationID string

// WorkflowTaskActivationID identifies one pending or resolved Workflow node activation.
type WorkflowTaskActivationID string

// TaskID identifies one concrete task execution.
type TaskID string

// ClaimID identifies one execution responsibility period.
type ClaimID string

// SubmissionID identifies one immutable Task result submission.
type SubmissionID string

// ArtifactID identifies one immutable deliverable created during a Claim.
type ArtifactID string

// ReviewID identifies one review round.
type ReviewID string

// TaskFailureID identifies one immutable Task failure report.
type TaskFailureID string

// TransitionDecisionID identifies one Workflow progression decision.
type TransitionDecisionID string

// WorkItemEventID identifies one event in a WorkItem history.
type WorkItemEventID string

// ActorID identifies a human or agent participating in work.
type ActorID string
