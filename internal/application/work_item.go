package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// CreateWorkItemCommand creates one concrete unit of work using the latest published Definition.
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

// CreateWorkItem resolves the latest published version and binds the WorkItem to it.
// Workflow start Tasks are created in the same transaction.
func (s *Service) CreateWorkItem(ctx context.Context, command CreateWorkItemCommand) (domain.WorkItem, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkItem{}, err
	}
	if strings.TrimSpace(string(command.Definition.ID)) == "" || !command.Definition.Mode.Valid() {
		return domain.WorkItem{}, invalidCommand("definition id and mode are required")
	}

	var created domain.WorkItem
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_work_item", command, &created, func(store WriteStore) error {
		var workflow *domain.WorkflowDefinition
		var binding domain.DefinitionBinding
		switch command.Definition.Mode {
		case domain.CoordinationModeWorkflow:
			definitions, err := store.ListWorkflowDefinitions()
			if err != nil {
				return fmt.Errorf("list workflow definitions: %w", err)
			}
			definition, ok := latestPublishedWorkflowDefinition(definitions, command.Definition.ID)
			if !ok {
				return fmt.Errorf("%w: published workflow definition %q", ErrNotFound, command.Definition.ID)
			}
			if err := definition.Validate(); err != nil {
				return fmt.Errorf("validate workflow definition: %w", err)
			}
			workflow = &definition
			binding = definition.Binding()
		case domain.CoordinationModeBlackboard:
			definitions, err := store.ListBlackboardDefinitions()
			if err != nil {
				return fmt.Errorf("list blackboard definitions: %w", err)
			}
			definition, ok := latestPublishedBlackboardDefinition(definitions, command.Definition.ID)
			if !ok {
				return fmt.Errorf("%w: published blackboard definition %q", ErrNotFound, command.Definition.ID)
			}
			if err := definition.Validate(); err != nil {
				return fmt.Errorf("validate blackboard definition: %w", err)
			}
			binding = definition.Binding()
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
			Definition:         binding,
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

func latestPublishedWorkflowDefinition(definitions []domain.WorkflowDefinition, id domain.DefinitionID) (domain.WorkflowDefinition, bool) {
	var latest domain.WorkflowDefinition
	found := false
	for _, definition := range definitions {
		if definition.ID == id && definition.Status == domain.DefinitionStatusPublished && (!found || definition.Version > latest.Version) {
			latest, found = definition, true
		}
	}
	return latest, found
}

func latestPublishedBlackboardDefinition(definitions []domain.BlackboardDefinition, id domain.DefinitionID) (domain.BlackboardDefinition, bool) {
	var latest domain.BlackboardDefinition
	found := false
	for _, definition := range definitions {
		if definition.ID == id && definition.Status == domain.DefinitionStatusPublished && (!found || definition.Version > latest.Version) {
			latest, found = definition, true
		}
	}
	return latest, found
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

func (s *Service) completeWorkItemIfDone(
	store WriteStore,
	workItem *domain.WorkItem,
	actor *domain.ActorRef,
) error {
	if workItem.Status != domain.WorkItemStatusOpen {
		return nil
	}
	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return fmt.Errorf("list tasks for completion: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}
	for _, task := range tasks {
		if task.Status != domain.TaskStatusCompleted && task.Status != domain.TaskStatusSkipped {
			return nil
		}
	}

	if workItem.CoordinationMode() == domain.CoordinationModeWorkflow {
		definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
		if err != nil {
			return fmt.Errorf("get workflow definition for completion: %w", err)
		}
		for _, task := range tasks {
			groups, err := workflowChoiceGroups(definition, task)
			if err != nil {
				return err
			}
			if len(groups) > 0 && !hasAppliedDecision(task) {
				return nil
			}
		}
		activations, err := store.ListWorkflowTaskActivations(workItem.ID)
		if err != nil {
			return fmt.Errorf("list workflow activations for completion: %w", err)
		}
		for _, activation := range activations {
			if activation.Status == domain.WorkflowActivationWaiting {
				return nil
			}
		}
	}
	return s.completeWorkItem(store, workItem, actor, summarizeWorkItemResult(tasks))
}

func summarizeWorkItemResult(tasks []domain.Task) string {
	type taskResult struct {
		title  string
		result string
	}
	results := make([]taskResult, 0, len(tasks))
	for _, task := range tasks {
		if task.Status != domain.TaskStatusCompleted || len(task.Submissions) == 0 {
			continue
		}
		latest := task.Submissions[len(task.Submissions)-1]
		results = append(results, taskResult{title: task.Title, result: latest.Result})
	}
	if len(results) == 0 {
		return ""
	}
	if len(results) == 1 {
		return results[0].result
	}
	sections := make([]string, 0, len(results))
	for _, result := range results {
		sections = append(sections, fmt.Sprintf("%s\n%s", result.title, result.result))
	}
	return strings.Join(sections, "\n\n")
}

func (s *Service) completeWorkItem(
	store WriteStore,
	workItem *domain.WorkItem,
	actor *domain.ActorRef,
	result string,
) error {
	now := s.clock.Now()
	workItem.Status = domain.WorkItemStatusCompleted
	workItem.Result = strings.TrimSpace(result)
	workItem.CompletedAt = &now
	workItem.UpdatedAt = now
	workItem.Version++
	if err := workItem.Validate(); err != nil {
		return err
	}
	if err := store.SaveWorkItem(*workItem); err != nil {
		return fmt.Errorf("save completed work item: %w", err)
	}
	return s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemCompleted, string(workItem.ID), actor, workItem.Result)
}

// CompleteBlackboardWorkItemCommand directly completes an empty Blackboard.
type CompleteBlackboardWorkItemCommand struct {
	WorkItemID  domain.WorkItemID
	Identity    Identity
	OperationID string
	Result      string
}

// CompleteBlackboardWorkItem completes an empty Blackboard without creating a Task.
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
		if len(tasks) != 0 {
			return conflict("non-empty blackboard %q completes from its task states", workItem.ID)
		}

		actor := command.Identity.Actor
		if err := s.completeWorkItem(store, &workItem, &actor, command.Result); err != nil {
			return err
		}
		completed = workItem
		return nil
	})
	if err != nil {
		return domain.WorkItem{}, err
	}
	return normalizeWorkItemCollections(completed), nil
}
