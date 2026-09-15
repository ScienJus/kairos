package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

type RestartWorkflowCommand struct {
	WorkItemID   domain.WorkItemID
	Identity     Identity
	Version      int64
	OperationID  string
	Instructions string
}

// RestartWorkflow copies intent and a bounded failure summary, never execution
// state. The source binding is pinned even when a newer Definition is published.
func (s *Service) RestartWorkflow(ctx context.Context, command RestartWorkflowCommand) (domain.WorkItem, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if command.Identity.Actor.Kind != domain.ActorHuman {
		return domain.WorkItem{}, forbidden("only a human can restart a workflow")
	}
	if command.WorkItemID == "" || command.Version < 0 || strings.TrimSpace(command.OperationID) == "" {
		return domain.WorkItem{}, invalidCommand("work item id, version and Idempotency-Key are required")
	}
	if len(command.Instructions) > domain.MaxHistoryTextBytes {
		return domain.WorkItem{}, invalidCommand("instructions exceed %d UTF-8 bytes", domain.MaxHistoryTextBytes)
	}
	var created domain.WorkItem
	err := s.replayableCreate(ctx, command.Identity, command.OperationID, "restart_workflow", command, &created, func(store WriteStore) error {
		source, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return err
		}
		tasks, err := store.ListTasks(source.ID)
		if err != nil {
			return err
		}
		if !workflowRecoveryAvailable(source, tasks) {
			return conflict("only a Workflow with current failures can start over")
		}
		if source.Version != command.Version {
			return conflict("source version changed; refresh before restarting")
		}
		definition, err := store.GetWorkflowDefinition(source.Definition.ID, source.Definition.Version)
		if err != nil {
			return err
		}
		id, err := s.newID("work item id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		work := domain.WorkItem{ID: domain.WorkItemID(id), Definition: source.Definition, Status: domain.WorkItemStatusOpen, AcceptanceMode: source.AcceptanceMode, Title: source.Title, Goal: source.Goal, Context: source.Context, Constraints: source.Constraints, AcceptanceCriteria: source.AcceptanceCriteria, Tags: append([]string{}, source.Tags...), CreatedAt: now, UpdatedAt: now, RestartOfWorkItemID: &source.ID, RestartContext: workflowRestartSummary(source, tasks), RecoveryInstructions: command.Instructions, WorkflowMaxTaskExecutions: source.WorkflowMaxTaskExecutions}
		if err := work.Validate(); err != nil {
			return err
		}
		if err := store.CreateWorkItem(work); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, work.ID, nil, domain.WorkItemEventWorkItemCreated, string(work.ID), &actor, "restart of work item "+string(source.ID)); err != nil {
			return err
		}
		if err := s.createWorkflowStartTasks(store, work, definition, actor); err != nil {
			return err
		}
		if source.Status == domain.WorkItemStatusOpen {
			// Starting over replaces an Open Workflow with failed Tasks as well.
			// End its other Claims in the same transaction before publishing the copy.
			if err := s.failWorkItem(store, &source, nil, &actor, "Human started over after Task failure; replacement WorkItem "+string(work.ID), now); err != nil {
				return err
			}
		}
		// A source revision fences concurrent restart/continue requests. The source
		// stays Failed and retains its history, including Claims ended above.
		source.Version++
		source.UpdatedAt = now
		if err := store.SaveWorkItem(source); err != nil {
			return err
		}
		if err := s.appendEvent(store, source.ID, nil, domain.WorkItemEventWorkItemRestarted, string(source.ID), &actor, "restarted as work item "+string(work.ID)); err != nil {
			return err
		}
		created = work
		return nil
	})
	return normalizeWorkItemCollections(created), err
}

// Restart summaries carry only this failure; execution history stays on source.
func workflowRestartSummary(source domain.WorkItem, tasks []domain.Task) string {
	var summary strings.Builder
	fmt.Fprintf(&summary, "WorkItem %s failed. Restart uses the same Workflow version and initial nodes.\n", source.ID)
	if source.Failure != nil {
		fmt.Fprintf(&summary, "Workflow failure: %s\n", source.Failure.Message)
	}
	for _, task := range currentWorkflowAttempts(tasks) {
		if task.Status == domain.TaskStatusFailed && len(task.Failures) > 0 {
			fmt.Fprintf(&summary, "Task %s failed: %s\n", task.ID, task.Failures[len(task.Failures)-1].Reason)
		}
	}
	return recoverySummary(summary.String())
}
