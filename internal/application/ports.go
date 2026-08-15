package application

import (
	"context"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// Clock supplies deterministic timestamps to application operations.
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies opaque identifiers to application operations.
type IDGenerator interface {
	NewID() string
}

// WorkCandidateKind distinguishes executable Tasks from empty Blackboards that need planning.
type WorkCandidateKind string

const (
	WorkCandidateTask            WorkCandidateKind = "task"
	WorkCandidateEmptyBlackboard WorkCandidateKind = "empty_blackboard"
)

// WorkCandidate combines a discoverable opportunity with its WorkItem.
type WorkCandidate struct {
	Kind       WorkCandidateKind
	WorkItem   domain.WorkItem
	Task       domain.Task
	Definition DefinitionExecutionContext
}

// IdempotencyRecord stores the durable result of one actor mutation.
type IdempotencyRecord struct {
	Actor       domain.ActorRef
	OperationID string
	Operation   string
	RequestHash string
	Response    string
	CreatedAt   time.Time
}

// ReadStore exposes the state required by application operations.
type ReadStore interface {
	GetWorkItem(domain.WorkItemID) (domain.WorkItem, error)
	GetTask(domain.TaskID) (domain.Task, error)
	ListTasks(domain.WorkItemID) ([]domain.Task, error)
	ListTaskRelations(domain.WorkItemID) ([]domain.TaskRelation, error)
	ListClaims(domain.TaskID) ([]domain.Claim, error)
	GetWorkflowTaskActivation(domain.WorkflowTaskActivationID) (domain.WorkflowTaskActivation, error)
	ListWorkflowTaskActivations(domain.WorkItemID) ([]domain.WorkflowTaskActivation, error)
	ListOpenTasks() ([]WorkCandidate, error)
	ListEmptyBlackboards() ([]domain.WorkItem, error)

	GetWorkflowDefinition(domain.DefinitionID, int64) (domain.WorkflowDefinition, error)
	GetBlackboardDefinition(domain.DefinitionID, int64) (domain.BlackboardDefinition, error)

	LastWorkItemEventSequence(domain.WorkItemID) (int64, error)
	GetIdempotencyRecord(domain.ActorRef, string) (IdempotencyRecord, error)
}

// WriteStore exposes mutations performed inside one repository transaction.
type WriteStore interface {
	ReadStore

	CreateWorkItem(domain.WorkItem) error
	SaveWorkItem(domain.WorkItem) error

	CreateTask(domain.Task) error
	SaveTask(domain.Task) error
	CreateTaskRelation(domain.TaskRelation) error
	CreateWorkflowTaskActivation(domain.WorkflowTaskActivation) error
	SaveWorkflowTaskActivation(domain.WorkflowTaskActivation) error

	CreateClaim(domain.Claim) error
	SaveClaim(domain.Claim) error

	AppendWorkItemEvent(domain.WorkItemEvent) error
	LockIdempotencyKey(domain.ActorRef, string) error
	CreateIdempotencyRecord(IdempotencyRecord) error
}

// Repository provides consistent reads and atomic updates. Update implementations
// must prevent concurrent writes from violating version and Claim invariants.
type Repository interface {
	View(context.Context, func(ReadStore) error) error
	Update(context.Context, func(WriteStore) error) error
}
