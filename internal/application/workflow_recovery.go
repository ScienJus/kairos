package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// ResumeWorkflowCommand is a Human management operation, never an Agent retry.
// Version protects against stale pages and duplicate recovery requests.
type ResumeWorkflowCommand struct {
	WorkItemID        domain.WorkItemID
	Identity          Identity
	Version           int64
	MaxTaskExecutions int
	Instructions      string
}

// ResumeWorkflow creates replacement attempts for failed/interrupted Tasks and
// delivers missing inputs from committed decisions. Successful branches and
// pending reviews remain valid. Counters and ended Claims are never reset; any
// capacity conflict rolls back the entire recovery transaction.
func (s *Service) ResumeWorkflow(ctx context.Context, command ResumeWorkflowCommand) (domain.WorkItem, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorHuman {
		return domain.WorkItem{}, forbidden("only a human can resume a workflow")
	}
	if strings.TrimSpace(string(command.WorkItemID)) == "" || command.Version < 0 {
		return domain.WorkItem{}, invalidCommand("work item id and a nonnegative version are required")
	}
	if command.MaxTaskExecutions < 0 || command.MaxTaskExecutions > domain.MaxWorkflowTaskExecutions {
		return domain.WorkItem{}, invalidCommand("max_task_executions must be between 0 and %d", domain.MaxWorkflowTaskExecutions)
	}
	if len(command.Instructions) > domain.MaxHistoryTextBytes {
		return domain.WorkItem{}, invalidCommand("instructions exceed %d UTF-8 bytes", domain.MaxHistoryTextBytes)
	}
	var result domain.WorkItem
	err := s.repository.Update(ctx, func(store WriteStore) error {
		work, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return err
		}
		if work.Version != command.Version {
			return conflict("work item version changed; refresh before resuming")
		}
		tasks, err := store.ListTasks(work.ID)
		if err != nil {
			return err
		}
		if !workflowRecoveryAvailable(work, tasks) {
			return conflict("only a Workflow with current failures can be continued")
		}
		definition, err := store.GetWorkflowDefinition(work.Definition.ID, work.Definition.Version)
		if err != nil {
			return err
		}
		limit := command.MaxTaskExecutions
		if limit == 0 {
			limit = effectiveWorkflowExecutionLimit(work, definition)
		}
		if limit < effectiveWorkflowExecutionLimit(work, definition) {
			return invalidCommand("max_task_executions cannot decrease the current per-node limit")
		}
		if work.Failure != nil && work.Failure.Kind == domain.FailureWorkflowExecutionLimit && limit <= work.Failure.Executions {
			return invalidCommand("max_task_executions must exceed the blocked node's execution count (%d)", work.Failure.Executions)
		}
		claims, err := store.ListClaimsByWorkItem(work.ID)
		if err != nil {
			return err
		}
		recoveryTasks := workflowRecoveryTasks(work, tasks, claims)
		failureReason := ""
		if work.Failure != nil {
			failureReason = work.Failure.Message
		}
		work.Status = domain.WorkItemStatusOpen
		work.Failure = nil
		work.RecoveryInstructions = command.Instructions
		work.WorkflowMaxTaskExecutions = limit
		work.UpdatedAt = s.clock.Now()
		work.Version++
		if err := store.SaveWorkItem(work); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, work.ID, nil, domain.WorkItemEventWorkItemResumed, string(work.ID), &actor, fmt.Sprintf("resumed with per-node max_task_executions %d", limit)); err != nil {
			return err
		}
		before, err := store.ListWorkflowTaskActivations(work.ID)
		if err != nil {
			return err
		}
		state, err := newWorkflowRetryState(store, work, definition, tasks, before)
		if err != nil {
			return err
		}
		// Replace failed attempts and attempts interrupted by whole-workflow
		// failure. Never rerun successful Tasks or pending Human Reviews.
		tasks = currentWorkflowAttempts(tasks)
		retried := false
		for _, task := range recoveryTasks {
			if _, err := s.retryWorkflowTask(store, state, work, task, actor, command.Instructions, failureReason); err != nil {
				if limit, ok := err.(*workflowRetryLimitError); ok {
					return conflict("%s", limit)
				}
				return err
			}
			retried = true
		}
		delivered := deliveredWorkflowInputs(before)
		previousStatus := make(map[domain.WorkflowTaskActivationID]domain.WorkflowActivationStatus, len(before))
		for _, activation := range before {
			previousStatus[activation.ID] = activation.Status
		}
		compiled, err := definition.CompileGraph()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			for _, decision := range task.TransitionDecisions {
				if decision.AppliedAt == nil {
					continue
				}
				// Skip fully delivered decisions without rerunning their routing.
				pending := false
				if task.WorkflowTaskID == nil {
					return conflict("workflow task is missing its node identity")
				}
				for _, group := range compiled.GroupsFor(*task.WorkflowTaskID) {
					if group.ID != decision.ChoiceGroupID {
						continue
					}
					for _, relationID := range group.RelationIDs {
						if !delivered[decision.ID][relationID] {
							pending = true
							break
						}
					}
				}
				if !pending {
					continue
				}
				if err := s.propagateWorkflowDecision(store, &work, task, decision, delivered[decision.ID], false); err != nil {
					return err
				}
				if work.Status == domain.WorkItemStatusFailed {
					return conflict("recovery still reaches an execution limit; choose a higher per-node limit")
				}
			}
		}
		after, err := store.ListWorkflowTaskActivations(work.ID)
		if err != nil {
			return err
		}
		// Avoid resuming a malformed/legacy record with no blocked work to resume.
		progressed := retried || len(after) > len(before)
		for _, activation := range after {
			if status, exists := previousStatus[activation.ID]; exists && status != activation.Status {
				progressed = true
			}
		}
		if !progressed {
			for _, task := range tasks {
				if task.Status == domain.TaskStatusPending || task.Status == domain.TaskStatusInReview {
					progressed = true
					break
				}
			}
		}
		if !progressed {
			return conflict("no unfinished workflow work could be continued; restart as a new WorkItem")
		}
		if err := s.completeWorkItemIfDone(store, &work, &actor); err != nil {
			return err
		}
		result = work
		return nil
	})
	if err != nil {
		return domain.WorkItem{}, err
	}
	return normalizeWorkItemCollections(result), nil
}
