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
	Kind       WorkCandidateKind          `json:"kind"`
	WorkItem   domain.WorkItem            `json:"work_item"`
	Task       *domain.Task               `json:"task"`
	Definition DefinitionExecutionContext `json:"definition"`
}

// IdempotencyStatus tracks a replay record; only managed upload uses pending.
type IdempotencyStatus string

const (
	IdempotencyPending   IdempotencyStatus = "pending"
	IdempotencyCompleted IdempotencyStatus = "completed"
)

// IdempotencyRecord stores upload recovery state or a replayable creation result.
type IdempotencyRecord struct {
	Actor       domain.ActorRef
	OperationID string
	Operation   string
	Status      IdempotencyStatus
	RequestHash string
	Response    string
	CreatedAt   time.Time
}

// WorkItemFilter contains fields that are evaluated by the persistence layer.
type WorkItemFilter struct {
	Statuses []domain.WorkItemStatus
	Modes    []domain.CoordinationMode
	Tags     []string
	Page     PageRequest[WorkItemCursor]
}

// PageRequest describes a bounded keyset page. A zero Limit is reserved for
// internal aggregate reads that intentionally need the complete collection.
type PageRequest[T any] struct {
	Limit int
	After *T
}

const MaxPageLimit = 200

type Page[T any] struct {
	Items   []T
	HasMore bool
}

type WorkItemCursor struct {
	UpdatedAt time.Time
	ID        domain.WorkItemID
}

type DefinitionCatalogCursor struct {
	ID domain.DefinitionID
}

type DefinitionCatalogFilter struct {
	Page PageRequest[DefinitionCatalogCursor]
}

type DefinitionVersionCursor struct {
	Version int64
}

type DefinitionVersionFilter struct {
	ID   domain.DefinitionID
	Page PageRequest[DefinitionVersionCursor]
}

type ArtifactCursor struct {
	CreatedAt time.Time
	ID        domain.ArtifactID
}

type ArtifactFilter struct {
	WorkItemID    domain.WorkItemID
	TaskID        domain.TaskID
	SubmittedOnly bool
	Page          PageRequest[ArtifactCursor]
}

type HumanAttentionCursor struct {
	Priority   int
	UpdatedAt  time.Time
	WorkItemID domain.WorkItemID
	TaskID     domain.TaskID
}

// OpenTaskFilter narrows discovery candidates before application-level checks.
type OpenTaskFilter struct {
	ActorKind domain.ActorKind
	Role      string
	Tags      []string
}

// ReadStore exposes the state required by application operations.
type ReadStore interface {
	GetWorkItem(domain.WorkItemID) (domain.WorkItem, error)
	ListWorkItems(WorkItemFilter) ([]domain.WorkItem, error)
	GetTask(domain.TaskID) (domain.Task, error)
	ListTasks(domain.WorkItemID) ([]domain.Task, error)
	ListTaskRelations(domain.WorkItemID) ([]domain.TaskRelation, error)
	ListClaims(domain.TaskID) ([]domain.Claim, error)
	ListClaimsByWorkItem(domain.WorkItemID) ([]domain.Claim, error)
	ListHumanAttention(PageRequest[HumanAttentionCursor]) ([]HumanAttentionItem, error)
	GetArtifact(domain.ArtifactID) (domain.Artifact, error)
	ListArtifacts(ArtifactFilter) ([]domain.Artifact, error)
	GetArtifactBlob(string) (domain.ArtifactBlob, error)
	ListArtifactGarbage(time.Time) ([]domain.Artifact, error)
	ListUnreferencedArtifactBlobs(time.Time) ([]domain.ArtifactBlob, error)
	ArtifactBlobReferenced(string) (bool, error)
	GetWorkflowTaskActivation(domain.WorkflowTaskActivationID) (domain.WorkflowTaskActivation, error)
	ListWorkflowTaskActivations(domain.WorkItemID) ([]domain.WorkflowTaskActivation, error)
	ListOpenTasks(OpenTaskFilter) ([]WorkCandidate, error)
	ListEmptyBlackboards([]string) ([]domain.WorkItem, error)
	ListBlackboardsAwaitingLifecycleDecision([]string) ([]domain.WorkItem, error)
	ListReapableAgentClaimTasks(time.Time) ([]domain.TaskID, error)

	GetDefinitionMetadata([]domain.DefinitionBinding) (map[domain.DefinitionBinding]domain.DefinitionMetadata, error)
	GetWorkflowDefinition(domain.DefinitionID, int64) (domain.WorkflowDefinition, error)
	GetBlackboardDefinition(domain.DefinitionID, int64) (domain.BlackboardDefinition, error)
	GetLatestWorkflowDefinition(domain.DefinitionID) (domain.WorkflowDefinition, error)
	GetLatestBlackboardDefinition(domain.DefinitionID) (domain.BlackboardDefinition, error)
	ListWorkflowDefinitionCatalog(DefinitionCatalogFilter) ([]domain.WorkflowDefinition, error)
	ListBlackboardDefinitionCatalog(DefinitionCatalogFilter) ([]domain.BlackboardDefinition, error)
	ListWorkflowDefinitionVersions(DefinitionVersionFilter) ([]domain.WorkflowDefinition, error)
	ListBlackboardDefinitionVersions(DefinitionVersionFilter) ([]domain.BlackboardDefinition, error)

	LastWorkItemEventSequence(domain.WorkItemID) (int64, error)
	GetIdempotencyRecord(domain.ActorRef, string) (IdempotencyRecord, error)
	ListPendingIdempotencyRecords(time.Time) ([]IdempotencyRecord, error)
}

// WriteStore exposes mutations performed inside one repository transaction.
type WriteStore interface {
	ReadStore
	LockDefinitionVersion(domain.CoordinationMode, domain.DefinitionID) error
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
	SaveArtifact(domain.Artifact, time.Time) error
	DeleteArtifact(domain.ArtifactID) error
	CreateArtifactBlob(domain.ArtifactBlob) error
	DeleteArtifactBlob(string) error

	AppendWorkItemEvent(domain.WorkItemEvent) error
	LockIdempotencyKey(domain.ActorRef, string) error
	CreateIdempotencyRecord(IdempotencyRecord) error
	SaveIdempotencyRecord(IdempotencyRecord, time.Time) error
	DeleteIdempotencyRecord(domain.ActorRef, string) error
	DeleteCompletedArtifactOperationRecords(time.Time) (int, error)
}

// Repository provides consistent reads and atomic updates. Update implementations
// must prevent concurrent writes from violating version and Claim invariants.
type Repository interface {
	View(context.Context, func(ReadStore) error) error
	Update(context.Context, func(WriteStore) error) error
}
