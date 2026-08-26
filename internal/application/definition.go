package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// DefinitionMetadataCommand contains fields shared by immutable Definition versions.
type DefinitionMetadataCommand struct {
	ID                domain.DefinitionID
	Name              string
	Description       string
	AgentInstructions string
	SuggestedTags     []string
}

// CreateWorkflowDefinitionCommand creates one immutable Workflow Definition version.
type CreateWorkflowDefinitionCommand struct {
	Identity Identity
	// BaseVersion is nil only when creating version 1. Appends must name the
	// latest stored version so concurrent edits cannot overwrite each other.
	BaseVersion *int64
	Metadata    DefinitionMetadataCommand
	Graph       domain.WorkflowGraph
}

// CreateWorkflowDefinition validates and stores one Workflow Definition version.
func (s *Service) CreateWorkflowDefinition(
	ctx context.Context,
	command CreateWorkflowDefinitionCommand,
) (domain.WorkflowDefinition, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	if err := validateDefinitionMetadataCommand(command.Metadata); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	if err := validateDefinitionBaseVersion(command.BaseVersion); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	var created domain.WorkflowDefinition
	err := s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockDefinitionVersion(domain.CoordinationModeWorkflow, command.Metadata.ID); err != nil {
			return fmt.Errorf("lock workflow definition version: %w", err)
		}
		version, err := nextWorkflowDefinitionVersion(store, command.Metadata.ID, command.BaseVersion)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		definition := domain.WorkflowDefinition{
			DefinitionMetadata: definitionMetadata(command.Metadata, version, now),
			Graph:              command.Graph,
		}
		if err := definition.Validate(); err != nil {
			return err
		}
		if err := store.CreateWorkflowDefinition(definition); err != nil {
			return fmt.Errorf("create workflow definition: %w", err)
		}
		created = definition
		return nil
	})
	if err != nil {
		return domain.WorkflowDefinition{}, err
	}
	return normalizeWorkflowDefinition(created), nil
}

// CreateBlackboardDefinitionCommand creates one immutable Blackboard Definition version.
type CreateBlackboardDefinitionCommand struct {
	Identity Identity
	// BaseVersion is nil only when creating version 1. Appends must name the
	// latest stored version so concurrent edits cannot overwrite each other.
	BaseVersion *int64
	Metadata    DefinitionMetadataCommand
}

// CreateBlackboardDefinition validates and stores one Blackboard Definition version.
func (s *Service) CreateBlackboardDefinition(
	ctx context.Context,
	command CreateBlackboardDefinitionCommand,
) (domain.BlackboardDefinition, error) {
	if err := command.Identity.Validate(); err != nil {
		return domain.BlackboardDefinition{}, err
	}
	if err := validateDefinitionMetadataCommand(command.Metadata); err != nil {
		return domain.BlackboardDefinition{}, err
	}
	if err := validateDefinitionBaseVersion(command.BaseVersion); err != nil {
		return domain.BlackboardDefinition{}, err
	}
	var created domain.BlackboardDefinition
	err := s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockDefinitionVersion(domain.CoordinationModeBlackboard, command.Metadata.ID); err != nil {
			return fmt.Errorf("lock blackboard definition version: %w", err)
		}
		version, err := nextBlackboardDefinitionVersion(store, command.Metadata.ID, command.BaseVersion)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		definition := domain.BlackboardDefinition{DefinitionMetadata: definitionMetadata(command.Metadata, version, now)}
		if err := definition.Validate(); err != nil {
			return err
		}
		if err := store.CreateBlackboardDefinition(definition); err != nil {
			return fmt.Errorf("create blackboard definition: %w", err)
		}
		created = definition
		return nil
	})
	if err != nil {
		return domain.BlackboardDefinition{}, err
	}
	return normalizeBlackboardDefinition(created), nil
}

// GetDefinitionQuery selects one immutable Definition version.
type GetDefinitionQuery struct {
	Identity Identity
	ID       domain.DefinitionID
	Version  int64
}

// GetWorkflowDefinition returns one Workflow Definition version.
func (s *Service) GetWorkflowDefinition(ctx context.Context, query GetDefinitionQuery) (domain.WorkflowDefinition, error) {
	if err := validateGetDefinitionQuery(query); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	var result domain.WorkflowDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definition, err := store.GetWorkflowDefinition(query.ID, query.Version)
		if err != nil {
			return fmt.Errorf("get workflow definition: %w", err)
		}
		result = definition
		return nil
	})
	return normalizeWorkflowDefinition(result), err
}

// GetBlackboardDefinition returns one Blackboard Definition version.
func (s *Service) GetBlackboardDefinition(ctx context.Context, query GetDefinitionQuery) (domain.BlackboardDefinition, error) {
	if err := validateGetDefinitionQuery(query); err != nil {
		return domain.BlackboardDefinition{}, err
	}
	var result domain.BlackboardDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definition, err := store.GetBlackboardDefinition(query.ID, query.Version)
		if err != nil {
			return fmt.Errorf("get blackboard definition: %w", err)
		}
		result = definition
		return nil
	})
	return normalizeBlackboardDefinition(result), err
}

// GetLatestWorkflowDefinition returns the latest stored version for one Workflow Definition ID.
func (s *Service) GetLatestWorkflowDefinition(ctx context.Context, actor Identity, id domain.DefinitionID) (domain.WorkflowDefinition, error) {
	if err := validateDefinitionID(actor, id); err != nil {
		return domain.WorkflowDefinition{}, err
	}
	var result domain.WorkflowDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definition, err := store.GetLatestWorkflowDefinition(id)
		if err != nil {
			return fmt.Errorf("get latest workflow definition: %w", err)
		}
		result = definition
		return nil
	})
	return normalizeWorkflowDefinition(result), err
}

// GetLatestBlackboardDefinition returns the latest stored version for one Blackboard Definition ID.
func (s *Service) GetLatestBlackboardDefinition(ctx context.Context, actor Identity, id domain.DefinitionID) (domain.BlackboardDefinition, error) {
	if err := validateDefinitionID(actor, id); err != nil {
		return domain.BlackboardDefinition{}, err
	}
	var result domain.BlackboardDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definition, err := store.GetLatestBlackboardDefinition(id)
		if err != nil {
			return fmt.Errorf("get latest blackboard definition: %w", err)
		}
		result = definition
		return nil
	})
	return normalizeBlackboardDefinition(result), err
}

// ListWorkflowDefinitionCatalog returns the latest version per Workflow Definition ID.
func (s *Service) ListWorkflowDefinitionCatalog(ctx context.Context, actor Identity, filter DefinitionCatalogFilter) (Page[domain.WorkflowDefinition], error) {
	if err := actor.Validate(); err != nil {
		return Page[domain.WorkflowDefinition]{}, err
	}
	if err := validatePageRequest(filter.Page.Limit); err != nil {
		return Page[domain.WorkflowDefinition]{}, err
	}
	var result []domain.WorkflowDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definitions, err := store.ListWorkflowDefinitionCatalog(filter)
		if err != nil {
			return fmt.Errorf("list workflow definition catalog: %w", err)
		}
		result = definitions
		return nil
	})
	if err != nil {
		return Page[domain.WorkflowDefinition]{}, err
	}
	return boundedPage(normalizeWorkflowDefinitions(result), filter.Page.Limit), nil
}

// ListBlackboardDefinitionCatalog returns the latest version per Blackboard Definition ID.
func (s *Service) ListBlackboardDefinitionCatalog(ctx context.Context, actor Identity, filter DefinitionCatalogFilter) (Page[domain.BlackboardDefinition], error) {
	if err := actor.Validate(); err != nil {
		return Page[domain.BlackboardDefinition]{}, err
	}
	if err := validatePageRequest(filter.Page.Limit); err != nil {
		return Page[domain.BlackboardDefinition]{}, err
	}
	var result []domain.BlackboardDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definitions, err := store.ListBlackboardDefinitionCatalog(filter)
		if err != nil {
			return fmt.Errorf("list blackboard definition catalog: %w", err)
		}
		result = definitions
		return nil
	})
	if err != nil {
		return Page[domain.BlackboardDefinition]{}, err
	}
	return boundedPage(normalizeBlackboardDefinitions(result), filter.Page.Limit), nil
}

// ListWorkflowDefinitionVersions returns one Definition ID's versions newest first.
func (s *Service) ListWorkflowDefinitionVersions(ctx context.Context, actor Identity, filter DefinitionVersionFilter) (Page[domain.WorkflowDefinition], error) {
	if err := validateDefinitionVersionFilter(actor, filter.ID, filter.Page.Limit); err != nil {
		return Page[domain.WorkflowDefinition]{}, err
	}
	var result []domain.WorkflowDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		if _, err := store.GetLatestWorkflowDefinition(filter.ID); err != nil {
			return fmt.Errorf("get workflow definition: %w", err)
		}
		definitions, err := store.ListWorkflowDefinitionVersions(filter)
		if err != nil {
			return fmt.Errorf("list workflow definition versions: %w", err)
		}
		result = definitions
		return nil
	})
	if err != nil {
		return Page[domain.WorkflowDefinition]{}, err
	}
	return boundedPage(normalizeWorkflowDefinitions(result), filter.Page.Limit), nil
}

// ListBlackboardDefinitionVersions returns one Definition ID's versions newest first.
func (s *Service) ListBlackboardDefinitionVersions(ctx context.Context, actor Identity, filter DefinitionVersionFilter) (Page[domain.BlackboardDefinition], error) {
	if err := validateDefinitionVersionFilter(actor, filter.ID, filter.Page.Limit); err != nil {
		return Page[domain.BlackboardDefinition]{}, err
	}
	var result []domain.BlackboardDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		if _, err := store.GetLatestBlackboardDefinition(filter.ID); err != nil {
			return fmt.Errorf("get blackboard definition: %w", err)
		}
		definitions, err := store.ListBlackboardDefinitionVersions(filter)
		if err != nil {
			return fmt.Errorf("list blackboard definition versions: %w", err)
		}
		result = definitions
		return nil
	})
	if err != nil {
		return Page[domain.BlackboardDefinition]{}, err
	}
	return boundedPage(normalizeBlackboardDefinitions(result), filter.Page.Limit), nil
}

func validateDefinitionMetadataCommand(command DefinitionMetadataCommand) error {
	if strings.TrimSpace(string(command.ID)) == "" {
		return invalidCommand("definition id is required")
	}
	return nil
}

func validateDefinitionBaseVersion(version *int64) error {
	if version != nil && *version <= 0 {
		return invalidCommand("definition base version must be greater than zero")
	}
	return nil
}

// Workflow and Blackboard Definitions use the same append-only update model:
// each accepted edit creates latest+1, and the base check runs while the
// Definition lock is held. A concurrent editor based on an older version gets
// a conflict instead of silently replacing changes accepted before it.
func nextWorkflowDefinitionVersion(store ReadStore, id domain.DefinitionID, baseVersion *int64) (int64, error) {
	latest, err := store.GetLatestWorkflowDefinition(id)
	if errors.Is(err, ErrNotFound) {
		if baseVersion != nil {
			return 0, conflict("workflow definition %q does not have base version %d", id, *baseVersion)
		}
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get latest workflow definition: %w", err)
	}
	if baseVersion == nil {
		return 0, conflict("base_version is required to append workflow definition %q", id)
	}
	if *baseVersion != latest.Version {
		return 0, conflict("workflow definition %q advanced from version %d to %d", id, *baseVersion, latest.Version)
	}
	return incrementDefinitionVersion(latest.Version)
}

func nextBlackboardDefinitionVersion(store ReadStore, id domain.DefinitionID, baseVersion *int64) (int64, error) {
	latest, err := store.GetLatestBlackboardDefinition(id)
	if errors.Is(err, ErrNotFound) {
		if baseVersion != nil {
			return 0, conflict("blackboard definition %q does not have base version %d", id, *baseVersion)
		}
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get latest blackboard definition: %w", err)
	}
	if baseVersion == nil {
		return 0, conflict("base_version is required to append blackboard definition %q", id)
	}
	if *baseVersion != latest.Version {
		return 0, conflict("blackboard definition %q advanced from version %d to %d", id, *baseVersion, latest.Version)
	}
	return incrementDefinitionVersion(latest.Version)
}

func incrementDefinitionVersion(version int64) (int64, error) {
	if version == int64(^uint64(0)>>1) {
		return 0, invalidCommand("definition version limit reached")
	}
	return version + 1, nil
}

func validateGetDefinitionQuery(query GetDefinitionQuery) error {
	if err := query.Identity.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(query.ID)) == "" || query.Version <= 0 {
		return invalidCommand("definition id and a positive version are required")
	}
	return nil
}

func validateDefinitionID(actor Identity, id domain.DefinitionID) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(id)) == "" {
		return invalidCommand("definition id is required")
	}
	return nil
}

func validateDefinitionVersionFilter(actor Identity, id domain.DefinitionID, limit int) error {
	if err := validateDefinitionID(actor, id); err != nil {
		return err
	}
	return validatePageRequest(limit)
}

func definitionMetadata(command DefinitionMetadataCommand, version int64, now time.Time) domain.DefinitionMetadata {
	return domain.DefinitionMetadata{
		ID: command.ID, Version: version, Name: strings.TrimSpace(command.Name),
		Description: strings.TrimSpace(command.Description), AgentInstructions: strings.TrimSpace(command.AgentInstructions),
		SuggestedTags: append([]string(nil), command.SuggestedTags...),
		CreatedAt:     now, UpdatedAt: now,
	}
}
