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

// WorkItemFailure is the last terminal failure snapshot. Recovery clears the
// snapshot; the original failure remains in the append-only event history.
type WorkItemFailure struct {
	Kind           string         `json:"kind"`
	Message        string         `json:"message"`
	WorkflowTaskID WorkflowTaskID `json:"workflow_task_id"`
	Executions     int            `json:"executions"`
	Limit          int            `json:"limit"`
}

const FailureWorkflowExecutionLimit = "workflow_execution_limit"
const FailureExecution = "execution_failure"

// WorkItem represents one concrete unit of work.
type WorkItem struct {
	// ID uniquely identifies this concrete work item. [Both]
	ID                   WorkItemID  `json:"id"`
	RestartOfWorkItemID  *WorkItemID `json:"restart_of_work_item_id"`
	RestartContext       string      `json:"restart_context"`
	RecoveryInstructions string      `json:"recovery_instructions"`

	// Definition identifies the coordination space, mode, and immutable version. [Both]
	Definition DefinitionBinding `json:"definition"`

	// Status is the current lifecycle state. [Both]
	Status WorkItemStatus `json:"status"`

	Failure *WorkItemFailure `json:"failure"`
	// WorkflowMaxTaskExecutions overrides the bound Definition for this WorkItem;
	// zero inherits its limit. Every node still has an independent counter.
	WorkflowMaxTaskExecutions int `json:"workflow_max_task_executions"`

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
	if err := validateHistoryText("restart_context", w.RestartContext); err != nil {
		return err
	}
	if err := validateHistoryText("recovery_instructions", w.RecoveryInstructions); err != nil {
		return err
	}
	if w.RestartOfWorkItemID != nil && (w.CoordinationMode() != CoordinationModeWorkflow || *w.RestartOfWorkItemID == w.ID || strings.TrimSpace(string(*w.RestartOfWorkItemID)) == "") {
		return invalid("restart_of_work_item_id", "must reference another Workflow WorkItem")
	}
	if err := w.Definition.Validate(); err != nil {
		return err
	}
	if !w.Status.Valid() {
		return invalid("status", "unsupported value %q", w.Status)
	}
	if w.WorkflowMaxTaskExecutions < 0 || w.WorkflowMaxTaskExecutions > MaxWorkflowTaskExecutions {
		return invalid("workflow_max_task_executions", "must be between 0 and %d", MaxWorkflowTaskExecutions)
	}
	if w.CoordinationMode() != CoordinationModeWorkflow && w.WorkflowMaxTaskExecutions != 0 {
		return invalid("workflow_max_task_executions", "only supported for workflows")
	}
	if w.Failure != nil {
		if w.Status != WorkItemStatusFailed {
			return invalid("failure", "requires failed status")
		}
		if err := validateHistoryText("failure.message", w.Failure.Message); err != nil {
			return err
		}
		switch w.Failure.Kind {
		case FailureWorkflowExecutionLimit:
			if w.CoordinationMode() != CoordinationModeWorkflow {
				return invalid("failure.kind", "requires workflow mode")
			}
			if w.Failure.Limit < 1 || w.Failure.Limit > MaxWorkflowTaskExecutions {
				return invalid("failure.limit", "out of range")
			}
			if w.Failure.WorkflowTaskID == "" || w.Failure.Executions < w.Failure.Limit {
				return invalid("failure", "missing node execution limit details")
			}
		case FailureExecution:
			if w.Failure.WorkflowTaskID != "" || w.Failure.Executions != 0 || w.Failure.Limit != 0 {
				return invalid("failure", "ordinary failure has no workflow limit details")
			}
		default:
			return invalid("failure.kind", "unsupported value")
		}
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
