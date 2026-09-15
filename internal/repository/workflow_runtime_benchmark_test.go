package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

// Measures a real SQLite submission which fans out to eight nodes. Setup is
// excluded; each measured transaction rolls back so every sample sees the same
// history and active Claim. Completed history has one 1 KiB result per Task.
func BenchmarkWorkflowFanoutHistory(b *testing.B) {
	for _, history := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("tasks_%d", history), func(b *testing.B) {
			ctx := context.Background()
			repo, err := OpenSQLite(ctx, filepath.Join(b.TempDir(), "workflow.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = repo.Close() })
			command := prepareWorkflowFanout(b, repo, history)
			counted := &workflowRecoveryCountingRepository{Repository: rollbackWorkflowBenchmarkRepository{repo}, reads: make(map[string]int)}
			measured, err := application.NewService(counted, repositoryTestClock{}, &repositoryTestIDs{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := measured.SubmitTask(ctx, command); !errors.Is(err, errWorkflowBenchmarkRollback) {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(counted.reads["tasks"])/float64(b.N), "task-lists/op")
			b.ReportMetric(float64(counted.reads["activations"])/float64(b.N), "activation-lists/op")
		})
	}
}

var errWorkflowBenchmarkRollback = errors.New("rollback completed benchmark submission")

type rollbackWorkflowBenchmarkRepository struct{ application.Repository }

func (r rollbackWorkflowBenchmarkRepository) Update(ctx context.Context, fn func(application.WriteStore) error) error {
	return r.Repository.Update(ctx, func(store application.WriteStore) error {
		if err := fn(store); err != nil {
			return err
		}
		return errWorkflowBenchmarkRollback
	})
}

func prepareWorkflowFanout(t testing.TB, repo *SQLRepository, history int) application.SubmitTaskCommand {
	t.Helper()
	ctx := context.Background()
	actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "benchmark"}}
	def := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "scale", Version: 1, Name: "Scale", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}, Graph: domain.WorkflowGraph{MaxTaskExecutions: 500}}
	for i := 0; i < 100; i++ {
		id := domain.WorkflowTaskID(fmt.Sprintf("node-%d", i))
		def.Graph.Tasks = append(def.Graph.Tasks, domain.WorkflowTaskDefinition{ID: id, Title: string(id), Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
		if i == 0 || i > 8 {
			def.Graph.StartTaskIDs = append(def.Graph.StartTaskIDs, id)
		}
		if i >= 1 && i <= 8 {
			def.Graph.Relations = append(def.Graph.Relations, domain.WorkflowRelationDefinition{ID: domain.WorkflowRelationID(fmt.Sprintf("edge-%d", i)), FromTaskID: "node-0", ToTaskID: id})
		}
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	service, err := application.NewService(repo, repositoryTestClock{}, &repositoryTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Fanout", Goal: "Measure expansion"})
	if err != nil {
		t.Fatal(err)
	}
	var source domain.Task
	err = repo.Update(ctx, func(store application.WriteStore) error {
		tasks, err := store.ListTasks(work.ID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if *task.WorkflowTaskID == "node-0" {
				source = task
			}
		}
		// Historical aggregate snapshots across 91 independent nodes stay
		// below 500 executions per node, including their initial instances.
		for i := len(tasks); i < history; i++ {
			task := source
			task.ID = domain.TaskID(fmt.Sprintf("history-%d", i))
			nodeID := domain.WorkflowTaskID(fmt.Sprintf("node-%d", 9+i%91))
			activationID := domain.WorkflowTaskActivationID(fmt.Sprintf("activation-%d", i))
			task.WorkflowTaskID, task.WorkflowActivationID = &nodeID, &activationID
			task.Position, task.Status, task.CompletedAt = int64(i), domain.TaskStatusCompleted, &repositoryTestTime
			task.Submissions = []domain.TaskSubmission{{ID: domain.SubmissionID(fmt.Sprintf("submission-%d", i)), TaskID: task.ID, ClaimID: domain.ClaimID(fmt.Sprintf("claim-%d", i)), Result: strings.Repeat("x", 1024), SubmittedAt: repositoryTestTime}}
			activation := domain.WorkflowTaskActivation{ID: activationID, WorkItemID: work.ID, WorkflowTaskID: nodeID, CorrelationID: domain.WorkflowCorrelationID(fmt.Sprintf("correlation-%d", i)), Status: domain.WorkflowActivationResolved, Outcome: domain.WorkflowActivationCreated, CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime, ResolvedAt: &repositoryTestTime}
			if err := task.Validate(domain.CoordinationModeWorkflow); err != nil {
				return err
			}
			if err := activation.Validate(); err != nil {
				return err
			}
			if err := store.CreateWorkflowTaskActivation(activation); err != nil {
				return err
			}
			if err := store.CreateTask(task); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: source.ID, Identity: actor})
	if err != nil {
		t.Fatal(err)
	}
	return application.SubmitTaskCommand{TaskID: source.ID, ClaimID: claim.ID, Identity: actor, Result: "Fan out", Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "exit:node-0"}}
}
