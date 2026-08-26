package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// CancelWorkItemCommand terminally cancels an active WorkItem.
type CancelWorkItemCommand struct {
	WorkItemID domain.WorkItemID
	Identity   Identity
	Reason     string
}

// CancelWorkItem cancels a WorkItem and ends its active Claims without changing
// the outcome of Tasks that have already reached a durable state.
func (s *Service) CancelWorkItem(ctx context.Context, command CancelWorkItemCommand) (domain.WorkItem, error) {
	if strings.TrimSpace(string(command.WorkItemID)) == "" {
		return domain.WorkItem{}, invalidCommand("work item id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorHuman {
		return domain.WorkItem{}, forbidden("only a human can cancel a work item")
	}
	command.Reason = strings.TrimSpace(command.Reason)
	if command.Reason == "" {
		return domain.WorkItem{}, invalidCommand("cancellation reason is required")
	}

	var cancelled domain.WorkItem
	err := s.repository.Update(ctx, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", command.WorkItemID, err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		switch workItem.Status {
		case domain.WorkItemStatusOpen, domain.WorkItemStatusAwaitingAgentAcceptance, domain.WorkItemStatusAwaitingHumanAcceptance:
		default:
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}

		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list tasks for work item %q: %w", workItem.ID, err)
		}
		now := s.clock.Now()
		actor := command.Identity.Actor
		for _, task := range tasks {
			if task.ActiveClaimID == nil {
				continue
			}
			claims, err := store.ListClaims(task.ID)
			if err != nil {
				return fmt.Errorf("list claims for task %q: %w", task.ID, err)
			}
			claimIndex := findClaim(claims, *task.ActiveClaimID)
			if claimIndex < 0 || !claims[claimIndex].Active() {
				return conflict("task %q has an invalid active claim", task.ID)
			}
			claim := claims[claimIndex]
			claim.EndedAt = &now
			claim.EndReason = domain.ClaimEndWorkItemCancelled
			claims[claimIndex] = claim
			task.ActiveClaimID = nil
			if task.Status == domain.TaskStatusWorking {
				task.Status = domain.TaskStatusPending
			}
			task.UpdatedAt = now
			task.Version++
			if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
				return err
			}
			if err := store.SaveClaim(claim); err != nil {
				return fmt.Errorf("save cancelled claim %q: %w", claim.ID, err)
			}
			if err := store.SaveTask(task); err != nil {
				return fmt.Errorf("save task %q after cancellation: %w", task.ID, err)
			}
			if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskRevoked, string(claim.ID), &actor, "work item cancelled"); err != nil {
				return err
			}
		}

		workItem.Status = domain.WorkItemStatusCancelled
		workItem.Result = ""
		workItem.CompletedAt = nil
		workItem.CancelledAt = &now
		workItem.CancelledBy = &actor
		workItem.CancellationReason = command.Reason
		workItem.UpdatedAt = now
		workItem.Version++
		if err := workItem.Validate(); err != nil {
			return err
		}
		if err := store.SaveWorkItem(workItem); err != nil {
			return fmt.Errorf("save cancelled work item: %w", err)
		}
		if err := s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemCancelled, string(workItem.ID), &actor, command.Reason); err != nil {
			return err
		}
		cancelled = workItem
		return nil
	})
	if err != nil {
		return domain.WorkItem{}, err
	}
	return normalizeWorkItemCollections(cancelled), nil
}
