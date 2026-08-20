package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// FindWorkQuery describes one actor's candidate query.
type FindWorkQuery struct {
	Identity Identity
	Tags     []string
	Limit    int
}

// FindWork returns visible Tasks and Blackboards that need planning or a lifecycle decision.
func (s *Service) FindWork(ctx context.Context, query FindWorkQuery) ([]WorkCandidate, error) {
	if err := query.Identity.Validate(); err != nil {
		return nil, err
	}
	if query.Limit < 0 {
		return nil, invalidCommand("limit must not be negative")
	}
	for _, tag := range query.Tags {
		if strings.TrimSpace(tag) == "" {
			return nil, invalidCommand("tags must not contain empty values")
		}
	}

	result := make([]WorkCandidate, 0)
	err := s.repository.View(ctx, func(store ReadStore) error {
		atLimit := func() bool { return query.Limit > 0 && len(result) >= query.Limit }

		workItems, err := store.ListBlackboardsAwaitingLifecycleDecision()
		if err != nil {
			return fmt.Errorf("list blackboards for completion or acceptance: %w", err)
		}
	lifecyclePriority:
		for _, status := range []domain.WorkItemStatus{
			domain.WorkItemStatusAwaitingAgentAcceptance,
			domain.WorkItemStatusOpen,
		} {
			for _, workItem := range workItems {
				if workItem.Status != status || !containsAll(workItem.Tags, query.Tags) {
					continue
				}
				kind := WorkCandidateBlackboardCompletion
				if status == domain.WorkItemStatusAwaitingAgentAcceptance {
					if query.Identity.Actor.Kind != domain.ActorAgent {
						continue
					}
					kind = WorkCandidateWorkItemAcceptance
				}
				result = append(result, WorkCandidate{Kind: kind, WorkItem: workItem})
				if atLimit() {
					break lifecyclePriority
				}
			}
		}

		if !atLimit() {
			candidates, err := store.ListOpenTasks()
			if err != nil {
				return fmt.Errorf("list open tasks: %w", err)
			}
			for _, candidate := range candidates {
				if candidate.Kind != WorkCandidateTask {
					continue
				}
				if candidate.WorkItem.Status != domain.WorkItemStatusOpen || candidate.Task == nil || candidate.Task.Status != domain.TaskStatusPending {
					continue
				}
				if candidate.WorkItem.CoordinationMode() == domain.CoordinationModeWorkflow {
					eligible, err := workflowTaskEligible(store, candidate.WorkItem, *candidate.Task)
					if err != nil {
						return err
					}
					if !eligible {
						continue
					}
				}
				if err := identityCanExecute(query.Identity, *candidate.Task); err != nil {
					if errorsIsForbidden(err) {
						continue
					}
					return err
				}
				// Workflow eligibility is determined by graph state and role. Tags are
				// descriptive metadata for Workflow tasks, not an execution filter.
				if candidate.WorkItem.CoordinationMode() != domain.CoordinationModeWorkflow && !containsAll(candidate.Task.Tags, query.Tags) {
					continue
				}
				result = append(result, candidate)
				if atLimit() {
					break
				}
			}
		}

		if !atLimit() {
			emptyBlackboards, err := store.ListEmptyBlackboards()
			if err != nil {
				return fmt.Errorf("list empty blackboards: %w", err)
			}
			for _, workItem := range emptyBlackboards {
				if !containsAll(workItem.Tags, query.Tags) {
					continue
				}
				result = append(result, WorkCandidate{Kind: WorkCandidateEmptyBlackboard, WorkItem: workItem})
				if atLimit() {
					break
				}
			}
		}

		bindings := make([]domain.DefinitionBinding, 0, len(result))
		seenBindings := make(map[domain.DefinitionBinding]struct{}, len(result))
		for _, candidate := range result {
			binding := candidate.WorkItem.Definition
			if _, exists := seenBindings[binding]; exists {
				continue
			}
			seenBindings[binding] = struct{}{}
			bindings = append(bindings, binding)
		}
		definitions, err := store.GetDefinitionMetadata(bindings)
		if err != nil {
			return fmt.Errorf("get candidate definitions: %w", err)
		}
		for index := range result {
			binding := result[index].WorkItem.Definition
			metadata, exists := definitions[binding]
			if !exists {
				return fmt.Errorf("%w: definition %q version %d mode %q", ErrNotFound, binding.ID, binding.Version, binding.Mode)
			}
			result[index].WorkItem = normalizeWorkItemCollections(result[index].WorkItem)
			if result[index].Task != nil {
				task := normalizeTaskCollections(*result[index].Task)
				result[index].Task = &task
			}
			result[index].Definition = normalizeDefinitionContext(definitionExecutionContext(metadata))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func blackboardTasksConverged(tasks []domain.Task) bool {
	for _, task := range tasks {
		if task.Status != domain.TaskStatusCompleted && task.Status != domain.TaskStatusSkipped {
			return false
		}
	}
	return true
}

func containsAll(values, required []string) bool {
	for _, value := range required {
		if !slices.Contains(values, value) {
			return false
		}
	}
	return true
}

func errorsIsForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

// ClaimTaskCommand establishes execution responsibility for one pending Task.
type ClaimTaskCommand struct {
	TaskID       domain.TaskID
	Identity     Identity
	OperationID  string
	LeaseSeconds int64
}

// ClaimTask atomically claims one pending Task.
func (s *Service) ClaimTask(ctx context.Context, command ClaimTaskCommand) (domain.Claim, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" {
		return domain.Claim{}, invalidCommand("task id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Claim{}, err
	}

	var created domain.Claim
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "claim_task", command, &created, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		if task.Status != domain.TaskStatusPending || task.ActiveClaimID != nil {
			return conflict("task %q is not claimable", task.ID)
		}
		if workItem.CoordinationMode() == domain.CoordinationModeWorkflow {
			eligible, err := workflowTaskEligible(store, workItem, task)
			if err != nil {
				return err
			}
			if !eligible {
				return conflict("workflow dependencies for task %q are not complete", task.ID)
			}
		}
		if err := identityCanExecute(command.Identity, task); err != nil {
			return err
		}

		id, err := s.newID("claim id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		claimID := domain.ClaimID(id)
		claim := domain.Claim{
			ID:        claimID,
			TaskID:    task.ID,
			Executor:  command.Identity.Actor,
			ClaimedAt: now,
		}
		if command.Identity.Actor.Kind == domain.ActorAgent {
			lease := normalizeClaimLease(command.LeaseSeconds, s.claimLease)
			claim.LastHeartbeatAt = now
			claim.LeaseUntil = now.Add(lease)
			claim.LeaseSeconds = int64(lease / time.Second)
		} else if command.LeaseSeconds != 0 {
			return invalidCommand("lease_seconds is only valid for agent claims")
		}
		task.Status = domain.TaskStatusWorking
		task.ActiveClaimID = &claimID
		task.Version++
		task.UpdatedAt = now

		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		claims = append(claims, claim)
		if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
			return err
		}
		if err := store.CreateClaim(claim); err != nil {
			return fmt.Errorf("create claim: %w", err)
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save task: %w", err)
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskClaimed, string(claim.ID), &actor, ""); err != nil {
			return err
		}
		created = claim
		return nil
	})
	if err != nil {
		return domain.Claim{}, err
	}
	return created, nil
}

func workflowTaskEligible(store ReadStore, workItem domain.WorkItem, task domain.Task) (bool, error) {
	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return false, fmt.Errorf("list workflow tasks: %w", err)
	}
	relations, err := store.ListTaskRelations(workItem.ID)
	if err != nil {
		return false, fmt.Errorf("list workflow relations: %w", err)
	}
	byID := make(map[domain.TaskID]domain.Task, len(tasks))
	for _, existing := range tasks {
		byID[existing.ID] = existing
	}
	for _, relation := range relations {
		if relation.ToTaskID != task.ID {
			continue
		}
		predecessor, ok := byID[relation.FromTaskID]
		if !ok {
			return false, invalidCommand("relation references missing task %q", relation.FromTaskID)
		}
		if predecessor.Status != domain.TaskStatusCompleted && predecessor.Status != domain.TaskStatusSkipped {
			return false, nil
		}
	}
	return true, nil
}
