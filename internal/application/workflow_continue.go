package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// ContinueWorkflowCommand is a Human management operation, never an Agent retry.
// Version protects against stale pages and duplicate recovery requests.
type ContinueWorkflowCommand struct {
	WorkItemID              domain.WorkItemID
	Identity                Identity
	Version                 int64
	MaxTaskInstancesPerNode int
	Instructions            string
}

// ContinueWorkflow creates replacement attempts for failed/interrupted Tasks and
// delivers missing inputs from committed decisions. Successful branches and
// pending reviews remain valid. Counters and ended Claims are never reset; any
// capacity conflict rolls back the entire recovery transaction.
func (s *Service) ContinueWorkflow(ctx context.Context, command ContinueWorkflowCommand) (domain.WorkItem, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorHuman {
		return domain.WorkItem{}, forbidden("only a human can continue a workflow")
	}
	if strings.TrimSpace(string(command.WorkItemID)) == "" || command.Version < 0 {
		return domain.WorkItem{}, invalidCommand("work item id and a nonnegative version are required")
	}
	if command.MaxTaskInstancesPerNode < 0 || command.MaxTaskInstancesPerNode > domain.MaxWorkflowTaskInstancesPerNode {
		return domain.WorkItem{}, invalidCommand("max_task_instances_per_node must be between 0 and %d", domain.MaxWorkflowTaskInstancesPerNode)
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
			return conflict("work item version changed; refresh before continuing")
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
		limit := command.MaxTaskInstancesPerNode
		if limit == 0 {
			limit = effectiveWorkflowTaskInstanceLimit(work, definition)
		}
		if limit < effectiveWorkflowTaskInstanceLimit(work, definition) {
			return invalidCommand("max_task_instances_per_node cannot decrease the current per-node limit")
		}
		if work.Failure != nil && work.Failure.Kind == domain.FailureWorkflowTaskInstanceLimit && limit <= work.Failure.TaskInstances {
			return invalidCommand("max_task_instances_per_node must exceed the blocked node's task instance count (%d)", work.Failure.TaskInstances)
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
		work.WorkflowMaxTaskInstancesPerNode = limit
		work.UpdatedAt = s.clock.Now()
		work.Version++
		if err := store.SaveWorkItem(work); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, work.ID, nil, domain.WorkItemEventWorkItemContinued, string(work.ID), &actor, fmt.Sprintf("continued with per-node max_task_instances_per_node %d", limit)); err != nil {
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
					return conflict("recovery still reaches a task instance limit; choose a higher per-node limit")
				}
			}
		}
		after, err := store.ListWorkflowTaskActivations(work.ID)
		if err != nil {
			return err
		}
		// Avoid continuing a malformed record with no blocked work.
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
			return conflict("no unfinished workflow work could be continued; start over as a new WorkItem")
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
