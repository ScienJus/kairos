package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// DefinitionExecutionContext contains collaboration guidance shared by both modes.
type DefinitionExecutionContext struct {
	Name              string
	Description       string
	AgentInstructions string
	SuggestedTags     []string
}

// WorkflowChoiceOption describes one legal progression choice for the current Task.
type WorkflowChoiceOption struct {
	ID   domain.WorkflowChoiceGroupID
	Kind domain.WorkflowChoiceGroupKind

	// Targets are the selected group's direct Task Definitions.
	Targets []domain.WorkflowTaskDefinition

	// SkippableOptionalTasks are reachable without crossing a Task that must execute.
	// Executors submit only the IDs they intend to skip.
	SkippableOptionalTasks []domain.WorkflowTaskDefinition
}

// WorkflowExecutionContext contains the choices derived from the bound Definition.
type WorkflowExecutionContext struct {
	// UpstreamTasks are ordered nearest-first and include skipped ancestors, so
	// the executor can reach the durable results that feed this Task.
	UpstreamTasks []domain.Task

	ChoiceGroups []WorkflowChoiceOption
}

// BlackboardExecutionContext contains the other Tasks in the current shared
// space. The Task being executed is available separately on TaskExecutionContext.
type BlackboardExecutionContext struct {
	Tasks     []domain.Task
	Relations []domain.TaskRelation
}

// TaskExecutionContext contains the durable context needed to execute one Task.
// Task owns its complete Submission, Review, Failure, and Transition histories.
type TaskExecutionContext struct {
	WorkItem   domain.WorkItem
	Task       domain.Task
	Claims     []domain.Claim
	Definition DefinitionExecutionContext

	Workflow   *WorkflowExecutionContext
	Blackboard *BlackboardExecutionContext
}

// GetTaskExecutionContextQuery identifies the Task and requesting executor.
type GetTaskExecutionContextQuery struct {
	TaskID   domain.TaskID
	Identity Identity
}

// GetTaskExecutionContext returns one executor-facing view without exposing
// Workflow relation planning as a client-authored graph.
func (s *Service) GetTaskExecutionContext(
	ctx context.Context,
	query GetTaskExecutionContextQuery,
) (TaskExecutionContext, error) {
	if strings.TrimSpace(string(query.TaskID)) == "" {
		return TaskExecutionContext{}, invalidCommand("task id is required")
	}
	if err := query.Identity.Validate(); err != nil {
		return TaskExecutionContext{}, err
	}

	var result TaskExecutionContext
	err := s.repository.View(ctx, func(store ReadStore) error {
		task, err := store.GetTask(query.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", query.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if err := identityCanExecute(query.Identity, task); err != nil {
			return err
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		if err := executionContextVisibleTo(query.Identity, task, claims); err != nil {
			return err
		}

		result = TaskExecutionContext{WorkItem: workItem, Task: task, Claims: claims}
		switch workItem.CoordinationMode() {
		case domain.CoordinationModeWorkflow:
			definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
			if err != nil {
				return fmt.Errorf("get workflow definition: %w", err)
			}
			result.Definition = definitionExecutionContext(definition.DefinitionMetadata)
			workflow, err := workflowExecutionContext(definition, task)
			if err != nil {
				return err
			}
			workflow.UpstreamTasks, err = workflowUpstreamTasks(store, workItem.ID, task.ID)
			if err != nil {
				return err
			}
			result.Workflow = &workflow
		case domain.CoordinationModeBlackboard:
			definition, err := store.GetBlackboardDefinition(workItem.Definition.ID, workItem.Definition.Version)
			if err != nil {
				return fmt.Errorf("get blackboard definition: %w", err)
			}
			result.Definition = definitionExecutionContext(definition.DefinitionMetadata)
			tasks, err := store.ListTasks(workItem.ID)
			if err != nil {
				return fmt.Errorf("list blackboard tasks: %w", err)
			}
			otherTasks := make([]domain.Task, 0, len(tasks))
			for _, sharedTask := range tasks {
				if sharedTask.ID != task.ID {
					otherTasks = append(otherTasks, sharedTask)
				}
			}
			relations, err := store.ListTaskRelations(workItem.ID)
			if err != nil {
				return fmt.Errorf("list blackboard relations: %w", err)
			}
			if relations == nil {
				relations = []domain.TaskRelation{}
			}
			result.Blackboard = &BlackboardExecutionContext{Tasks: otherTasks, Relations: relations}
		default:
			return invalidCommand("work item %q has invalid coordination mode", workItem.ID)
		}
		return nil
	})
	if err != nil {
		return TaskExecutionContext{}, err
	}
	return result, nil
}

func workflowUpstreamTasks(
	store ReadStore,
	workItemID domain.WorkItemID,
	taskID domain.TaskID,
) ([]domain.Task, error) {
	tasks, err := store.ListTasks(workItemID)
	if err != nil {
		return nil, fmt.Errorf("list workflow tasks: %w", err)
	}
	relations, err := store.ListTaskRelations(workItemID)
	if err != nil {
		return nil, fmt.Errorf("list workflow relations: %w", err)
	}
	byID := make(map[domain.TaskID]domain.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	visited := map[domain.TaskID]struct{}{taskID: {}}
	queue := []domain.TaskID{taskID}
	var upstream []domain.Task
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, relation := range relations {
			if relation.ToTaskID != current {
				continue
			}
			if _, exists := visited[relation.FromTaskID]; exists {
				continue
			}
			predecessor, exists := byID[relation.FromTaskID]
			if !exists {
				return nil, invalidCommand("relation references missing task %q", relation.FromTaskID)
			}
			visited[predecessor.ID] = struct{}{}
			upstream = append(upstream, predecessor)
			queue = append(queue, predecessor.ID)
		}
	}
	return upstream, nil
}

func definitionExecutionContext(metadata domain.DefinitionMetadata) DefinitionExecutionContext {
	return DefinitionExecutionContext{
		Name:              metadata.Name,
		Description:       metadata.Description,
		AgentInstructions: metadata.AgentInstructions,
		SuggestedTags:     append([]string(nil), metadata.SuggestedTags...),
	}
}

func executionContextVisibleTo(identity Identity, task domain.Task, claims []domain.Claim) error {
	if task.Status != domain.TaskStatusWorking {
		return nil
	}
	if task.ActiveClaimID == nil {
		return conflict("working task %q has no active claim", task.ID)
	}
	claimIndex := findClaim(claims, *task.ActiveClaimID)
	if claimIndex < 0 {
		return conflict("active claim %q does not exist for task %q", *task.ActiveClaimID, task.ID)
	}
	if !sameActor(claims[claimIndex].Executor, identity.Actor) {
		return forbidden("actor does not own active claim %q", *task.ActiveClaimID)
	}
	return nil
}

func workflowExecutionContext(
	definition domain.WorkflowDefinition,
	task domain.Task,
) (WorkflowExecutionContext, error) {
	if task.WorkflowTaskID == nil {
		return WorkflowExecutionContext{}, invalidCommand("task %q has no workflow task id", task.ID)
	}
	compiled, err := definition.CompileGraph()
	if err != nil {
		return WorkflowExecutionContext{}, err
	}
	groups := compiled.GroupsFor(*task.WorkflowTaskID)
	context := WorkflowExecutionContext{ChoiceGroups: make([]WorkflowChoiceOption, 0, len(groups))}
	for _, group := range groups {
		option := WorkflowChoiceOption{ID: group.ID, Kind: group.Kind}
		for _, relationID := range group.RelationIDs {
			relation, exists := workflowRelationDefinition(definition, relationID)
			if !exists {
				return WorkflowExecutionContext{}, invalidCommand("workflow relation %q does not exist", relationID)
			}
			target, exists := workflowTaskDefinition(definition, relation.ToTaskID)
			if !exists {
				return WorkflowExecutionContext{}, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
			}
			option.Targets = append(option.Targets, target)
		}
		if group.Kind == domain.WorkflowChoiceGroupExit {
			candidates, err := reachableOptionalTasks(definition, compiled, group)
			if err != nil {
				return WorkflowExecutionContext{}, err
			}
			option.SkippableOptionalTasks = candidates
		}
		context.ChoiceGroups = append(context.ChoiceGroups, option)
	}
	return context, nil
}

func reachableOptionalTasks(
	definition domain.WorkflowDefinition,
	compiled domain.CompiledWorkflowGraph,
	root domain.WorkflowChoiceGroup,
) ([]domain.WorkflowTaskDefinition, error) {
	queuedRelations := append([]domain.WorkflowRelationID(nil), root.RelationIDs...)
	visited := make(map[domain.WorkflowTaskID]struct{})
	var candidates []domain.WorkflowTaskDefinition
	for len(queuedRelations) > 0 {
		relationID := queuedRelations[0]
		queuedRelations = queuedRelations[1:]
		relation, exists := workflowRelationDefinition(definition, relationID)
		if !exists {
			return nil, invalidCommand("workflow relation %q does not exist", relationID)
		}
		target, exists := workflowTaskDefinition(definition, relation.ToTaskID)
		if !exists {
			return nil, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
		}
		if target.Execution != domain.ExecutionOptional {
			continue
		}
		if _, exists := visited[target.ID]; exists {
			continue
		}
		visited[target.ID] = struct{}{}
		candidates = append(candidates, target)
		exitGroup, exists := workflowExitChoiceGroup(compiled, target.ID)
		if exists {
			queuedRelations = append(queuedRelations, exitGroup.RelationIDs...)
		}
	}
	return candidates, nil
}
