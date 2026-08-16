package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// WorkItemExecutionContext contains a durable WorkItem view that remains
// addressable after it leaves the open candidate set.
type WorkItemExecutionContext struct {
	WorkItem   domain.WorkItem
	Definition DefinitionExecutionContext
	Tasks      []domain.Task
	Relations  []domain.TaskRelation
}

// GetWorkItemExecutionContextQuery identifies one WorkItem and requesting actor.
type GetWorkItemExecutionContextQuery struct {
	WorkItemID domain.WorkItemID
	Identity   Identity
}

// GetWorkItemExecutionContext returns the shared coordination state for one
// open or terminal WorkItem.
func (s *Service) GetWorkItemExecutionContext(
	ctx context.Context,
	query GetWorkItemExecutionContextQuery,
) (WorkItemExecutionContext, error) {
	if strings.TrimSpace(string(query.WorkItemID)) == "" {
		return WorkItemExecutionContext{}, invalidCommand("work item id is required")
	}
	if err := query.Identity.Validate(); err != nil {
		return WorkItemExecutionContext{}, err
	}

	var result WorkItemExecutionContext
	err := s.repository.View(ctx, func(store ReadStore) error {
		workItem, err := store.GetWorkItem(query.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", query.WorkItemID, err)
		}
		definition, err := loadDefinitionExecutionContext(store, workItem)
		if err != nil {
			return err
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list tasks for work item %q: %w", workItem.ID, err)
		}
		relations, err := store.ListTaskRelations(workItem.ID)
		if err != nil {
			return fmt.Errorf("list relations for work item %q: %w", workItem.ID, err)
		}
		if tasks == nil {
			tasks = []domain.Task{}
		}
		if relations == nil {
			relations = []domain.TaskRelation{}
		}
		result = WorkItemExecutionContext{
			WorkItem: workItem, Definition: definition, Tasks: tasks, Relations: relations,
		}
		return nil
	})
	if err != nil {
		return WorkItemExecutionContext{}, err
	}
	return result, nil
}

func loadDefinitionExecutionContext(store ReadStore, workItem domain.WorkItem) (DefinitionExecutionContext, error) {
	switch workItem.CoordinationMode() {
	case domain.CoordinationModeWorkflow:
		definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
		if err != nil {
			return DefinitionExecutionContext{}, fmt.Errorf("get workflow definition: %w", err)
		}
		return definitionExecutionContext(definition.DefinitionMetadata), nil
	case domain.CoordinationModeBlackboard:
		definition, err := store.GetBlackboardDefinition(workItem.Definition.ID, workItem.Definition.Version)
		if err != nil {
			return DefinitionExecutionContext{}, fmt.Errorf("get blackboard definition: %w", err)
		}
		return definitionExecutionContext(definition.DefinitionMetadata), nil
	default:
		return DefinitionExecutionContext{}, invalidCommand("work item %q has invalid coordination mode", workItem.ID)
	}
}
