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
		Identity: actor, OperationID: "blackboard-v1",
		Metadata: DefinitionMetadataCommand{
			ID: "engineering", Version: 1, Name: "Engineering", Status: domain.DefinitionStatusPublished,
		},
	})
	if err != nil {
		t.Fatalf("create Blackboard definition: %v", err)
	}
	if blackboard.CreatedAt != applicationTestTime || blackboard.UpdatedAt != applicationTestTime {
		t.Fatalf("Blackboard timestamps = %v, %v", blackboard.CreatedAt, blackboard.UpdatedAt)
	}

	workflowCommand := CreateWorkflowDefinitionCommand{
		Identity: actor, OperationID: "workflow-v1",
		Metadata: DefinitionMetadataCommand{
			ID: "delivery", Version: 1, Name: "Delivery", Status: domain.DefinitionStatusPublished,
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

	retried, err := service.CreateWorkflowDefinition(context.Background(), workflowCommand)
	if err != nil {
		t.Fatalf("retry Workflow definition creation: %v", err)
	}
	if retried.ID != workflow.ID || retried.Version != workflow.Version {
		t.Fatalf("retried Workflow = %+v, want %+v", retried, workflow)
	}

	workflows, err := service.ListWorkflowDefinitions(context.Background(), actor)
	if err != nil {
		t.Fatalf("list Workflow definitions: %v", err)
	}
	blackboards, err := service.ListBlackboardDefinitions(context.Background(), actor)
	if err != nil {
		t.Fatalf("list Blackboard definitions: %v", err)
	}
	if len(workflows) != 1 || len(blackboards) != 1 {
		t.Fatalf("listed %d Workflows and %d Blackboards", len(workflows), len(blackboards))
	}
}

func TestPublishedWorkflowDefinitionRequiresValidGraph(t *testing.T) {
	service := newTestService(t, newTestRepository())
	actor := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "architect"}

	_, err := service.CreateWorkflowDefinition(context.Background(), CreateWorkflowDefinitionCommand{
		Identity: actor,
		Metadata: DefinitionMetadataCommand{
			ID: "invalid", Version: 1, Name: "Invalid", Status: domain.DefinitionStatusPublished,
		},
	})
	if !errors.Is(err, domain.ErrInvalidModel) {
		t.Fatalf("error = %v, want invalid domain model", err)
	}

	draft, err := service.CreateWorkflowDefinition(context.Background(), CreateWorkflowDefinitionCommand{
		Identity: actor,
		Metadata: DefinitionMetadataCommand{
			ID: "draft", Version: 1, Name: "Draft", Status: domain.DefinitionStatusDraft,
		},
	})
	if err != nil {
		t.Fatalf("create draft Workflow without graph: %v", err)
	}
	if draft.Status != domain.DefinitionStatusDraft {
		t.Fatalf("draft status = %q", draft.Status)
	}
}
