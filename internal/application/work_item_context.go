package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// ListWorkItemsQuery filters the durable WorkItem collection for operational views.
type ListWorkItemsQuery struct {
	Identity Identity
	Statuses []domain.WorkItemStatus
	Modes    []domain.CoordinationMode
	Tags     []string
}

// ListWorkItems returns durable WorkItems, including terminal items, newest first.
func (s *Service) ListWorkItems(ctx context.Context, query ListWorkItemsQuery) ([]domain.WorkItem, error) {
	if err := query.Identity.Validate(); err != nil {
		return nil, err
	}
	for _, status := range query.Statuses {
		if !status.Valid() {
			return nil, invalidCommand("unsupported work item status %q", status)
		}
	}
	for _, mode := range query.Modes {
		if !mode.Valid() {
			return nil, invalidCommand("unsupported coordination mode %q", mode)
		}
	}

	var result []domain.WorkItem
	err := s.repository.View(ctx, func(store ReadStore) error {
		workItems, err := store.ListWorkItems()
		if err != nil {
			return fmt.Errorf("list work items: %w", err)
		}
		for _, workItem := range workItems {
			if len(query.Statuses) > 0 && !slices.Contains(query.Statuses, workItem.Status) {
				continue
			}
			if len(query.Modes) > 0 && !slices.Contains(query.Modes, workItem.CoordinationMode()) {
				continue
			}
			if !containsAllStrings(workItem.Tags, query.Tags) {
				continue
			}
			result = append(result, normalizeWorkItemCollections(workItem))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(result, func(a, b domain.WorkItem) int {
		if compared := b.UpdatedAt.Compare(a.UpdatedAt); compared != 0 {
			return compared
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	if result == nil {
		result = []domain.WorkItem{}
	}
	return result, nil
}

func containsAllStrings(values, required []string) bool {
	for _, value := range required {
		if !slices.Contains(values, value) {
			return false
		}
	}
	return true
}

// WorkItemExecutionContext contains a durable WorkItem view that remains
// addressable after it leaves the open candidate set.
type WorkItemExecutionContext struct {
	WorkItem     domain.WorkItem
	Definition   DefinitionExecutionContext
	Tasks        []domain.Task
	Relations    []domain.TaskRelation
	Claims       []domain.Claim
	ActiveClaims []domain.Claim
	Artifacts    []domain.Artifact
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
		artifacts, err := store.ListArtifacts(workItem.ID)
		if err != nil {
			return fmt.Errorf("list artifacts for work item %q: %w", workItem.ID, err)
		}
		committedArtifacts := make([]domain.Artifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			if artifact.SubmissionID != nil {
				committedArtifacts = append(committedArtifacts, artifact)
			}
		}
		claims := []domain.Claim{}
		activeClaims := []domain.Claim{}
		for _, task := range tasks {
			taskClaims, err := store.ListClaims(task.ID)
			if err != nil {
				return fmt.Errorf("list claims for task %q: %w", task.ID, err)
			}
			claims = append(claims, taskClaims...)
			if task.ActiveClaimID == nil {
				continue
			}
			for _, claim := range taskClaims {
				if claim.ID == *task.ActiveClaimID && claim.Active() {
					activeClaims = append(activeClaims, claim)
					break
				}
			}
		}
		result = WorkItemExecutionContext{
			WorkItem: normalizeWorkItemCollections(workItem), Definition: normalizeDefinitionContext(definition),
			Tasks: normalizeTasks(tasks), Relations: relations, Claims: claims, ActiveClaims: activeClaims, Artifacts: committedArtifacts,
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
