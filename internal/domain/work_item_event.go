package domain

import (
	"strings"
	"time"
)

// WorkItemEventType identifies an observable WorkItem lifecycle change.
type WorkItemEventType string

const (
	WorkItemEventWorkItemCreated   WorkItemEventType = "work_item.created"
	WorkItemEventWorkItemUpdated   WorkItemEventType = "work_item.updated"
	WorkItemEventWorkItemCompleted WorkItemEventType = "work_item.completed"
	WorkItemEventWorkItemCancelled WorkItemEventType = "work_item.cancelled"
	WorkItemEventWorkItemFailed    WorkItemEventType = "work_item.failed"

	WorkItemEventTaskCreated    WorkItemEventType = "task.created"
	WorkItemEventTaskUpdated    WorkItemEventType = "task.updated"
	WorkItemEventTaskClaimed    WorkItemEventType = "task.claimed"
	WorkItemEventTaskReleased   WorkItemEventType = "task.released"
	WorkItemEventTaskRevoked    WorkItemEventType = "task.revoked"
	WorkItemEventTaskSubmitted  WorkItemEventType = "task.submitted"
	WorkItemEventTaskCompleted  WorkItemEventType = "task.completed"
	WorkItemEventTaskSkipped    WorkItemEventType = "task.skipped"
	WorkItemEventTaskFailed     WorkItemEventType = "task.failed"
	WorkItemEventTaskReopened   WorkItemEventType = "task.reopened"
	WorkItemEventTaskDecomposed WorkItemEventType = "task.decomposed"
	WorkItemEventRelationAdded  WorkItemEventType = "task_relation.added"

	WorkItemEventReviewRequested WorkItemEventType = "review.requested"
	WorkItemEventReviewApproved  WorkItemEventType = "review.approved"
	WorkItemEventReviewRejected  WorkItemEventType = "review.rejected"

	WorkItemEventTransitionDecided WorkItemEventType = "transition.decided"
	WorkItemEventTransitionApplied WorkItemEventType = "transition.applied"
)

// Valid reports whether the event type is recognized.
func (t WorkItemEventType) Valid() bool {
	_, ok := t.workItemScoped()
	return ok
}

func (t WorkItemEventType) workItemScoped() (bool, bool) {
	switch t {
	case WorkItemEventWorkItemCreated,
		WorkItemEventWorkItemUpdated,
		WorkItemEventWorkItemCompleted,
		WorkItemEventWorkItemCancelled,
		WorkItemEventWorkItemFailed:
		return true, true
	case WorkItemEventTaskCreated,
		WorkItemEventTaskUpdated,
		WorkItemEventTaskClaimed,
		WorkItemEventTaskReleased,
		WorkItemEventTaskRevoked,
		WorkItemEventTaskSubmitted,
		WorkItemEventTaskCompleted,
		WorkItemEventTaskSkipped,
		WorkItemEventTaskFailed,
		WorkItemEventTaskReopened,
		WorkItemEventTaskDecomposed,
		WorkItemEventRelationAdded,
		WorkItemEventReviewRequested,
		WorkItemEventReviewApproved,
		WorkItemEventReviewRejected,
		WorkItemEventTransitionDecided,
		WorkItemEventTransitionApplied:
		return false, true
	default:
		return false, false
	}
}

// WorkItemEvent records one append-only observable change.
type WorkItemEvent struct {
	ID         WorkItemEventID
	WorkItemID WorkItemID
	Sequence   int64
	Type       WorkItemEventType

	TaskID *TaskID

	// EntityID identifies the domain record described by Type.
	EntityID string
	Actor    *ActorRef

	Message    string
	OccurredAt time.Time
}

// Validate checks the WorkItemEvent invariants.
func (e WorkItemEvent) Validate() error {
	if strings.TrimSpace(string(e.ID)) == "" {
		return invalid("event.id", "is required")
	}
	if strings.TrimSpace(string(e.WorkItemID)) == "" {
		return invalid("event.work_item_id", "is required")
	}
	if e.Sequence <= 0 {
		return invalid("event.sequence", "must be greater than zero")
	}
	workItemScoped, validType := e.Type.workItemScoped()
	if !validType {
		return invalid("event.type", "unsupported value %q", e.Type)
	}
	if !workItemScoped {
		if e.TaskID == nil || strings.TrimSpace(string(*e.TaskID)) == "" {
			return invalid("event.task_id", "is required for task-scoped events")
		}
	} else if e.TaskID != nil {
		return invalid("event.task_id", "must be nil for work item events")
	}
	if strings.TrimSpace(e.EntityID) == "" {
		return invalid("event.entity_id", "is required")
	}
	if workItemScoped && e.EntityID != string(e.WorkItemID) {
		return invalid("event.entity_id", "must match work_item_id for work item events")
	}
	if e.Actor != nil {
		if err := e.Actor.Validate(); err != nil {
			return err
		}
	}
	if e.OccurredAt.IsZero() {
		return invalid("event.occurred_at", "is required")
	}
	return nil
}

// ValidateWorkItemEventHistory verifies an append-only history in sequence order.
func ValidateWorkItemEventHistory(workItemID WorkItemID, events []WorkItemEvent) error {
	if strings.TrimSpace(string(workItemID)) == "" {
		return invalid("work_item_id", "is required")
	}
	seen := make(map[WorkItemEventID]struct{}, len(events))
	var previousSequence int64
	var previousTime time.Time

	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
		if event.WorkItemID != workItemID {
			return invalid("events", "event %q belongs to another work item", event.ID)
		}
		if _, ok := seen[event.ID]; ok {
			return invalid("events", "contains duplicate event %q", event.ID)
		}
		seen[event.ID] = struct{}{}
		if event.Sequence <= previousSequence {
			return invalid("events", "sequence must be strictly increasing")
		}
		if !previousTime.IsZero() && event.OccurredAt.Before(previousTime) {
			return invalid("events", "must be ordered by occurred_at")
		}
		previousSequence = event.Sequence
		previousTime = event.OccurredAt
	}
	return nil
}
