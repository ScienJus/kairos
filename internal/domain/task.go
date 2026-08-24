package domain

import (
	"strings"
	"time"
)

// TaskStatus is the execution state of a Task.
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusWorking         TaskStatus = "working"
	TaskStatusWaitingChildren TaskStatus = "waiting_children"
	TaskStatusInReview        TaskStatus = "in_review"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusSkipped         TaskStatus = "skipped"
	TaskStatusFailed          TaskStatus = "failed"
)

// Valid reports whether the task status is recognized.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusPending, TaskStatusWorking, TaskStatusWaitingChildren, TaskStatusInReview, TaskStatusCompleted, TaskStatusSkipped, TaskStatusFailed:
		return true
	default:
		return false
	}
}

// ExecutorRequirement restricts the kind of actor that may execute a Task.
type ExecutorRequirement string

const (
	ExecutorAgent  ExecutorRequirement = "agent"
	ExecutorHuman  ExecutorRequirement = "human"
	ExecutorEither ExecutorRequirement = "either"
)

// Valid reports whether the executor requirement is recognized.
func (e ExecutorRequirement) Valid() bool {
	return e == ExecutorAgent || e == ExecutorHuman || e == ExecutorEither
}

// ExecutionPolicy determines whether a Workflow Task must be executed.
type ExecutionPolicy string

const (
	ExecutionRequired ExecutionPolicy = "required"
	ExecutionOptional ExecutionPolicy = "optional"
)

// Valid reports whether the execution policy is recognized.
func (p ExecutionPolicy) Valid() bool {
	return p == ExecutionRequired || p == ExecutionOptional
}

// ReviewPolicy defines the preconfigured human-review requirement of a Workflow Task.
type ReviewPolicy string

const (
	ReviewNone            ReviewPolicy = "none"
	ReviewExecutorDecides ReviewPolicy = "executor_decides"
	ReviewRequired        ReviewPolicy = "required"
)

// Valid reports whether the review policy is recognized.
func (p ReviewPolicy) Valid() bool {
	return p == ReviewNone || p == ReviewExecutorDecides || p == ReviewRequired
}

// Task represents one executable unit or decomposed aggregation boundary.
type Task struct {
	// ID uniquely identifies this concrete task execution. [Both]
	ID TaskID `json:"id"`

	// WorkItemID identifies the parent work item. [Both]
	WorkItemID WorkItemID `json:"work_item_id"`

	// WorkflowTaskID identifies the source node in the bound workflow version.
	// It must be nil in Blackboard mode. [Workflow]
	WorkflowTaskID *WorkflowTaskID `json:"workflow_task_id"`

	// WorkflowActivationID identifies the internal activation that produced this
	// concrete Task. It must be nil in Blackboard mode. [Workflow]
	WorkflowActivationID *WorkflowTaskActivationID `json:"workflow_activation_id"`

	// Status is the current execution state. [Both]
	Status TaskStatus `json:"status"`

	// ActiveClaimID identifies the current execution responsibility.
	// It is non-nil exactly while Status is Working. [Both]
	ActiveClaimID *ClaimID `json:"active_claim_id"`

	// ParentTaskID identifies the aggregate Task that contains this Task.
	// It must be nil in Workflow mode and cannot change after creation. [Blackboard]
	ParentTaskID *TaskID `json:"parent_task_id"`

	// DecomposedAt records when execution responsibility moved to child Tasks.
	// A decomposed Task never produces its own execution result. [Blackboard]
	DecomposedAt *time.Time `json:"decomposed_at"`

	// Title is the short human-readable label. [Both]
	Title string `json:"title"`

	// Description explains what the executor should do. [Both]
	Description string `json:"description"`

	// AcceptanceCriteria define the expected result of this Task. [Both]
	AcceptanceCriteria string `json:"acceptance_criteria"`

	// Executor restricts whether a human, an agent, or either may execute the Task. [Both]
	Executor   ExecutorRequirement `json:"executor"`
	SkippedBy  *ActorRef           `json:"skipped_by"`
	SkipReason string              `json:"skip_reason"`

	// AllowedRoles formally restrict which Agent roles may execute the Task.
	// Workflow uses this as part of candidate eligibility. Blackboard usually
	// leaves it empty and relies on Tags, but a non-empty value remains a hard
	// execution constraint. Human executors are never filtered by roles. [Both]
	AllowedRoles []string `json:"allowed_roles"`

	// Tags provide searchable discovery metadata. Workflow treats them as
	// descriptive filters that do not affect graph eligibility. Blackboard uses
	// them as the primary discovery mechanism. Tags never grant permission. [Both]
	Tags []string `json:"tags"`

	// Execution determines whether this Task is required or optional.
	// It must be nil in Blackboard mode. [Workflow]
	Execution *ExecutionPolicy `json:"execution"`

	// ReviewPolicy defines the preconfigured human-review requirement.
	// It must be nil in Blackboard mode, where Review is requested dynamically. [Workflow]
	ReviewPolicy *ReviewPolicy `json:"review_policy"`

	// Reviews contains the complete Review history ordered by RequestedAt. [Both]
	Reviews []Review `json:"reviews"`

	// Submissions contains every immutable result ordered by SubmittedAt. [Both]
	Submissions []TaskSubmission `json:"submissions"`

	// Failures contains every immutable failure report ordered by FailedAt. [Both]
	// RetryPrompt values are included in later execution context.
	Failures []TaskFailure `json:"failures"`

	// TransitionDecisions contains every progression decision in decision order.
	// Rejected Review rounds retain their unapplied decisions. [Workflow]
	TransitionDecisions []TransitionDecision `json:"transition_decisions"`

	// Position controls display order and does not express dependencies. [Both]
	Position int64 `json:"position"`

	// Version is used for optimistic concurrency control. [Both]
	Version int64 `json:"version"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// Validate checks the Task invariants for the given coordination mode.
func (t Task) Validate(mode CoordinationMode) error {
	if !mode.Valid() {
		return invalid("mode", "unsupported value %q", mode)
	}
	if strings.TrimSpace(string(t.ID)) == "" {
		return invalid("id", "is required")
	}
	if strings.TrimSpace(string(t.WorkItemID)) == "" {
		return invalid("work_item_id", "is required")
	}
	if !t.Status.Valid() {
		return invalid("status", "unsupported value %q", t.Status)
	}
	if strings.TrimSpace(t.Title) == "" {
		return invalid("title", "is required")
	}
	if !t.Executor.Valid() {
		return invalid("executor", "unsupported value %q", t.Executor)
	}
	if t.Position < 0 {
		return invalid("position", "must not be negative")
	}
	if t.Version < 0 {
		return invalid("version", "must not be negative")
	}
	if err := validateTimestamps(t.CreatedAt, t.UpdatedAt); err != nil {
		return err
	}
	if err := validateStringSet("allowed_roles", t.AllowedRoles); err != nil {
		return err
	}
	if err := validateStringSet("tags", t.Tags); err != nil {
		return err
	}
	if t.Executor == ExecutorHuman && len(t.AllowedRoles) > 0 {
		return invalid("allowed_roles", "must be empty for human-only tasks")
	}

	switch mode {
	case CoordinationModeWorkflow:
		if t.ParentTaskID != nil {
			return invalid("parent_task_id", "must be nil in workflow mode")
		}
		if t.DecomposedAt != nil {
			return invalid("decomposed_at", "must be nil in workflow mode")
		}
		if t.WorkflowTaskID == nil || strings.TrimSpace(string(*t.WorkflowTaskID)) == "" {
			return invalid("workflow_task_id", "is required in workflow mode")
		}
		if t.WorkflowActivationID == nil || strings.TrimSpace(string(*t.WorkflowActivationID)) == "" {
			return invalid("workflow_activation_id", "is required in workflow mode")
		}
		if t.Execution == nil || !t.Execution.Valid() {
			return invalid("execution", "a valid policy is required in workflow mode")
		}
		if t.ReviewPolicy == nil || !t.ReviewPolicy.Valid() {
			return invalid("review_policy", "a valid policy is required in workflow mode")
		}
	case CoordinationModeBlackboard:
		if t.ParentTaskID != nil {
			if strings.TrimSpace(string(*t.ParentTaskID)) == "" {
				return invalid("parent_task_id", "must not be empty")
			}
			if *t.ParentTaskID == t.ID {
				return invalid("parent_task_id", "must not reference the task itself")
			}
		}
		if t.WorkflowTaskID != nil {
			return invalid("workflow_task_id", "must be nil in blackboard mode")
		}
		if t.WorkflowActivationID != nil {
			return invalid("workflow_activation_id", "must be nil in blackboard mode")
		}
		if t.Execution != nil {
			return invalid("execution", "must be nil in blackboard mode")
		}
		if t.ReviewPolicy != nil {
			return invalid("review_policy", "must be nil in blackboard mode")
		}
		if len(t.TransitionDecisions) > 0 {
			return invalid("transition_decisions", "must be empty in blackboard mode")
		}
	}

	if err := validateReviewHistory(t.ID, t.Reviews); err != nil {
		return err
	}
	if err := validateSubmissionHistory(t.ID, t.Submissions); err != nil {
		return err
	}
	if err := validateTaskFailureHistory(t.ID, t.Failures); err != nil {
		return err
	}
	submissions := make(map[SubmissionID]struct{}, len(t.Submissions))
	for _, submission := range t.Submissions {
		submissions[submission.ID] = struct{}{}
	}
	for _, review := range t.Reviews {
		if mode == CoordinationModeBlackboard && review.SubmissionID == nil {
			return invalid("review.submission_id", "is required for blackboard reviews")
		}
		if mode == CoordinationModeWorkflow && review.SubmissionID == nil && *t.Execution != ExecutionOptional {
			return invalid("review.submission_id", "may be nil only for an optional task skip decision")
		}
		if review.SubmissionID == nil {
			continue
		}
		if _, ok := submissions[*review.SubmissionID]; !ok {
			return invalid("review.submission_id", "submission %q does not exist in task history", *review.SubmissionID)
		}
	}
	if err := validateTransitionDecisionHistory(t, submissions); err != nil {
		return err
	}
	if t.Status == TaskStatusWorking && t.ActiveClaimID == nil {
		return invalid("active_claim_id", "is required while the task is working")
	}
	if t.Status != TaskStatusWorking && t.ActiveClaimID != nil {
		return invalid("active_claim_id", "must be nil unless the task is working")
	}
	hasPendingReview := len(t.Reviews) > 0 && t.Reviews[len(t.Reviews)-1].Status == ReviewStatusPending
	if t.Status == TaskStatusInReview && !hasPendingReview {
		return invalid("reviews", "an in-review task requires a pending review")
	}
	if t.Status != TaskStatusInReview && hasPendingReview {
		return invalid("status", "must be in_review while a review is pending")
	}
	if t.Status == TaskStatusWaitingChildren && t.DecomposedAt == nil {
		return invalid("decomposed_at", "is required while waiting for child tasks")
	}
	if t.DecomposedAt != nil {
		if t.DecomposedAt.IsZero() || t.DecomposedAt.Before(t.CreatedAt) || t.DecomposedAt.After(t.UpdatedAt) {
			return invalid("decomposed_at", "must fall within the task lifetime")
		}
		if t.Status != TaskStatusWaitingChildren && t.Status != TaskStatusCompleted {
			return invalid("status", "a decomposed task must be waiting_children or completed")
		}
		if len(t.Submissions) > 0 || len(t.Reviews) > 0 || len(t.Failures) > 0 || len(t.TransitionDecisions) > 0 {
			return invalid("decomposed_at", "a decomposed task must not contain execution results")
		}
	}

	if t.Status == TaskStatusCompleted || t.Status == TaskStatusSkipped {
		if t.CompletedAt == nil {
			return invalid("completed_at", "is required for ended tasks")
		}
		if t.Status == TaskStatusCompleted && len(t.Submissions) == 0 && t.DecomposedAt == nil {
			return invalid("submissions", "a completed task requires a submission")
		}
	} else if t.CompletedAt != nil {
		return invalid("completed_at", "must be nil unless the task is completed or skipped")
	}
	if t.Status != TaskStatusSkipped && (t.SkippedBy != nil || strings.TrimSpace(t.SkipReason) != "") {
		return invalid("skipped_by", "is only valid for skipped tasks")
	}
	if t.SkippedBy != nil {
		if err := t.SkippedBy.Validate(); err != nil {
			return err
		}
	}

	if t.Status == TaskStatusFailed {
		if len(t.Failures) == 0 || t.Failures[len(t.Failures)-1].Action != TaskFailureFailWorkItem {
			return invalid("failures", "a failed task requires a fail_work_item failure")
		}
	} else if len(t.Failures) > 0 && t.Failures[len(t.Failures)-1].Action == TaskFailureFailWorkItem {
		return invalid("status", "must be failed after a fail_work_item failure")
	}

	return nil
}

func validateTransitionDecisionHistory(task Task, submissions map[SubmissionID]struct{}) error {
	seen := make(map[TransitionDecisionID]struct{}, len(task.TransitionDecisions))
	var previous time.Time
	applied := 0

	for index, decision := range task.TransitionDecisions {
		if err := decision.Validate(); err != nil {
			return err
		}
		if decision.WorkItemID != task.WorkItemID {
			return invalid("transition_decisions.work_item_id", "does not match task work item")
		}
		if decision.SourceTaskID != task.ID {
			return invalid("transition_decisions.source_task_id", "does not match task")
		}
		if _, exists := seen[decision.ID]; exists {
			return invalid("transition_decisions", "contains duplicate decision %q", decision.ID)
		}
		seen[decision.ID] = struct{}{}
		if !previous.IsZero() && decision.DecidedAt.Before(previous) {
			return invalid("transition_decisions", "must be ordered by decided_at")
		}
		previous = decision.DecidedAt
		if decision.SourceSubmissionID != nil {
			if _, exists := submissions[*decision.SourceSubmissionID]; !exists {
				return invalid(
					"transition_decisions.source_submission_id",
					"submission %q does not exist in task history",
					*decision.SourceSubmissionID,
				)
			}
		}
		if decision.AppliedAt == nil {
			continue
		}
		applied++
		if applied > 1 {
			return invalid("transition_decisions", "contains more than one applied decision")
		}
		if index != len(task.TransitionDecisions)-1 {
			return invalid("transition_decisions", "the applied decision must be the latest decision")
		}
	}

	if applied == 0 {
		return nil
	}
	if task.Status != TaskStatusCompleted && task.Status != TaskStatusSkipped {
		return invalid("transition_decisions", "an applied decision requires an ended source task")
	}
	decision := task.TransitionDecisions[len(task.TransitionDecisions)-1]
	if task.Status == TaskStatusCompleted && decision.SourceSubmissionID == nil {
		return invalid("transition_decisions.source_submission_id", "is required for a completed source task")
	}
	if task.Status == TaskStatusSkipped && decision.SourceSubmissionID != nil {
		return invalid("transition_decisions.source_submission_id", "must be nil for a skipped source task")
	}
	return nil
}
