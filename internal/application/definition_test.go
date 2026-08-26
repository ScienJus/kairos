package application

import (
	"context"
	"errors"
	"testing"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestCreateDefinitionsKeepsWorkflowGraphSeparateFromBlackboard(t *testing.T) {
	repository := newTestRepository()
	service := newTestService(t, repository)
	actor := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "architect"}

	blackboard, err := service.CreateBlackboardDefinition(context.Background(), CreateBlackboardDefinitionCommand{
		Identity: actor,
		Metadata: DefinitionMetadataCommand{
			ID: "engineering", Name: "Engineering",
		},
	})
	if err != nil {
		t.Fatalf("create Blackboard definition: %v", err)
	}
	if blackboard.CreatedAt != applicationTestTime || blackboard.UpdatedAt != applicationTestTime {
		t.Fatalf("Blackboard timestamps = %v, %v", blackboard.CreatedAt, blackboard.UpdatedAt)
	}
	blackboardBase := blackboard.Version
	blackboardV2, err := service.CreateBlackboardDefinition(context.Background(), CreateBlackboardDefinitionCommand{
		Identity: actor, BaseVersion: &blackboardBase,
		Metadata: DefinitionMetadataCommand{ID: blackboard.ID, Name: "Engineering v2"},
	})
	if err != nil || blackboardV2.Version != 2 {
		t.Fatalf("create next Blackboard version: %#v, err=%v", blackboardV2, err)
	}
	if _, err := service.CreateBlackboardDefinition(context.Background(), CreateBlackboardDefinitionCommand{
		Identity: actor, BaseVersion: &blackboardBase,
		Metadata: DefinitionMetadataCommand{ID: blackboard.ID, Name: "Stale Engineering edit"},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Blackboard base error = %v, want conflict", err)
	}

	workflowCommand := CreateWorkflowDefinitionCommand{
		Identity: actor,
		Metadata: DefinitionMetadataCommand{
			ID: "delivery", Name: "Delivery",
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{{
				ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent,
				AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone,
			}},
		},
	}
	workflow, err := service.CreateWorkflowDefinition(context.Background(), workflowCommand)
	if err != nil {
		t.Fatalf("create Workflow definition: %v", err)
	}
	if len(workflow.Graph.Tasks) != 1 || workflow.Graph.Tasks[0].ID != "implement" {
		t.Fatalf("Workflow graph = %+v", workflow.Graph)
	}

	if _, err := service.CreateWorkflowDefinition(context.Background(), workflowCommand); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry Workflow definition creation = %v, want conflict", err)
	}
	workflowV2Command := workflowCommand
	workflowV2Command.BaseVersion = &workflow.Version
	workflowV2Command.Metadata.Name = "Delivery v2"
	workflowV2, err := service.CreateWorkflowDefinition(context.Background(), workflowV2Command)
	if err != nil {
		t.Fatalf("create next Workflow version: %v", err)
	}
	if workflowV2.Version != 2 {
		t.Fatalf("next Workflow version = %d, want 2", workflowV2.Version)
	}
	missingBaseCommand := workflowCommand
	if _, err := service.CreateWorkflowDefinition(context.Background(), missingBaseCommand); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing Workflow base error = %v, want conflict", err)
	}
	staleBaseCommand := workflowCommand
	staleBaseCommand.BaseVersion = &workflow.Version
	if _, err := service.CreateWorkflowDefinition(context.Background(), staleBaseCommand); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Workflow base error = %v, want conflict", err)
	}

	workflows, err := service.ListWorkflowDefinitionCatalog(context.Background(), actor, DefinitionCatalogFilter{Page: PageRequest[DefinitionCatalogCursor]{Limit: 50}})
	if err != nil {
		t.Fatalf("list Workflow definitions: %v", err)
	}
	blackboards, err := service.ListBlackboardDefinitionCatalog(context.Background(), actor, DefinitionCatalogFilter{Page: PageRequest[DefinitionCatalogCursor]{Limit: 50}})
	if err != nil {
		t.Fatalf("list Blackboard definitions: %v", err)
	}
	if len(workflows.Items) != 1 || len(blackboards.Items) != 1 {
		t.Fatalf("listed %d Workflows and %d Blackboards", len(workflows.Items), len(blackboards.Items))
	}
}

func TestWorkflowDefinitionRequiresValidGraph(t *testing.T) {
	service := newTestService(t, newTestRepository())
	actor := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "architect"}

	_, err := service.CreateWorkflowDefinition(context.Background(), CreateWorkflowDefinitionCommand{
		Identity: actor,
		Metadata: DefinitionMetadataCommand{
			ID: "invalid", Name: "Invalid",
		},
	})
	if !errors.Is(err, domain.ErrInvalidModel) {
		t.Fatalf("error = %v, want invalid domain model", err)
	}
}
