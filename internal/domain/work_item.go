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
	WorkItemStatusOpen                    WorkItemStatus = "open"
	WorkItemStatusCompleted               WorkItemStatus = "completed"
	WorkItemStatusCancelled               WorkItemStatus = "cancelled"
	WorkItemStatusFailed                  WorkItemStatus = "failed"
	WorkItemStatusAwaitingAgentAcceptance WorkItemStatus = "awaiting_agent_acceptance"
	WorkItemStatusAwaitingHumanAcceptance WorkItemStatus = "awaiting_human_acceptance"
)

// Valid reports whether the work item status is recognized.
func (s WorkItemStatus) Valid() bool {
	switch s {
	case WorkItemStatusOpen, WorkItemStatusCompleted, WorkItemStatusCancelled, WorkItemStatusFailed, WorkItemStatusAwaitingAgentAcceptance, WorkItemStatusAwaitingHumanAcceptance:
		return true
	default:
		return false
	}
}

type WorkItemAcceptanceMode string

const (
	WorkItemAcceptanceNone  WorkItemAcceptanceMode = "none"
	WorkItemAcceptanceAgent WorkItemAcceptanceMode = "agent"
	WorkItemAcceptanceHuman WorkItemAcceptanceMode = "human"
)

func (m WorkItemAcceptanceMode) Valid() bool {
	return m == WorkItemAcceptanceNone || m == WorkItemAcceptanceAgent || m == WorkItemAcceptanceHuman
}

// WorkItem represents one concrete unit of work.
type WorkItem struct {
	// ID uniquely identifies this concrete work item. [Both]
	ID WorkItemID `json:"id"`

	// Definition identifies the coordination space, mode, and immutable version. [Both]
	Definition DefinitionBinding `json:"definition"`

	// Status is the current lifecycle state. [Both]
	Status WorkItemStatus `json:"status"`

	// AcceptanceMode controls what happens after a collaborator submits completion.
	AcceptanceMode WorkItemAcceptanceMode `json:"acceptance_mode"`

	// Title is the short label shown in lists and Kanban cards. [Both]
	Title string `json:"title"`

	// Goal describes the outcome this work item is expected to achieve. [Both]
	Goal string `json:"goal"`

	// Context provides background information needed to understand the work. [Both]
	Context string `json:"context"`

	// Constraints describe boundaries that execution must respect. [Both]
	Constraints string `json:"constraints"`

	// AcceptanceCriteria define how completion should be evaluated. [Both]
	AcceptanceCriteria string `json:"acceptance_criteria"`

	// Tags provide discovery metadata, including before a Blackboard has Tasks. [Both]
	Tags []string `json:"tags"`

	// Result stores a Blackboard completion proposal while acceptance is pending,
	// and its accepted final outcome after completion. It remains empty for Workflow. [Both]
	Result string `json:"result"`

	// Version is the server-maintained WorkItem revision. Blackboard planning
	// mutations increment it, while Task execution uses the individual Task
	// version. [Both]
	Version int64 `json:"version"`

	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	CancelledAt        *time.Time `json:"cancelled_at"`
	CancelledBy        *ActorRef  `json:"cancelled_by"`
	CancellationReason string     `json:"cancellation_reason"`
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
	if w.AcceptanceMode == "" {
		w.AcceptanceMode = WorkItemAcceptanceNone
	}
	if !w.AcceptanceMode.Valid() {
		return invalid("acceptance_mode", "unsupported value %q", w.AcceptanceMode)
	}
	if strings.TrimSpace(w.Title) == "" {
		return invalid("title", "is required")
	}
	if strings.TrimSpace(w.Goal) == "" {
		return invalid("goal", "is required")
	}
	if err := validateHistoryText("result", w.Result); err != nil {
		return err
	}
	if err := validateHistoryText("cancellation_reason", w.CancellationReason); err != nil {
		return err
	}
	if w.Version < 0 {
		return invalid("version", "must not be negative")
	}
	if err := validateStringSet("tags", w.Tags); err != nil {
		return err
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
	if w.Status == WorkItemStatusCancelled {
		if w.CancelledAt == nil {
			return invalid("cancelled_at", "is required for cancelled work items")
		}
		if w.CancelledBy == nil {
			return invalid("cancelled_by", "is required for cancelled work items")
		}
		if err := w.CancelledBy.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(w.CancellationReason) == "" {
			return invalid("cancellation_reason", "is required for cancelled work items")
		}
	} else if w.CancelledAt != nil || w.CancelledBy != nil || w.CancellationReason != "" {
		return invalid("cancellation", "metadata must be empty unless the work item is cancelled")
	}

	return nil
}

// CoordinationMode returns the mode inherited from the bound definition.
func (w WorkItem) CoordinationMode() CoordinationMode {
	return w.Definition.Mode
}
