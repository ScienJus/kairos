package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// DefinitionMetadataCommand contains fields shared by immutable Definition versions.
type DefinitionMetadataCommand struct {
	ID                domain.DefinitionID
	Version           int64
	Name              string
	Description       string
	AgentInstructions string
	SuggestedTags     []string
	Status            domain.DefinitionStatus
}

// CreateWorkflowDefinitionCommand creates one immutable Workflow Definition version.
type CreateWorkflowDefinitionCommand struct {
	Identity    Identity
	OperationID string
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
	var created domain.WorkflowDefinition
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_workflow_definition", command, &created, func(store WriteStore) error {
		now := s.clock.Now()
		definition := domain.WorkflowDefinition{
			DefinitionMetadata: definitionMetadata(command.Metadata, now),
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
	return created, nil
}

// CreateBlackboardDefinitionCommand creates one immutable Blackboard Definition version.
type CreateBlackboardDefinitionCommand struct {
	Identity    Identity
	OperationID string
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
	var created domain.BlackboardDefinition
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_blackboard_definition", command, &created, func(store WriteStore) error {
		now := s.clock.Now()
		definition := domain.BlackboardDefinition{DefinitionMetadata: definitionMetadata(command.Metadata, now)}
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
	return created, nil
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
	return result, err
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
	return result, err
}

// ListWorkflowDefinitions returns every stored Workflow Definition version.
func (s *Service) ListWorkflowDefinitions(ctx context.Context, actor Identity) ([]domain.WorkflowDefinition, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	var result []domain.WorkflowDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definitions, err := store.ListWorkflowDefinitions()
		if err != nil {
			return fmt.Errorf("list workflow definitions: %w", err)
		}
		result = definitions
		return nil
	})
	return result, err
}

// ListBlackboardDefinitions returns every stored Blackboard Definition version.
func (s *Service) ListBlackboardDefinitions(ctx context.Context, actor Identity) ([]domain.BlackboardDefinition, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	var result []domain.BlackboardDefinition
	err := s.repository.View(ctx, func(store ReadStore) error {
		definitions, err := store.ListBlackboardDefinitions()
		if err != nil {
			return fmt.Errorf("list blackboard definitions: %w", err)
		}
		result = definitions
		return nil
	})
	return result, err
}

func validateDefinitionMetadataCommand(command DefinitionMetadataCommand) error {
	if strings.TrimSpace(string(command.ID)) == "" {
		return invalidCommand("definition id is required")
	}
	if command.Version <= 0 {
		return invalidCommand("definition version must be greater than zero")
	}
	return nil
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

func definitionMetadata(command DefinitionMetadataCommand, now time.Time) domain.DefinitionMetadata {
	return domain.DefinitionMetadata{
		ID: command.ID, Version: command.Version, Name: strings.TrimSpace(command.Name),
		Description: strings.TrimSpace(command.Description), AgentInstructions: strings.TrimSpace(command.AgentInstructions),
		SuggestedTags: append([]string(nil), command.SuggestedTags...), Status: command.Status,
		CreatedAt: now, UpdatedAt: now,
	}
}
