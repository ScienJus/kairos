package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// CreateWorkItemCommand creates one concrete unit of work in a published Definition.
type CreateWorkItemCommand struct {
	Definition  domain.DefinitionBinding
	Identity    Identity
	OperationID string

	Title              string
	Goal               string
	Context            string
	Constraints        string
	AcceptanceCriteria string
	Tags               []string
}

// CreateWorkItem binds a WorkItem to an immutable Definition version.
// Workflow start Tasks are created in the same transaction.
func (s *Service) CreateWorkItem(ctx context.Context, command CreateWorkItemCommand) (domain.WorkItem, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if err := command.Definition.Validate(); err != nil {
		return domain.WorkItem{}, invalidCommand("invalid definition binding: %v", err)
	}

	var created domain.WorkItem
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_work_item", command, &created, func(store WriteStore) error {
		var workflow *domain.WorkflowDefinition
		switch command.Definition.Mode {
		case domain.CoordinationModeWorkflow:
			definition, err := store.GetWorkflowDefinition(command.Definition.ID, command.Definition.Version)
			if err != nil {
				return fmt.Errorf("get workflow definition: %w", err)
			}
			if definition.Binding() != command.Definition {
				return conflict("workflow definition does not match binding")
			}
			if definition.Status != domain.DefinitionStatusPublished {
				return conflict("workflow definition is %s", definition.Status)
			}
			if err := definition.Validate(); err != nil {
				return fmt.Errorf("validate workflow definition: %w", err)
			}
			workflow = &definition
		case domain.CoordinationModeBlackboard:
			definition, err := store.GetBlackboardDefinition(command.Definition.ID, command.Definition.Version)
			if err != nil {
				return fmt.Errorf("get blackboard definition: %w", err)
			}
			if definition.Binding() != command.Definition {
				return conflict("blackboard definition does not match binding")
			}
			if definition.Status != domain.DefinitionStatusPublished {
				return conflict("blackboard definition is %s", definition.Status)
			}
			if err := definition.Validate(); err != nil {
				return fmt.Errorf("validate blackboard definition: %w", err)
			}
		default:
			return invalidCommand("unsupported coordination mode %q", command.Definition.Mode)
		}

		id, err := s.newID("work item id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		workItem := domain.WorkItem{
			ID:                 domain.WorkItemID(id),
			Definition:         command.Definition,
			Status:             domain.WorkItemStatusOpen,
			Title:              strings.TrimSpace(command.Title),
			Goal:               strings.TrimSpace(command.Goal),
			Context:            strings.TrimSpace(command.Context),
			Constraints:        strings.TrimSpace(command.Constraints),
			AcceptanceCriteria: strings.TrimSpace(command.AcceptanceCriteria),
			Tags:               append([]string(nil), command.Tags...),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := workItem.Validate(); err != nil {
			return err
		}
		if err := store.CreateWorkItem(workItem); err != nil {
			return fmt.Errorf("create work item: %w", err)
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemCreated, string(workItem.ID), &actor, ""); err != nil {
			return err
		}

		if workflow != nil {
			definitions := make(map[domain.WorkflowTaskID]domain.WorkflowTaskDefinition, len(workflow.Graph.Tasks))
			for _, definition := range workflow.Graph.Tasks {
				definitions[definition.ID] = definition
			}
			correlationID, err := s.newID("workflow correlation id")
			if err != nil {
				return err
			}
			for position, taskID := range workflow.Graph.StartTaskIDs {
				activationID, err := s.newID("workflow activation id")
				if err != nil {
					return err
				}
				task, err := s.newWorkflowTask(
					workItem.ID,
					definitions[taskID],
					domain.WorkflowTaskActivationID(activationID),
					int64(position),
					now,
				)
				if err != nil {
					return err
				}
				activation := domain.WorkflowTaskActivation{
					ID:             domain.WorkflowTaskActivationID(activationID),
					WorkItemID:     workItem.ID,
					WorkflowTaskID: taskID,
					CorrelationID:  domain.WorkflowCorrelationID(correlationID),
					Status:         domain.WorkflowActivationResolved,
					Outcome:        domain.WorkflowActivationCreated,
					CreatedAt:      now,
					UpdatedAt:      now,
					ResolvedAt:     &now,
				}
				if err := activation.Validate(); err != nil {
					return err
				}
				if err := store.CreateTask(task); err != nil {
					return fmt.Errorf("create workflow start task: %w", err)
				}
				if err := store.CreateWorkflowTaskActivation(activation); err != nil {
					return fmt.Errorf("create workflow start activation: %w", err)
				}
				if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskCreated, string(task.ID), &actor, ""); err != nil {
					return err
				}
			}
		}

		created = workItem
		return nil
	})
	if err != nil {
		return domain.WorkItem{}, err
	}
	return created, nil
}

func (s *Service) newWorkflowTask(
	workItemID domain.WorkItemID,
	definition domain.WorkflowTaskDefinition,
	activationID domain.WorkflowTaskActivationID,
	position int64,
	now time.Time,
) (domain.Task, error) {
	id, err := s.newID("task id")
	if err != nil {
		return domain.Task{}, err
	}
	workflowTaskID := definition.ID
	execution := definition.Execution
	review := definition.ReviewPolicy
	task := domain.Task{
		ID:                   domain.TaskID(id),
		WorkItemID:           workItemID,
		WorkflowTaskID:       &workflowTaskID,
		WorkflowActivationID: &activationID,
		Status:               domain.TaskStatusPending,
		Title:                definition.Title,
		Description:          definition.Description,
		AcceptanceCriteria:   definition.AcceptanceCriteria,
		Executor:             definition.Executor,
		AllowedRoles:         append([]string(nil), definition.AllowedRoles...),
		Tags:                 append([]string(nil), definition.DefaultTags...),
		Execution:            &execution,
		ReviewPolicy:         &review,
		Position:             position,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := task.Validate(domain.CoordinationModeWorkflow); err != nil {
		return domain.Task{}, err
	}
	return task, nil
}

// CompleteBlackboardWorkItemCommand records the explicit completion judgment.
type CompleteBlackboardWorkItemCommand struct {
	WorkItemID  domain.WorkItemID
	Identity    Identity
	OperationID string
	Result      string
}

// CompleteBlackboardWorkItem completes an open Blackboard after all current Tasks end.
func (s *Service) CompleteBlackboardWorkItem(ctx context.Context, command CompleteBlackboardWorkItemCommand) (domain.WorkItem, error) {
	if strings.TrimSpace(string(command.WorkItemID)) == "" {
		return domain.WorkItem{}, invalidCommand("work item id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if strings.TrimSpace(command.Result) == "" {
		return domain.WorkItem{}, invalidCommand("result is required")
	}

	var completed domain.WorkItem
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "complete_blackboard_work_item", command, &completed, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", command.WorkItemID, err)
		}
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			return conflict("work item %q is not a blackboard", workItem.ID)
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list tasks: %w", err)
		}
		for _, task := range tasks {
			if task.Status != domain.TaskStatusCompleted && task.Status != domain.TaskStatusSkipped {
				return conflict("task %q is still %s", task.ID, task.Status)
			}
		}

		now := s.clock.Now()
		workItem.Status = domain.WorkItemStatusCompleted
		workItem.Result = strings.TrimSpace(command.Result)
		workItem.CompletedAt = &now
		workItem.UpdatedAt = now
		workItem.Version++
		if err := workItem.Validate(); err != nil {
			return err
		}
		if err := store.SaveWorkItem(workItem); err != nil {
			return fmt.Errorf("save completed work item: %w", err)
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemCompleted, string(workItem.ID), &actor, workItem.Result); err != nil {
			return err
		}
		completed = workItem
		return nil
	})
	if err != nil {
		return domain.WorkItem{}, err
	}
	return completed, nil
}
