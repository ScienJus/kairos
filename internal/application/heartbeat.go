package application

import (
	"context"
	"fmt"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// expireActiveClaim releases an expired lease inside the caller's transaction.
func (s *Service) expireActiveClaim(store WriteStore, task *domain.Task, claims []domain.Claim, now time.Time) (bool, error) {
	if task.ActiveClaimID == nil {
		return false, nil
	}
	idx := findClaim(claims, *task.ActiveClaimID)
	if idx < 0 || claims[idx].Executor.Kind != domain.ActorAgent || !claims[idx].Active() || claims[idx].LeaseUntil.IsZero() || now.Before(claims[idx].LeaseUntil) {
		return false, nil
	}
	claim := claims[idx]
	claim.EndedAt = &now
	claim.EndReason = domain.ClaimEndExpired
	if err := store.SaveClaim(claim); err != nil {
		return false, err
	}
	task.Status = domain.TaskStatusPending
	task.ActiveClaimID = nil
	task.UpdatedAt = now
	task.Version++
	if err := store.SaveTask(*task); err != nil {
		return false, err
	}
	workItem, err := store.GetWorkItem(task.WorkItemID)
	if err != nil {
		return false, err
	}
	workItem.UpdatedAt = now
	workItem.Version++
	if err := store.SaveWorkItem(workItem); err != nil {
		return false, err
	}
	message := fmt.Sprintf("lease expired at %s", claim.LeaseUntil.UTC().Format(time.RFC3339Nano))
	actor := claim.Executor
	if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskClaimExpired, string(claim.ID), &actor, message); err != nil {
		return false, err
	}
	return true, nil
}

type HeartbeatClaimCommand struct {
	TaskID       domain.TaskID
	ClaimID      domain.ClaimID
	Identity     Identity
	OperationID  string
	LeaseSeconds int64
}

// HeartbeatClaim extends an active lease. A lease cannot be revived after expiry.
func (s *Service) HeartbeatClaim(ctx context.Context, command HeartbeatClaimCommand) (domain.Claim, error) {
	if command.TaskID == "" || command.ClaimID == "" {
		return domain.Claim{}, invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Claim{}, err
	}
	var result domain.Claim
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "heartbeat_claim", command, &result, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task: %w", err)
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims: %w", err)
		}
		idx := findClaim(claims, command.ClaimID)
		if idx < 0 {
			return fmt.Errorf("%w: claim %q", ErrNotFound, command.ClaimID)
		}
		claim := claims[idx]
		if claim.Executor.Kind != domain.ActorAgent {
			return invalidCommand("human claims do not use heartbeat")
		}
		if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || *task.ActiveClaimID != claim.ID || !claim.Active() {
			return conflict("claim %q is not active", claim.ID)
		}
		if !sameActor(claim.Executor, command.Identity.Actor) {
			return forbidden("actor does not own claim %q", claim.ID)
		}
		now := s.clock.Now()
		if !claim.LeaseUntil.IsZero() && !now.Before(claim.LeaseUntil) {
			return conflict("claim %q lease has expired", claim.ID)
		}
		lease := normalizeClaimLease(command.LeaseSeconds, time.Duration(claim.LeaseSeconds)*time.Second)
		if claim.LeaseSeconds <= 0 {
			lease = normalizeClaimLease(command.LeaseSeconds, s.claimLease)
		}
		claim.LastHeartbeatAt = now
		claim.LeaseUntil = now.Add(lease)
		claim.LeaseSeconds = int64(lease / time.Second)
		if err := store.SaveClaim(claim); err != nil {
			return fmt.Errorf("save heartbeat: %w", err)
		}
		result = claim
		return nil
	})
	return result, err
}
