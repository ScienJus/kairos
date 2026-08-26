package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// ListWorkItemsQuery filters the durable WorkItem collection for operational views.
type ListWorkItemsQuery struct {
	Identity Identity
	Statuses []domain.WorkItemStatus
	Modes    []domain.CoordinationMode
	Tags     []string
	Page     PageRequest[WorkItemCursor]
}

// ListWorkItems returns durable WorkItems, including terminal items, newest first.
func (s *Service) ListWorkItems(ctx context.Context, query ListWorkItemsQuery) (Page[domain.WorkItem], error) {
	if err := query.Identity.Validate(); err != nil {
		return Page[domain.WorkItem]{}, err
	}
	if err := validatePageRequest(query.Page.Limit); err != nil {
		return Page[domain.WorkItem]{}, err
	}
	for _, status := range query.Statuses {
		if !status.Valid() {
			return Page[domain.WorkItem]{}, invalidCommand("unsupported work item status %q", status)
		}
	}
	for _, mode := range query.Modes {
		if !mode.Valid() {
			return Page[domain.WorkItem]{}, invalidCommand("unsupported coordination mode %q", mode)
		}
	}

	var result []domain.WorkItem
	err := s.repository.View(ctx, func(store ReadStore) error {
		workItems, err := store.ListWorkItems(WorkItemFilter{
			Statuses: query.Statuses,
			Modes:    query.Modes,
			Tags:     query.Tags,
			Page:     query.Page,
		})
		if err != nil {
			return fmt.Errorf("list work items: %w", err)
		}
		for _, workItem := range workItems {
			result = append(result, normalizeWorkItemCollections(workItem))
		}
		return nil
	})
	if err != nil {
		return Page[domain.WorkItem]{}, err
	}
	return boundedPage(result, query.Page.Limit), nil
}

func validatePageRequest(limit int) error {
	if limit < 1 || limit > MaxPageLimit {
		return invalidCommand("limit must be between 1 and %d", MaxPageLimit)
	}
	return nil
}

func boundedPage[T any](items []T, limit int) Page[T] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, HasMore: hasMore}
}

// WorkItemExecutionContext contains a durable WorkItem view that remains
// addressable after it leaves the open candidate set.
type WorkItemExecutionContext struct {
	WorkItem     domain.WorkItem            `json:"work_item"`
	Definition   DefinitionExecutionContext `json:"definition"`
	Tasks        []domain.Task              `json:"tasks"`
	Relations    []domain.TaskRelation      `json:"relations"`
	Claims       []domain.Claim             `json:"claims"`
	ActiveClaims []domain.Claim             `json:"active_claims"`
	Artifacts    []domain.Artifact          `json:"artifacts"`
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
		claims, err := store.ListClaimsByWorkItem(workItem.ID)
		if err != nil {
			return fmt.Errorf("list claims for work item %q: %w", workItem.ID, err)
		}
		artifacts, err := store.ListArtifacts(ArtifactFilter{WorkItemID: workItem.ID})
		if err != nil {
			return fmt.Errorf("list artifacts for work item %q: %w", workItem.ID, err)
		}
		committedArtifacts := make([]domain.Artifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			if artifact.SubmissionID != nil {
				committedArtifacts = append(committedArtifacts, artifact)
			}
		}
		if claims == nil {
			claims = []domain.Claim{}
		}
		activeClaims := []domain.Claim{}
		activeClaimIDs := make(map[domain.ClaimID]struct{})
		for _, task := range tasks {
			if task.ActiveClaimID != nil {
				activeClaimIDs[*task.ActiveClaimID] = struct{}{}
			}
		}
		for _, claim := range claims {
			if _, active := activeClaimIDs[claim.ID]; active && claim.Active() {
				activeClaims = append(activeClaims, claim)
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
