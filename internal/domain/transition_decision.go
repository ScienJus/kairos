package domain

import (
	"strings"
	"time"
)

// TransitionDecision records one Workflow progression decision. A decision may
// wait for Review before AppliedAt is set and downstream state is changed.
type TransitionDecision struct {
	ID TransitionDecisionID `json:"id"`

	WorkItemID   WorkItemID `json:"work_item_id"`
	SourceTaskID TaskID     `json:"source_task_id"`

	// SourceSubmissionID is nil when the source Task ended by being skipped.
	SourceSubmissionID *SubmissionID `json:"source_submission_id"`

	WorkflowTransition
	WorkflowSkipIntent

	DecidedBy ActorRef   `json:"decided_by"`
	DecidedAt time.Time  `json:"decided_at"`
	AppliedAt *time.Time `json:"applied_at"`
}

// Validate checks the TransitionDecision invariants that do not require its Definition.
func (d TransitionDecision) Validate() error {
	if strings.TrimSpace(string(d.ID)) == "" {
		return invalid("transition_decision.id", "is required")
	}
	if strings.TrimSpace(string(d.WorkItemID)) == "" {
		return invalid("transition_decision.work_item_id", "is required")
	}
	if strings.TrimSpace(string(d.SourceTaskID)) == "" {
		return invalid("transition_decision.source_task_id", "is required")
	}
	if d.SourceSubmissionID != nil && strings.TrimSpace(string(*d.SourceSubmissionID)) == "" {
		return invalid("transition_decision.source_submission_id", "must not be empty")
	}
	if err := d.WorkflowTransition.Validate(); err != nil {
		return err
	}
	if err := d.WorkflowSkipIntent.Validate(); err != nil {
		return err
	}
	if err := d.DecidedBy.Validate(); err != nil {
		return err
	}
	if d.DecidedAt.IsZero() {
		return invalid("transition_decision.decided_at", "is required")
	}
	if d.AppliedAt != nil {
		if d.AppliedAt.IsZero() {
			return invalid("transition_decision.applied_at", "must not be zero")
		}
		if d.AppliedAt.Before(d.DecidedAt) {
			return invalid("transition_decision.applied_at", "must not be before decided_at")
		}
	}
	return nil
}

func validateDecisionRelationID(
	field string,
	relationID WorkflowRelationID,
	seen map[WorkflowRelationID]string,
	set string,
) error {
	if strings.TrimSpace(string(relationID)) == "" {
		return invalid(field, "must not contain empty values")
	}
	if previous, ok := seen[relationID]; ok {
		return invalid(field, "relation %q is already %s", relationID, previous)
	}
	seen[relationID] = set
	return nil
}
