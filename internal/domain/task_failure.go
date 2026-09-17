package domain

import (
	"strings"
	"time"
)

// TaskFailureAction determines how Kairos reacts to an execution failure.
type TaskFailureAction string

const (
	TaskFailureRetry        TaskFailureAction = "retry"
	TaskFailureAwaitHuman   TaskFailureAction = "await_human"
	TaskFailureFailWorkItem TaskFailureAction = "fail_work_item"
)

// Valid reports whether the failure action is recognized.
func (a TaskFailureAction) Valid() bool {
	return a == TaskFailureAwaitHuman || a == TaskFailureRetry || a == TaskFailureFailWorkItem
}

// TaskFailure records one immutable failure reported from a Claim.
type TaskFailure struct {
	ID      TaskFailureID `json:"id"`
	TaskID  TaskID        `json:"task_id"`
	ClaimID ClaimID       `json:"claim_id"`

	Action TaskFailureAction `json:"action"`
	Reason string            `json:"reason"`

	// RetryPrompt guides the next attempt when Action is retry.
	RetryPrompt string `json:"retry_prompt"`

	FailedAt time.Time `json:"failed_at"`
}

// Validate checks the TaskFailure invariants.
func (f TaskFailure) Validate() error {
	if strings.TrimSpace(string(f.ID)) == "" {
		return invalid("failure.id", "is required")
	}
	if strings.TrimSpace(string(f.TaskID)) == "" {
		return invalid("failure.task_id", "is required")
	}
	if strings.TrimSpace(string(f.ClaimID)) == "" {
		return invalid("failure.claim_id", "is required")
	}
	if !f.Action.Valid() {
		return invalid("failure.action", "unsupported value %q", f.Action)
	}
	if strings.TrimSpace(f.Reason) == "" {
		return invalid("failure.reason", "is required")
	}
	if err := validateHistoryText("failure.reason", f.Reason); err != nil {
		return err
	}
	if err := validateHistoryText("failure.retry_prompt", f.RetryPrompt); err != nil {
		return err
	}
	if f.Action != TaskFailureRetry && strings.TrimSpace(f.RetryPrompt) != "" {
		return invalid("failure.retry_prompt", "is supported only for action retry")
	}
	if f.FailedAt.IsZero() {
		return invalid("failure.failed_at", "is required")
	}
	return nil
}

func validateTaskFailureHistory(taskID TaskID, failures []TaskFailure) error {
	seen := make(map[TaskFailureID]struct{}, len(failures))
	var previous time.Time

	for _, failure := range failures {
		if err := failure.Validate(); err != nil {
			return err
		}
		if failure.TaskID != taskID {
			return invalid("failures", "failure %q belongs to another task", failure.ID)
		}
		if _, ok := seen[failure.ID]; ok {
			return invalid("failures", "contains duplicate failure %q", failure.ID)
		}
		seen[failure.ID] = struct{}{}

		if !previous.IsZero() && failure.FailedAt.Before(previous) {
			return invalid("failures", "must be ordered by failed_at")
		}
		previous = failure.FailedAt
	}

	return nil
}
