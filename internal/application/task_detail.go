package application

import (
	"context"
	"fmt"

	"github.com/ScienJus/kairos/internal/domain"
)

type GetTaskDetailQuery struct {
	TaskID   domain.TaskID
	Identity Identity
}

type TaskCapabilities struct {
	CanClaim     bool
	CanSubmit    bool
	CanRelease   bool
	CanFail      bool
	CanReview    bool
	CanSkip      bool
	CanDecompose bool
	CanAddChild  bool
}

type TaskHistory struct {
	Claims              []domain.Claim
	Submissions         []domain.TaskSubmission
	Reviews             []domain.Review
	Failures            []domain.TaskFailure
	TransitionDecisions []domain.TransitionDecision
}

type TaskDetail struct {
	Task           domain.Task
	Responsibility TaskResponsibility
	Outcome        TaskOutcome
	CurrentReview  *domain.Review
	History        TaskHistory
	Capabilities   TaskCapabilities
}

func (s *Service) GetTaskDetail(ctx context.Context, query GetTaskDetailQuery) (TaskDetail, error) {
	if err := query.Identity.Validate(); err != nil {
		return TaskDetail{}, invalidCommand("invalid identity: %v", err)
	}
	var result TaskDetail
	err := s.repository.View(ctx, func(store ReadStore) error {
		task, err := store.GetTask(query.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", query.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		result.Task = normalizeTaskCollections(task)
		result.Responsibility, result.Outcome = projectTaskLifecycle(task, claims)
		result.History = TaskHistory{Claims: normalizeClaims(claims), Submissions: normalizeSubmissions(task.Submissions), Reviews: normalizeReviews(task.Reviews), Failures: normalizeFailures(task.Failures), TransitionDecisions: normalizeTransitionDecisions(task.TransitionDecisions)}
		if len(task.Reviews) > 0 {
			review := task.Reviews[len(task.Reviews)-1]
			result.CurrentReview = &review
		}
		workItemOpen := workItem.Status == domain.WorkItemStatusOpen
		canExecute := identityCanExecute(query.Identity, task) == nil
		claimable := workItemOpen && task.Status == domain.TaskStatusPending && task.ActiveClaimID == nil && canExecute
		if claimable && workItem.CoordinationMode() == domain.CoordinationModeWorkflow {
			claimable, err = workflowTaskEligible(store, workItem, task)
			if err != nil {
				return err
			}
		}
		result.Capabilities.CanClaim = claimable
		for _, claim := range claims {
			if workItemOpen && claim.EndedAt == nil && task.ActiveClaimID != nil && claim.ID == *task.ActiveClaimID && sameActor(claim.Executor, query.Identity.Actor) {
				result.Capabilities.CanSubmit = task.Status == domain.TaskStatusWorking
				result.Capabilities.CanRelease = task.Status == domain.TaskStatusWorking
				result.Capabilities.CanFail = task.Status == domain.TaskStatusWorking
				result.Capabilities.CanDecompose = workItem.CoordinationMode() == domain.CoordinationModeBlackboard && validateBlackboardTaskDecomposition(workItem, task, claims, query.Identity, claim.ID) == nil
			}
		}
		result.Capabilities.CanReview = workItemOpen && query.Identity.Actor.Kind == domain.ActorHuman && result.CurrentReview != nil && result.CurrentReview.Status == domain.ReviewStatusPending && task.Status == domain.TaskStatusInReview && task.ActiveClaimID == nil
		result.Capabilities.CanSkip = workItemOpen && workItem.CoordinationMode() == domain.CoordinationModeBlackboard && task.Status == domain.TaskStatusPending && task.ActiveClaimID == nil
		result.Capabilities.CanAddChild = workItemOpen && workItem.CoordinationMode() == domain.CoordinationModeBlackboard && task.Status == domain.TaskStatusWaitingChildren && task.DecomposedAt != nil
		return nil
	})
	return result, err
}

func normalizeClaims(v []domain.Claim) []domain.Claim {
	if v == nil {
		return []domain.Claim{}
	}
	return v
}
func normalizeSubmissions(v []domain.TaskSubmission) []domain.TaskSubmission {
	if v == nil {
		return []domain.TaskSubmission{}
	}
	return v
}
func normalizeReviews(v []domain.Review) []domain.Review {
	if v == nil {
		return []domain.Review{}
	}
	return v
}
func normalizeFailures(v []domain.TaskFailure) []domain.TaskFailure {
	if v == nil {
		return []domain.TaskFailure{}
	}
	return v
}
func normalizeTransitionDecisions(v []domain.TransitionDecision) []domain.TransitionDecision {
	if v == nil {
		return []domain.TransitionDecision{}
	}
	return v
}
