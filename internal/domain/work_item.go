package domain

import (
	"strings"
	"time"
)

// CoordinationMode determines how a WorkItem's runtime graph is interpreted.
type CoordinationMode string

const (
	CoordinationModeWorkflow   CoordinationMode = "workflow"
	CoordinationModeBlackboard CoordinationMode = "blackboard"
)

// Valid reports whether the coordination mode is recognized.
func (m CoordinationMode) Valid() bool {
	return m == CoordinationModeWorkflow || m == CoordinationModeBlackboard
}

// WorkItemStatus is the lifecycle state of a WorkItem.
type WorkItemStatus string

const (
	WorkItemStatusOpen      WorkItemStatus = "open"
	WorkItemStatusCompleted WorkItemStatus = "completed"
	WorkItemStatusCancelled WorkItemStatus = "cancelled"
	WorkItemStatusFailed    WorkItemStatus = "failed"
)

// Valid reports whether the work item status is recognized.
func (s WorkItemStatus) Valid() bool {
	switch s {
	case WorkItemStatusOpen, WorkItemStatusCompleted, WorkItemStatusCancelled, WorkItemStatusFailed:
		return true
	default:
		return false
	}
}

// WorkItem represents one concrete unit of work.
type WorkItem struct {
	// ID uniquely identifies this concrete work item. [Both]
	ID WorkItemID

	// Definition identifies the coordination space, mode, and immutable version. [Both]
	Definition DefinitionBinding

	// Status is the current lifecycle state. [Both]
	Status WorkItemStatus

	// Title is the short label shown in lists and Kanban cards. [Both]
	Title string

	// Goal describes the outcome this work item is expected to achieve. [Both]
	Goal string

	// Context provides background information needed to understand the work. [Both]
	Context string

	// Constraints describe boundaries that execution must respect. [Both]
	Constraints string

	// AcceptanceCriteria define how completion should be evaluated. [Both]
	AcceptanceCriteria string

	// Result records the final outcome and important deliverables. [Both]
	Result string

	// Version is used for optimistic concurrency control. [Both]
	Version int64

	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
}

// Validate checks the WorkItem invariants.
func (w WorkItem) Validate() error {
	if strings.TrimSpace(string(w.ID)) == "" {
		return invalid("id", "is required")
	}
	if err := w.Definition.Validate(); err != nil {
		return err
	}
	if !w.Status.Valid() {
		return invalid("status", "unsupported value %q", w.Status)
	}
	if strings.TrimSpace(w.Title) == "" {
		return invalid("title", "is required")
	}
	if strings.TrimSpace(w.Goal) == "" {
		return invalid("goal", "is required")
	}
	if w.Version < 0 {
		return invalid("version", "must not be negative")
	}
	if err := validateTimestamps(w.CreatedAt, w.UpdatedAt); err != nil {
		return err
	}

	if w.Status == WorkItemStatusCompleted {
		if w.CompletedAt == nil {
			return invalid("completed_at", "is required for completed work items")
		}
	} else if w.CompletedAt != nil {
		return invalid("completed_at", "must be nil unless the work item is completed")
	}

	return nil
}

// CoordinationMode returns the mode inherited from the bound definition.
func (w WorkItem) CoordinationMode() CoordinationMode {
	return w.Definition.Mode
}
