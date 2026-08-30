package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// ClaimWorkCandidateCommand reserves one Blackboard lifecycle candidate.
type ClaimWorkCandidateCommand struct {
	WorkItemID   domain.WorkItemID
	Kind         WorkCandidateKind
	Identity     Identity
	OperationID  string
	LeaseSeconds int64
}

// ClaimWorkCandidate atomically reserves a currently discoverable lifecycle candidate.
func (s *Service) ClaimWorkCandidate(ctx context.Context, command ClaimWorkCandidateCommand) (domain.CoordinationClaim, error) {
	if strings.TrimSpace(string(command.WorkItemID)) == "" {
		return domain.CoordinationClaim{}, invalidCommand("work item id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.CoordinationClaim{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorAgent {
		return domain.CoordinationClaim{}, forbidden("only an agent can claim a work candidate")
	}
	kind := domain.CoordinationClaimKind(command.Kind)
	if !kind.Valid() {
		return domain.CoordinationClaim{}, invalidCommand("unsupported work candidate kind %q", command.Kind)
	}

	var created domain.CoordinationClaim
	var terminalErr error
	err := s.replayableCreate(ctx, command.Identity, command.OperationID, "claim_work_candidate", command, &created, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", command.WorkItemID, err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		current, exists, err := coordinationCandidate(store, workItem)
		if err != nil {
			return err
		}
		if !exists || current != kind {
			return conflict("work item %q is not a %s candidate", workItem.ID, kind)
		}
		claims, err := store.ListCoordinationClaims(workItem.ID)
		if err != nil {
			return fmt.Errorf("list coordination claims: %w", err)
		}
		if err := domain.ValidateCoordinationClaimHistory(workItem.ID, claims); err != nil {
			return err
		}
		if activeCoordinationClaim(claims) != nil {
			return conflict("work item %q already has an active coordination claim", workItem.ID)
		}
		if len(claims) >= MaxCoordinationClaimsPerWorkItem {
			now := s.clock.Now()
			reason := fmt.Sprintf("work item %s reached the maximum of %d coordination claims", workItem.ID, MaxCoordinationClaimsPerWorkItem)
			if err := s.failWorkItem(store, &workItem, nil, systemFailureActor(), reason, now); err != nil {
				return err
			}
			terminalErr = conflict("work item %q failed: %s", workItem.ID, reason)
			return commitAndReturn(terminalErr)
		}
		id, err := s.newID("coordination claim id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		lease := normalizeClaimLease(command.LeaseSeconds, s.claimLease)
		created = domain.CoordinationClaim{
			ID: domain.CoordinationClaimID(id), WorkItemID: workItem.ID, Kind: kind, Executor: command.Identity.Actor,
			ClaimedAt: now, LastHeartbeatAt: now, LeaseUntil: now.Add(lease), LeaseSeconds: int64(lease / time.Second),
		}
		if err := created.Validate(); err != nil {
			return err
		}
		return store.CreateCoordinationClaim(created)
	})
	if err != nil {
		return domain.CoordinationClaim{}, err
	}
	if terminalErr != nil {
		return domain.CoordinationClaim{}, terminalErr
	}
	return created, nil
}

// HeartbeatCoordinationClaimCommand extends WorkItem-level responsibility.
type HeartbeatCoordinationClaimCommand struct {
	WorkItemID   domain.WorkItemID
	ClaimID      domain.CoordinationClaimID
	Identity     Identity
	LeaseSeconds int64
}

func (s *Service) HeartbeatCoordinationClaim(ctx context.Context, command HeartbeatCoordinationClaimCommand) (domain.CoordinationClaim, error) {
	if command.WorkItemID == "" || command.ClaimID == "" {
		return domain.CoordinationClaim{}, invalidCommand("work item id and coordination claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.CoordinationClaim{}, err
	}
	var result domain.CoordinationClaim
	err := s.repository.Update(ctx, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item: %w", err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		claim, err := ownedActiveCoordinationClaim(store, workItem.ID, command.ClaimID, command.Identity)
		if err != nil {
			return err
		}
		current, exists, err := coordinationCandidate(store, workItem)
		if err != nil {
			return err
		}
		if !exists || current != claim.Kind {
			return conflict("coordination claim %q no longer matches current work", claim.ID)
		}
		now := s.clock.Now()
		lease := normalizeClaimLease(command.LeaseSeconds, time.Duration(claim.LeaseSeconds)*time.Second)
		claim.LastHeartbeatAt = now
		claim.LeaseUntil = now.Add(lease)
		claim.LeaseSeconds = int64(lease / time.Second)
		if err := store.SaveCoordinationClaim(claim); err != nil {
			return fmt.Errorf("save coordination heartbeat: %w", err)
		}
		result = claim
		return nil
	})
	return result, err
}

type ReleaseCoordinationClaimCommand struct {
	WorkItemID domain.WorkItemID
	ClaimID    domain.CoordinationClaimID
	Identity   Identity
}

func (s *Service) ReleaseCoordinationClaim(ctx context.Context, command ReleaseCoordinationClaimCommand) error {
	if command.WorkItemID == "" || command.ClaimID == "" {
		return invalidCommand("work item id and coordination claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return err
	}
	return s.repository.Update(ctx, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item: %w", err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		claim, err := ownedActiveCoordinationClaim(store, workItem.ID, command.ClaimID, command.Identity)
		if err != nil {
			return err
		}
		return endCoordinationClaim(store, &claim, domain.CoordinationClaimEndReleased, s.clock.Now())
	})
}

func coordinationCandidate(store ReadStore, workItem domain.WorkItem) (domain.CoordinationClaimKind, bool, error) {
	if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
		return "", false, nil
	}
	if workItem.Status == domain.WorkItemStatusAwaitingAgentAcceptance {
		return domain.CoordinationClaimWorkItemAcceptance, true, nil
	}
	if workItem.Status != domain.WorkItemStatusOpen {
		return "", false, nil
	}
	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return "", false, fmt.Errorf("list blackboard tasks: %w", err)
	}
	if len(tasks) == 0 {
		return domain.CoordinationClaimEmptyBlackboard, true, nil
	}
	if blackboardTasksConverged(tasks) {
		return domain.CoordinationClaimBlackboardCompletion, true, nil
	}
	return "", false, nil
}

func ownedActiveCoordinationClaim(store ReadStore, workItemID domain.WorkItemID, claimID domain.CoordinationClaimID, identity Identity) (domain.CoordinationClaim, error) {
	claims, err := store.ListCoordinationClaims(workItemID)
	if err != nil {
		return domain.CoordinationClaim{}, fmt.Errorf("list coordination claims: %w", err)
	}
	if err := domain.ValidateCoordinationClaimHistory(workItemID, claims); err != nil {
		return domain.CoordinationClaim{}, err
	}
	for _, claim := range claims {
		if claim.ID != claimID {
			continue
		}
		if !claim.Active() {
			return domain.CoordinationClaim{}, conflict("coordination claim %q is not active", claim.ID)
		}
		if !sameActor(claim.Executor, identity.Actor) {
			return domain.CoordinationClaim{}, forbidden("actor does not own coordination claim %q", claim.ID)
		}
		return claim, nil
	}
	return domain.CoordinationClaim{}, fmt.Errorf("%w: coordination claim %q", ErrNotFound, claimID)
}

func requireCoordinationClaim(store ReadStore, workItem domain.WorkItem, kind domain.CoordinationClaimKind, claimID domain.CoordinationClaimID, identity Identity) (domain.CoordinationClaim, error) {
	if identity.Actor.Kind != domain.ActorAgent {
		return domain.CoordinationClaim{}, nil
	}
	if claimID == "" {
		return domain.CoordinationClaim{}, conflict("an active coordination claim is required for %s", kind)
	}
	claim, err := ownedActiveCoordinationClaim(store, workItem.ID, claimID, identity)
	if err != nil {
		return domain.CoordinationClaim{}, err
	}
	if claim.Kind != kind {
		return domain.CoordinationClaim{}, conflict("coordination claim %q is for %s, not %s", claim.ID, claim.Kind, kind)
	}
	return claim, nil
}

func activeCoordinationClaim(claims []domain.CoordinationClaim) *domain.CoordinationClaim {
	for i := range claims {
		if claims[i].Active() {
			claim := claims[i]
			return &claim
		}
	}
	return nil
}

func endCoordinationClaim(store WriteStore, claim *domain.CoordinationClaim, reason domain.CoordinationClaimEndReason, now time.Time) error {
	if claim == nil || !claim.Active() {
		return nil
	}
	claim.EndedAt = &now
	claim.EndReason = reason
	if err := store.SaveCoordinationClaim(*claim); err != nil {
		return fmt.Errorf("save ended coordination claim: %w", err)
	}
	return nil
}

func endActiveCoordinationClaim(store WriteStore, workItemID domain.WorkItemID, reason domain.CoordinationClaimEndReason, now time.Time) error {
	claims, err := store.ListCoordinationClaims(workItemID)
	if err != nil {
		return fmt.Errorf("list coordination claims: %w", err)
	}
	if err := domain.ValidateCoordinationClaimHistory(workItemID, claims); err != nil {
		return err
	}
	claim := activeCoordinationClaim(claims)
	return endCoordinationClaim(store, claim, reason, now)
}
