package repository

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowFanoutDoesNotReloadHistoryPerEdge(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		command := prepareWorkflowFanout(t, repo, 100)
		counted := &workflowRecoveryCountingRepository{Repository: openPeer(t), reads: make(map[string]int)}
		service := repositoryTestService(t, counted)
		if _, err := service.SubmitTask(ctx, command); err != nil {
			t.Fatal(err)
		}
		if counted.reads["tasks"] != 1 || counted.reads["activations"] != 1 {
			t.Fatalf("fanout reloaded history per edge: %v", counted.reads)
		}
		if err := repo.View(ctx, func(store application.ReadStore) error {
			source, err := store.GetTask(command.TaskID)
			if err != nil {
				return err
			}
			if source.Status != domain.TaskStatusCompleted {
				t.Fatal("source submission was not committed")
			}
			tasks, err := store.ListTasks(source.WorkItemID)
			if err != nil {
				return err
			}
			positions := make(map[int64]bool)
			for _, task := range tasks {
				if task.Position >= 100 {
					if task.Status != domain.TaskStatusPending {
						t.Fatal("target is not Pending")
					}
					positions[task.Position] = true
				}
			}
			if len(tasks) != 108 || len(positions) != 8 {
				t.Fatalf("fanout lost/duplicated Task positions: %v", positions)
			}
			for position := int64(100); position < 108; position++ {
				if !positions[position] {
					t.Fatalf("missing position %d", position)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWorkflowRuntimeQueriesScopeAndWaitingConflict(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		command := prepareWorkflowFanout(t, repo, 100)
		var workID domain.WorkItemID
		var waiting domain.WorkflowTaskActivation
		if err := repo.Update(ctx, func(store application.WriteStore) error {
			source, err := store.GetTask(command.TaskID)
			if err != nil {
				return err
			}
			source.Position = 1000
			source.Version++
			if err := store.SaveTask(source); err != nil {
				return err
			}
			workID = source.WorkItemID
			waiting = domain.WorkflowTaskActivation{ID: "waiting", WorkItemID: workID, WorkflowTaskID: "node-0", CorrelationID: "waiting-correlation", Status: domain.WorkflowActivationWaiting, Inputs: []domain.WorkflowActivationInput{{RelationID: "edge-1"}}, CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
			if err := waiting.Validate(); err != nil {
				return err
			}
			return store.CreateWorkflowTaskActivation(waiting)
		}); err != nil {
			t.Fatal(err)
		}
		peer := openPeer(t)
		if err := peer.View(ctx, func(store application.ReadStore) error {
			for _, scenario := range []struct {
				work        domain.WorkItemID
				node        domain.WorkflowTaskID
				correlation domain.WorkflowCorrelationID
				found       bool
			}{
				{workID, "node-0", "waiting-correlation", true},
				{workID, "node-0", "other-correlation", false},
				{workID, "node-1", "waiting-correlation", false},
				{"other-work", "node-0", "waiting-correlation", false},
			} {
				got, found, err := store.FindWaitingWorkflowTaskActivation(scenario.work, scenario.node, scenario.correlation)
				if err != nil {
					return err
				}
				if found != scenario.found || (found && !reflect.DeepEqual(got, waiting)) {
					t.Fatalf("waiting lookup: %+v %v", got, found)
				}
			}
			for _, scenario := range []struct {
				work domain.WorkItemID
				node domain.WorkflowTaskID
				want int
			}{
				{workID, "node-0", 1}, // Waiting instance does not consume the budget.
				{workID, "node-1", 0}, {"other-work", "node-0", 0},
			} {
				count, err := store.CountResolvedWorkflowTaskActivations(scenario.work, scenario.node)
				if err != nil {
					return err
				}
				if count != scenario.want {
					t.Fatalf("task instance count %d, want %d", count, scenario.want)
				}
			}
			for _, scenario := range []struct {
				work domain.WorkItemID
				want int64
			}{{workID, 1001}, {"other-work", 0}} {
				position, err := store.NextTaskPosition(scenario.work)
				if err != nil {
					return err
				}
				if position != scenario.want {
					t.Fatalf("next position %d, want %d", position, scenario.want)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := repo.Update(ctx, func(store application.WriteStore) error {
			duplicate := waiting
			duplicate.ID = "duplicate-waiting"
			return store.CreateWorkflowTaskActivation(duplicate)
		}); err != nil {
			t.Fatal(err)
		}
		if err := peer.View(ctx, func(store application.ReadStore) error {
			_, _, err := store.FindWaitingWorkflowTaskActivation(workID, waiting.WorkflowTaskID, waiting.CorrelationID)
			if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "multiple waiting activations") {
				t.Fatalf("expected duplicate waiting conflict: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
