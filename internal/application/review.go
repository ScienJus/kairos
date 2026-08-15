package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// DecideReviewCommand records a human decision on the latest pending Review.
type DecideReviewCommand struct {
	TaskID      domain.TaskID
	ReviewID    domain.ReviewID
	Identity    Identity
	OperationID string
	Decision    domain.ReviewStatus
	Feedback    string
}

// DecideReview approves a Submission or reopens its Task for another Claim.
func (s *Service) DecideReview(ctx context.Context, command DecideReviewCommand) (domain.Review, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ReviewID)) == "" {
		return domain.Review{}, invalidCommand("task id and review id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Review{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorHuman {
		return domain.Review{}, forbidden("review decisions require a human identity")
	}
	if command.Decision != domain.ReviewStatusApproved && command.Decision != domain.ReviewStatusRejected {
		return domain.Review{}, invalidCommand("review decision must be approved or rejected")
	}

	var decided domain.Review
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "decide_review", command, &decided, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if workItem.Status != domain.WorkItemStatusOpen || task.Status != domain.TaskStatusInReview || task.ActiveClaimID != nil {
			return conflict("task %q is not awaiting review", task.ID)
		}
		if len(task.Reviews) == 0 || task.Reviews[len(task.Reviews)-1].ID != command.ReviewID {
			return conflict("review %q is not the current review", command.ReviewID)
		}
		review := task.Reviews[len(task.Reviews)-1]
		if review.Status != domain.ReviewStatusPending {
			return conflict("review %q is already %s", review.ID, review.Status)
		}

		now := s.clock.Now()
		reviewerID := command.Identity.Actor.ID
		review.Status = command.Decision
		review.DecidedBy = &reviewerID
		review.DecidedAt = &now
		review.Feedback = strings.TrimSpace(command.Feedback)
		if err := review.Validate(); err != nil {
			return err
		}
		task.Reviews[len(task.Reviews)-1] = review
		task.UpdatedAt = now
		task.Version++
		skipReview := review.SubmissionID == nil

		if review.Status == domain.ReviewStatusRejected {
			task.Status = domain.TaskStatusPending
			task.CompletedAt = nil
		} else {
			task.Status = domain.TaskStatusCompleted
			if skipReview {
				task.Status = domain.TaskStatusSkipped
			}
			task.CompletedAt = &now
			if workItem.CoordinationMode() == domain.CoordinationModeWorkflow && len(task.TransitionDecisions) > 0 {
				decision := &task.TransitionDecisions[len(task.TransitionDecisions)-1]
				if decision.AppliedAt != nil {
					return conflict("transition decision %q is already applied", decision.ID)
				}
				decision.AppliedAt = &now
			}
		}

		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
			return err
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save reviewed task: %w", err)
		}
		if skipReview {
			if task.WorkflowActivationID == nil {
				return invalidCommand("reviewed optional task %q has no workflow activation", task.ID)
			}
			activation, err := store.GetWorkflowTaskActivation(*task.WorkflowActivationID)
			if err != nil {
				return fmt.Errorf("get reviewed skip activation: %w", err)
			}
			activation.Outcome = domain.WorkflowActivationCreated
			if review.Status == domain.ReviewStatusApproved {
				activation.Outcome = domain.WorkflowActivationSkipped
			}
			activation.UpdatedAt = now
			if err := activation.Validate(); err != nil {
				return err
			}
			if err := store.SaveWorkflowTaskActivation(activation); err != nil {
				return fmt.Errorf("save reviewed skip activation: %w", err)
			}
		}
		actor := command.Identity.Actor
		eventType := domain.WorkItemEventReviewApproved
		if review.Status == domain.ReviewStatusRejected {
			eventType = domain.WorkItemEventReviewRejected
		}
		if err := s.appendEvent(store, workItem.ID, &task.ID, eventType, string(review.ID), &actor, review.Feedback); err != nil {
			return err
		}
		if review.Status == domain.ReviewStatusApproved {
			taskEvent := domain.WorkItemEventTaskCompleted
			entityID := string(review.ID)
			if skipReview {
				taskEvent = domain.WorkItemEventTaskSkipped
			} else {
				entityID = string(*review.SubmissionID)
			}
			if err := s.appendEvent(store, workItem.ID, &task.ID, taskEvent, entityID, &actor, ""); err != nil {
				return err
			}
			if workItem.CoordinationMode() == domain.CoordinationModeWorkflow && len(task.TransitionDecisions) > 0 {
				decision := task.TransitionDecisions[len(task.TransitionDecisions)-1]
				if err := s.applyWorkflowDecision(store, &workItem, task, decision); err != nil {
					return err
				}
			}
			if err := s.completeWorkflowIfDone(store, &workItem, &actor); err != nil {
				return err
			}
		}
		decided = review
		return nil
	})
	if err != nil {
		return domain.Review{}, err
	}
	return decided, nil
}
