package application

import (
	"context"
	"io"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

const DefaultClaimLease = 5 * time.Minute
const MinClaimLease = 15 * time.Second
const MaxClaimLease = 30 * time.Minute

func normalizeClaimLease(requested int64, fallback time.Duration) time.Duration {
	if requested > 0 {
		minSeconds := int64(MinClaimLease / time.Second)
		maxSeconds := int64(MaxClaimLease / time.Second)
		if requested < minSeconds {
			return MinClaimLease
		}
		if requested > maxSeconds {
			return MaxClaimLease
		}
		return time.Duration(requested) * time.Second
	}
	d := fallback
	if d < MinClaimLease {
		return MinClaimLease
	}
	if d > MaxClaimLease {
		return MaxClaimLease
	}
	return d
}

// Clock supplies deterministic timestamps to application operations.
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies opaque identifiers to application operations.
type IDGenerator interface {
	NewID() string
}

// ArtifactContentStore persists and resolves managed Artifact content.
type ArtifactContentStore interface {
	Scheme() string
	// UploadURI returns the final managed location registered before bytes are written.
	UploadURI(key string) (string, error)
	Put(context.Context, string, io.Reader) (domain.ArtifactBlob, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// WorkCandidateKind distinguishes execution, planning, completion, and acceptance opportunities.
type WorkCandidateKind string

const (
	WorkCandidateTask                 WorkCandidateKind = "task"
	WorkCandidateEmptyBlackboard      WorkCandidateKind = "empty_blackboard"
	WorkCandidateBlackboardCompletion WorkCandidateKind = "blackboard_completion"
	WorkCandidateWorkItemAcceptance   WorkCandidateKind = "work_item_acceptance"
)

// WorkCandidate combines a discoverable opportunity with its WorkItem.
type WorkCandidate struct {
	Kind       WorkCandidateKind
	WorkItem   domain.WorkItem
	Task       *domain.Task
	Definition DefinitionExecutionContext
}

// IdempotencyRecord stores the durable progress or result of one actor mutation.
type IdempotencyStatus string

const (
	IdempotencyPending   IdempotencyStatus = "pending"
	IdempotencyCompleted IdempotencyStatus = "completed"
)

type IdempotencyRecord struct {
	Actor       domain.ActorRef
	OperationID string
	Operation   string
	Status      IdempotencyStatus
	RequestHash string
	Response    string
	CreatedAt   time.Time
}

// ReadStore exposes the state required by application operations.
type ReadStore interface {
	GetWorkItem(domain.WorkItemID) (domain.WorkItem, error)
	ListWorkItems() ([]domain.WorkItem, error)
	GetTask(domain.TaskID) (domain.Task, error)
	ListTasks(domain.WorkItemID) ([]domain.Task, error)
	ListTaskRelations(domain.WorkItemID) ([]domain.TaskRelation, error)
	ListClaims(domain.TaskID) ([]domain.Claim, error)
	GetArtifact(domain.ArtifactID) (domain.Artifact, error)
	ListArtifacts(domain.WorkItemID) ([]domain.Artifact, error)
	GetArtifactBlob(string) (domain.ArtifactBlob, error)
	ListArtifactGarbage(time.Time) ([]domain.Artifact, error)
	ListUnreferencedArtifactBlobs(time.Time) ([]domain.ArtifactBlob, error)
	ArtifactBlobReferenced(string) (bool, error)
	GetWorkflowTaskActivation(domain.WorkflowTaskActivationID) (domain.WorkflowTaskActivation, error)
	ListWorkflowTaskActivations(domain.WorkItemID) ([]domain.WorkflowTaskActivation, error)
	ListOpenTasks() ([]WorkCandidate, error)
	ListEmptyBlackboards() ([]domain.WorkItem, error)
	ListBlackboardsAwaitingLifecycleDecision() ([]domain.WorkItem, error)
	ListReapableAgentClaimTasks(time.Time) ([]domain.TaskID, error)

	GetDefinitionMetadata([]domain.DefinitionBinding) (map[domain.DefinitionBinding]domain.DefinitionMetadata, error)
	GetWorkflowDefinition(domain.DefinitionID, int64) (domain.WorkflowDefinition, error)
	GetBlackboardDefinition(domain.DefinitionID, int64) (domain.BlackboardDefinition, error)
	ListWorkflowDefinitions() ([]domain.WorkflowDefinition, error)
	ListBlackboardDefinitions() ([]domain.BlackboardDefinition, error)

	LastWorkItemEventSequence(domain.WorkItemID) (int64, error)
	GetIdempotencyRecord(domain.ActorRef, string) (IdempotencyRecord, error)
	ListPendingIdempotencyRecords(time.Time) ([]IdempotencyRecord, error)
}

// WriteStore exposes mutations performed inside one repository transaction.
type WriteStore interface {
	ReadStore
	CreateWorkflowDefinition(domain.WorkflowDefinition) error
	CreateBlackboardDefinition(domain.BlackboardDefinition) error

	CreateWorkItem(domain.WorkItem) error
	SaveWorkItem(domain.WorkItem) error

	CreateTask(domain.Task) error
	SaveTask(domain.Task) error
	CreateTaskRelation(domain.TaskRelation) error
	CreateWorkflowTaskActivation(domain.WorkflowTaskActivation) error
	SaveWorkflowTaskActivation(domain.WorkflowTaskActivation) error

	CreateClaim(domain.Claim) error
	SaveClaim(domain.Claim) error
	CreateArtifact(domain.Artifact) error
	SaveArtifact(domain.Artifact) error
	DeleteArtifact(domain.ArtifactID) error
	CreateArtifactBlob(domain.ArtifactBlob) error
	DeleteArtifactBlob(string) error

	AppendWorkItemEvent(domain.WorkItemEvent) error
	LockIdempotencyKey(domain.ActorRef, string) error
	CreateIdempotencyRecord(IdempotencyRecord) error
	SaveIdempotencyRecord(IdempotencyRecord) error
	DeleteIdempotencyRecord(domain.ActorRef, string) error
}

// Repository provides consistent reads and atomic updates. Update implementations
// must prevent concurrent writes from violating version and Claim invariants.
type Repository interface {
	View(context.Context, func(ReadStore) error) error
	Update(context.Context, func(WriteStore) error) error
}
