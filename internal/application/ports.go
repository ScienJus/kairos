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

// WorkCandidate combines a Task with the WorkItem that defines its coordination mode.
type WorkCandidate struct {
	WorkItem domain.WorkItem
	Task     domain.Task
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

	GetWorkflowDefinition(domain.DefinitionID, int64) (domain.WorkflowDefinition, error)
	GetBlackboardDefinition(domain.DefinitionID, int64) (domain.BlackboardDefinition, error)

	LastWorkItemEventSequence(domain.WorkItemID) (int64, error)
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
}

// Repository provides consistent reads and atomic updates. Update implementations
// must prevent concurrent writes from violating version and Claim invariants.
type Repository interface {
	View(context.Context, func(ReadStore) error) error
	Update(context.Context, func(WriteStore) error) error
}
