package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// ReleaseClaimCommand gives up active execution responsibility.
type ReleaseClaimCommand struct {
	TaskID      domain.TaskID
	ClaimID     domain.ClaimID
	Identity    Identity
	OperationID string
	Reason      string
}

// ReleaseClaim returns a working Task to the candidate set.
func (s *Service) ReleaseClaim(ctx context.Context, command ReleaseClaimCommand) error {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ClaimID)) == "" {
		return invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return err
	}

	var result struct{}
	return s.idempotentUpdate(ctx, command.Identity, command.OperationID, "release_claim", command, &result, func(store WriteStore) error {
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
		claimIndex := findClaim(claims, command.ClaimID)
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

		now := s.clock.Now()
		claim.EndedAt = &now
		claim.EndReason = domain.ClaimEndReleased
		claims[claimIndex] = claim
		task.Status = domain.TaskStatusPending
		task.ActiveClaimID = nil
		task.UpdatedAt = now
		task.Version++
		if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
			return err
		}
		if err := store.SaveClaim(claim); err != nil {
			return fmt.Errorf("save released claim: %w", err)
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save released task: %w", err)
		}
		actor := command.Identity.Actor
		return s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskReleased, string(claim.ID), &actor, strings.TrimSpace(command.Reason))
	})
}

func findClaim(claims []domain.Claim, id domain.ClaimID) int {
	for i := range claims {
		if claims[i].ID == id {
			return i
		}
	}
	return -1
}
