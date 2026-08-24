package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// FailTaskCommand reports an execution failure from the active Claim.
type FailTaskCommand struct {
	TaskID      domain.TaskID
	ClaimID     domain.ClaimID
	Identity    Identity
	OperationID string
	Action      domain.TaskFailureAction
	Reason      string
	RetryPrompt string
}

// FailTask reopens the Task or fails its entire WorkItem.
func (s *Service) FailTask(ctx context.Context, command FailTaskCommand) (domain.TaskFailure, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ClaimID)) == "" {
		return domain.TaskFailure{}, invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.TaskFailure{}, err
	}

	var created domain.TaskFailure
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "fail_task", command, &created, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		claimIndex := -1
		for i := range claims {
			if claims[i].ID == command.ClaimID {
				claimIndex = i
				break
			}
		}
		if claimIndex < 0 {
			return fmt.Errorf("%w: claim %q", ErrNotFound, command.ClaimID)
		}
		claim := claims[claimIndex]
		if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || *task.ActiveClaimID != claim.ID || !claim.Active() {
			return conflict("claim %q is not active for task %q", claim.ID, task.ID)
		}
		if !sameActor(claim.Executor, command.Identity.Actor) {
			return forbidden("actor does not own claim %q", claim.ID)
		}

		id, err := s.newID("failure id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		failure := domain.TaskFailure{
			ID:          domain.TaskFailureID(id),
			TaskID:      task.ID,
			ClaimID:     claim.ID,
			Action:      command.Action,
			Reason:      command.Reason,
			RetryPrompt: command.RetryPrompt,
			FailedAt:    now,
		}
		if err := failure.Validate(); err != nil {
			return err
		}

		claim.EndedAt = &now
		claim.EndReason = domain.ClaimEndTaskFailed
		claims[claimIndex] = claim
		task.ActiveClaimID = nil
		task.Failures = append(task.Failures, failure)
		task.UpdatedAt = now
		task.Version++

		eventType := domain.WorkItemEventTaskReopened
		if failure.Action == domain.TaskFailureReopen {
			task.Status = domain.TaskStatusPending
		} else {
			task.Status = domain.TaskStatusFailed
			eventType = domain.WorkItemEventTaskFailed
			workItem.Status = domain.WorkItemStatusFailed
			workItem.UpdatedAt = now
			workItem.Version++
		}

		if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
			return err
		}
		if err := store.SaveClaim(claim); err != nil {
			return fmt.Errorf("save failed claim: %w", err)
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save failed task: %w", err)
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &task.ID, eventType, string(failure.ID), &actor, failure.Reason); err != nil {
			return err
		}

		if failure.Action == domain.TaskFailureFailWorkItem {
			if err := s.revokeOtherClaims(store, workItem, task.ID, now); err != nil {
				return err
			}
			if err := store.SaveWorkItem(workItem); err != nil {
				return fmt.Errorf("save failed work item: %w", err)
			}
			if err := s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemFailed, string(workItem.ID), &actor, failure.Reason); err != nil {
				return err
			}
		}

		created = failure
		return nil
	})
	if err != nil {
		return domain.TaskFailure{}, err
	}
	return created, nil
}

func (s *Service) revokeOtherClaims(
	store WriteStore,
	workItem domain.WorkItem,
	failedTaskID domain.TaskID,
	now time.Time,
) error {
	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return fmt.Errorf("list tasks for failed work item: %w", err)
	}
	for _, task := range tasks {
		if task.ID == failedTaskID || task.ActiveClaimID == nil {
			continue
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		for i := range claims {
			if !claims[i].Active() || claims[i].ID != *task.ActiveClaimID {
				continue
			}
			claims[i].EndedAt = &now
			claims[i].EndReason = domain.ClaimEndRevoked
			task.ActiveClaimID = nil
			task.Status = domain.TaskStatusPending
			task.UpdatedAt = now
			task.Version++
			if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
				return err
			}
			if err := store.SaveClaim(claims[i]); err != nil {
				return fmt.Errorf("save revoked claim: %w", err)
			}
			if err := store.SaveTask(task); err != nil {
				return fmt.Errorf("save task after revocation: %w", err)
			}
			actor := claims[i].Executor
			if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskRevoked, string(claims[i].ID), &actor, "work item failed"); err != nil {
				return err
			}
			break
		}
	}
	return nil
}
