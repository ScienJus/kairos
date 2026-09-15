package repository

import (
	"context"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowRecoveryMigrationBackfillsHistoricalFailures(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		definition := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "legacy-definition", Version: 1, Name: "Legacy", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}, Graph: domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"start"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "start", Title: "Start", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}}}}
		if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
			t.Fatal(err)
		}
		// Persist valid aggregates without the new optional fields, as deployed 005
		// did. Both system and user failures retain their original explanation.
		for _, id := range []domain.WorkItemID{"legacy", "ordinary", "no-event"} {
			work := domain.WorkItem{ID: id, Definition: domain.DefinitionBinding{ID: "legacy-definition", Version: 1, Mode: domain.CoordinationModeWorkflow}, Status: domain.WorkItemStatusFailed, Title: "Historical failure", Goal: "Keep history", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
			if err := work.Validate(); err != nil {
				t.Fatal(err)
			}
			if err := repo.Update(ctx, func(store application.WriteStore) error {
				if err := store.CreateWorkItem(work); err != nil {
					return err
				}
				if id == "no-event" {
					return nil
				}
				actor := domain.ActorRef{Kind: domain.ActorAgent, ID: "kairos"}
				if id == "ordinary" {
					actor = domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}
				}
				return store.AppendWorkItemEvent(domain.WorkItemEvent{ID: domain.WorkItemEventID("failure-" + id), WorkItemID: id, Sequence: 1, Type: domain.WorkItemEventWorkItemFailed, EntityID: string(id), Actor: &actor, Message: "workflow exceeded MaxTaskInstancesPerNode (10) after task source-task", OccurredAt: repositoryTestTime})
			}); err != nil {
				t.Fatal(err)
			}
		}
		for _, statement := range []string{"ALTER TABLE work_items DROP COLUMN workflow_max_task_instances_per_node", "DELETE FROM schema_migrations WHERE version = '006_workflow_recovery'"} {
			if _, err := repo.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.View(ctx, func(store application.ReadStore) error {
			for _, id := range []domain.WorkItemID{"legacy", "ordinary", "no-event"} {
				work, err := store.GetWorkItem(id)
				if err != nil {
					return err
				}
				if work.Status != domain.WorkItemStatusFailed || work.Version != 0 || work.WorkflowMaxTaskInstancesPerNode != 0 {
					t.Fatal("migration changed lifecycle or limit")
				}
				if id == "no-event" {
					if work.Failure != nil {
						t.Fatal("invented failure")
					}
					continue
				}
				want := domain.FailureExecution
				if work.Failure == nil || work.Failure.Kind != want || work.Failure.Message != "workflow exceeded MaxTaskInstancesPerNode (10) after task source-task" || work.Failure.Limit != 0 || work.Failure.TaskInstances != 0 {
					t.Fatalf("failure %s: %+v", id, work.Failure)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
