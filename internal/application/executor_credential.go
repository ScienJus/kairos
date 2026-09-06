package application

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

func executorClaimTokenHash(p Identity, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	if p.Actor.Kind != domain.ActorAgent {
		return "", forbidden("only Agent Claims support executor credentials")
	}
	return identity.ExecutorTokenHash(token)
}

// Authenticate resolves a Claim capability without loading its executor's Identity.
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	hash, err := identity.ExecutorTokenHash(token)
	if err != nil {
		return identity.Principal{}, identity.ErrUnauthenticated
	}
	var p identity.Principal
	err = s.repository.View(ctx, func(store ReadStore) error {
		matches, err := store.FindExecutorPrincipals(hash)
		if err != nil {
			return err
		}
		if len(matches) != 1 || matches[0].Executor == nil {
			return identity.ErrUnauthenticated
		}
		p = matches[0]
		return authorizeExecutor(store, p, identity.ScopedRead, p.Executor.WorkItemID)
	})
	if err != nil {
		return identity.Principal{}, err
	}
	return p, nil
}

// authorizeExecutor runs inside the business transaction. SQL writes lock the
// WorkItem here, sharing the lock order used by Claim endings and the reaper.
func authorizeExecutor(store ReadStore, p Identity, capability identity.Capability, workItemID domain.WorkItemID) error {
	if err := p.ValidateCapability(capability); err != nil {
		return err
	}
	e := p.Executor
	if e == nil {
		return nil
	}
	if e.WorkItemID != workItemID {
		return forbidden("resource is outside the executor WorkItem scope")
	}
	workItem, err := store.GetWorkItem(workItemID)
	if errors.Is(err, ErrNotFound) {
		return identity.ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if workItem.Status != domain.WorkItemStatusOpen && workItem.Status != domain.WorkItemStatusAwaitingAgentAcceptance {
		return identity.ErrUnauthenticated
	}
	if capability == identity.BlackboardPlanningWrite && workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
		return forbidden("planning writes require a Blackboard Claim")
	}
	if e.Profile == identity.TaskExecutor {
		task, err := store.GetTask(e.TaskID)
		if err != nil {
			return err
		}
		if task.WorkItemID != workItemID || task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || string(*task.ActiveClaimID) != e.ClaimID {
			return identity.ErrUnauthenticated
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return err
		}
		for _, c := range claims {
			if string(c.ID) == e.ClaimID && c.Active() && sameActor(c.Executor, p.Actor) && sameHash(c.ExecutorTokenHash, e.TokenHash) {
				return nil
			}
		}
	} else {
		claims, err := store.ListCoordinationClaims(workItemID)
		if err != nil {
			return err
		}
		for _, c := range claims {
			if string(c.ID) == e.ClaimID && c.Active() && c.Kind == e.CandidateKind && sameActor(c.Executor, p.Actor) && sameHash(c.ExecutorTokenHash, e.TokenHash) {
				return nil
			}
		}
	}
	return identity.ErrUnauthenticated
}

func sameHash(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func authorizeExecutorArtifact(store ReadStore, p Identity, taskID domain.TaskID, claimID domain.ClaimID) error {
	if p.Executor == nil {
		return p.Validate()
	}
	if err := p.ValidateCapability(identity.TaskArtifactWrite); err != nil {
		return err
	}
	if p.Executor.TaskID != taskID || p.Executor.ClaimID != string(claimID) {
		return forbidden("artifact must belong to the bound Task Claim")
	}
	return authorizeExecutor(store, p, identity.TaskArtifactWrite, p.Executor.WorkItemID)
}

// authorizeExecutorCreation also guards idempotent replays before reading any cached result.
func authorizeExecutorCreation(store ReadStore, p Identity, request any) error {
	if p.Executor == nil {
		return p.Validate()
	}
	switch c := request.(type) {
	case CreateArtifactCommand:
		return authorizeExecutorArtifact(store, p, c.TaskID, c.ClaimID)
	case CreateBlackboardTaskCommand:
		if c.CoordinationClaimID != "" {
			return forbidden("Task Executor cannot consume a Coordination Claim")
		}
		return authorizeExecutor(store, p, identity.BlackboardPlanningWrite, c.WorkItemID)
	case AddBlackboardChildTaskCommand:
		parent, err := store.GetTask(c.ParentTaskID)
		if err != nil {
			return err
		}
		return authorizeExecutor(store, p, identity.BlackboardPlanningWrite, parent.WorkItemID)
	default:
		return ErrForbidden
	}
}
