package application

import (
	"fmt"

	"github.com/ScienJus/kairos/internal/domain"
)

func (s *Service) appendEvent(
	store WriteStore,
	workItemID domain.WorkItemID,
	taskID *domain.TaskID,
	eventType domain.WorkItemEventType,
	entityID string,
	actor *domain.ActorRef,
	message string,
) error {
	sequence, err := store.LastWorkItemEventSequence(workItemID)
	if err != nil {
		return fmt.Errorf("read last event sequence: %w", err)
	}
	id, err := s.newID("event id")
	if err != nil {
		return err
	}
	event := domain.WorkItemEvent{
		ID:         domain.WorkItemEventID(id),
		WorkItemID: workItemID,
		Sequence:   sequence + 1,
		Type:       eventType,
		TaskID:     taskID,
		EntityID:   entityID,
		Actor:      actor,
		Message:    message,
		OccurredAt: s.clock.Now(),
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := store.AppendWorkItemEvent(event); err != nil {
		return fmt.Errorf("append work item event: %w", err)
	}
	return nil
}
