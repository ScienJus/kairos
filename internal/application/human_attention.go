package application

import (
	"context"
	"fmt"

	"github.com/ScienJus/kairos/internal/domain"
)

// HumanAttentionKind explains why a Task appears in the human attention view.
type HumanAttentionKind string

const (
	HumanAttentionReview     HumanAttentionKind = "review"
	HumanAttentionTask       HumanAttentionKind = "human_task"
	HumanAttentionAcceptance HumanAttentionKind = "work_item_acceptance"
)

// HumanAttentionItem is one lightweight homepage entry that needs a person.
type HumanAttentionItem struct {
	Kind     HumanAttentionKind `json:"kind"`
	WorkItem domain.WorkItem    `json:"work_item"`
	Task     *domain.Task       `json:"task"`
}

// Cursor returns the item's stable position in the Human Attention collection.
func (i HumanAttentionItem) Cursor() HumanAttentionCursor {
	updatedAt := i.WorkItem.UpdatedAt
	taskID := domain.TaskID("")
	if i.Task != nil {
		updatedAt = i.Task.UpdatedAt
		taskID = i.Task.ID
	}
	priority := 1
	if i.Kind == HumanAttentionReview {
		priority = 0
	}
	return HumanAttentionCursor{Priority: priority, UpdatedAt: updatedAt, WorkItemID: i.WorkItem.ID, TaskID: taskID}
}

// ListHumanAttention returns a page of pending Reviews, human Tasks, and human acceptances.
func (s *Service) ListHumanAttention(ctx context.Context, identity Identity, page PageRequest[HumanAttentionCursor]) (Page[HumanAttentionItem], error) {
	if err := identity.Validate(); err != nil {
		return Page[HumanAttentionItem]{}, err
	}
	if err := validatePageRequest(page.Limit); err != nil {
		return Page[HumanAttentionItem]{}, err
	}
	items := make([]HumanAttentionItem, 0)
	err := s.repository.View(ctx, func(store ReadStore) error {
		candidates, err := store.ListHumanAttention(page)
		if err != nil {
			return fmt.Errorf("list human attention: %w", err)
		}
		for _, candidate := range candidates {
			candidate.WorkItem = normalizeWorkItemCollections(candidate.WorkItem)
			if candidate.Task != nil {
				task := normalizeTaskCollections(*candidate.Task)
				candidate.Task = &task
			}
			items = append(items, candidate)
		}
		return nil
	})
	if err != nil {
		return Page[HumanAttentionItem]{}, err
	}
	return boundedPage(items, page.Limit), nil
}
