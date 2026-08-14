package domain

import "strings"

// WorkflowSkipIntent lists optional Task Definitions the executor permits the
// runtime to skip during the current graph expansion.
type WorkflowSkipIntent struct {
	// SkipTaskIDs apply only while the current expansion remains on skipped paths.
	SkipTaskIDs []WorkflowTaskID

	// ReviewRequestedTaskIDs is a subset of SkipTaskIDs whose executor_decides
	// policy should request human Review.
	ReviewRequestedTaskIDs []WorkflowTaskID
}

// Validate checks local Skip Intent invariants.
func (i WorkflowSkipIntent) Validate() error {
	skipped := make(map[WorkflowTaskID]struct{}, len(i.SkipTaskIDs))
	for _, taskID := range i.SkipTaskIDs {
		if strings.TrimSpace(string(taskID)) == "" {
			return invalid("workflow_skip_intent.skip_task_ids", "must not contain empty values")
		}
		if _, exists := skipped[taskID]; exists {
			return invalid("workflow_skip_intent.skip_task_ids", "contains duplicate task %q", taskID)
		}
		skipped[taskID] = struct{}{}
	}
	reviewed := make(map[WorkflowTaskID]struct{}, len(i.ReviewRequestedTaskIDs))
	for _, taskID := range i.ReviewRequestedTaskIDs {
		if strings.TrimSpace(string(taskID)) == "" {
			return invalid("workflow_skip_intent.review_requested_task_ids", "must not contain empty values")
		}
		if _, exists := reviewed[taskID]; exists {
			return invalid("workflow_skip_intent.review_requested_task_ids", "contains duplicate task %q", taskID)
		}
		if _, exists := skipped[taskID]; !exists {
			return invalid("workflow_skip_intent.review_requested_task_ids", "task %q must also be skipped", taskID)
		}
		reviewed[taskID] = struct{}{}
	}
	return nil
}

// WorkflowTransition contains one selected choice group's relation outcomes.
type WorkflowTransition struct {
	ChoiceGroupID WorkflowChoiceGroupID

	TriggeredRelationIDs       []WorkflowRelationID
	SkippedRelationIDs         []WorkflowRelationID
	ReviewRequestedRelationIDs []WorkflowRelationID

	Reason string
}

// Validate checks transition fields that do not require the Workflow Definition.
func (t WorkflowTransition) Validate() error {
	if strings.TrimSpace(string(t.ChoiceGroupID)) == "" {
		return invalid("workflow_transition.choice_group_id", "is required")
	}
	if len(t.TriggeredRelationIDs) == 0 && len(t.SkippedRelationIDs) == 0 {
		return invalid("workflow_transition.relations", "must not be empty")
	}

	seen := make(map[WorkflowRelationID]string, len(t.TriggeredRelationIDs)+len(t.SkippedRelationIDs))
	for _, relationID := range t.TriggeredRelationIDs {
		if err := validateDecisionRelationID("workflow_transition.triggered_relation_ids", relationID, seen, "triggered"); err != nil {
			return err
		}
	}
	for _, relationID := range t.SkippedRelationIDs {
		if err := validateDecisionRelationID("workflow_transition.skipped_relation_ids", relationID, seen, "skipped"); err != nil {
			return err
		}
	}
	seenReviewRequests := make(map[WorkflowRelationID]struct{}, len(t.ReviewRequestedRelationIDs))
	for _, relationID := range t.ReviewRequestedRelationIDs {
		if _, exists := seenReviewRequests[relationID]; exists {
			return invalid("workflow_transition.review_requested_relation_ids", "contains duplicate relation %q", relationID)
		}
		seenReviewRequests[relationID] = struct{}{}
		if set, exists := seen[relationID]; !exists || set != "skipped" {
			return invalid("workflow_transition.review_requested_relation_ids", "relation %q must be skipped", relationID)
		}
	}
	return nil
}
