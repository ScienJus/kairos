package application

import (
	"context"
	"fmt"
	"sort"

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

// ListHumanAttention returns pending Reviews and unclaimed Tasks assigned to a human.
func (s *Service) ListHumanAttention(ctx context.Context, identity Identity) ([]HumanAttentionItem, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	items := []HumanAttentionItem{}
	err := s.repository.View(ctx, func(store ReadStore) error {
		workItems, err := store.ListWorkItems(WorkItemFilter{
			Statuses: []domain.WorkItemStatus{
				domain.WorkItemStatusOpen,
				domain.WorkItemStatusAwaitingHumanAcceptance,
			},
		})
		if err != nil {
			return fmt.Errorf("list work items: %w", err)
		}
		for _, workItem := range workItems {
			if workItem.Status != domain.WorkItemStatusOpen && workItem.Status != domain.WorkItemStatusAwaitingHumanAcceptance {
				continue
			}
			if workItem.Status == domain.WorkItemStatusAwaitingHumanAcceptance {
				items = append(items, HumanAttentionItem{Kind: HumanAttentionAcceptance, WorkItem: normalizeWorkItemCollections(workItem)})
				continue
			}
			tasks, err := store.ListTasks(workItem.ID)
			if err != nil {
				return fmt.Errorf("list tasks for work item %q: %w", workItem.ID, err)
			}
			for _, task := range tasks {
				kind := HumanAttentionKind("")
				if task.Status == domain.TaskStatusInReview && hasPendingReview(task) {
					kind = HumanAttentionReview
				} else if task.Status == domain.TaskStatusPending && task.ActiveClaimID == nil && task.Executor == domain.ExecutorHuman {
					kind = HumanAttentionTask
				}
				if kind != "" {
					taskCopy := normalizeTaskCollections(task)
					items = append(items, HumanAttentionItem{Kind: kind, WorkItem: normalizeWorkItemCollections(workItem), Task: &taskCopy})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Kind != items[right].Kind {
			return items[left].Kind == HumanAttentionReview
		}
		leftUpdated := items[left].WorkItem.UpdatedAt
		rightUpdated := items[right].WorkItem.UpdatedAt
		if items[left].Task != nil {
			leftUpdated = items[left].Task.UpdatedAt
		}
		if items[right].Task != nil {
			rightUpdated = items[right].Task.UpdatedAt
		}
		return leftUpdated.After(rightUpdated)
	})
	return items, nil
}

func hasPendingReview(task domain.Task) bool {
	for _, review := range task.Reviews {
		if review.Status == domain.ReviewStatusPending {
			return true
		}
	}
	return false
}
