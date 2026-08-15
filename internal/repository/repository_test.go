package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

var repositoryTestTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
var repositoryTestIDSequence atomic.Int64

func TestSQLRepositoryContract(t *testing.T) {
	forEachSQLRepository(t, func(
		t *testing.T,
		repository *SQLRepository,
		openPeer func(*testing.T) *SQLRepository,
	) {
		ctx := context.Background()
		workflow := repositoryWorkflowDefinition()
		blackboard := repositoryBlackboardDefinition()
		if err := repository.CreateWorkflowDefinition(ctx, workflow); err != nil {
			t.Fatalf("create workflow definition: %v", err)
		}
		if err := repository.CreateBlackboardDefinition(ctx, blackboard); err != nil {
			t.Fatalf("create blackboard definition: %v", err)
		}

		t.Run("workflow round trip", func(t *testing.T) {
			testWorkflowRoundTrip(t, repository, workflow)
		})
		t.Run("rollback", func(t *testing.T) {
			testTransactionRollback(t, repository, blackboard)
		})
		t.Run("concurrent claim", func(t *testing.T) {
			testConcurrentClaim(t, repository, openPeer(t), blackboard)
		})
	})
}

func forEachSQLRepository(
	t *testing.T,
	test func(*testing.T, *SQLRepository, func(*testing.T) *SQLRepository),
) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kairos.db")
		open := func(t *testing.T) *SQLRepository {
			t.Helper()
			repository, err := OpenSQLite(context.Background(), path)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		test(t, open(t), open)
	})

	dsn := os.Getenv("KAIROS_TEST_POSTGRES_DSN")
	if dsn == "" {
		return
	}
	t.Run("postgres", func(t *testing.T) {
		open := func(t *testing.T) *SQLRepository {
			t.Helper()
			repository, err := OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatalf("open postgres: %v", err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		repository := open(t)
		if _, err := repository.db.ExecContext(context.Background(), `
			TRUNCATE TABLE
				work_item_events,
				claims,
				task_relations,
				tasks,
				workflow_activations,
				work_items,
				definitions
			CASCADE`); err != nil {
			t.Fatalf("reset postgres: %v", err)
		}
		test(t, repository, open)
	})
}

func testWorkflowRoundTrip(
	t *testing.T,
	repository *SQLRepository,
	definition domain.WorkflowDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "workflow-agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Persist workflow", Goal: "Verify SQL persistence",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	start := repositoryTaskByDefinition(t, repository, workItem.ID, "implement")
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim implement: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &application.WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit implement: %v", err)
	}

	docs := repositoryTaskByDefinition(t, repository, workItem.ID, "docs")
	if docs.Status != domain.TaskStatusSkipped || len(docs.TransitionDecisions) != 1 {
		t.Fatalf("persisted skipped docs: %#v", docs)
	}
	testTask := repositoryTaskByDefinition(t, repository, workItem.ID, "test")
	if testTask.Status != domain.TaskStatusPending {
		t.Fatalf("test task status: got %s", testTask.Status)
	}
	contextView, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{
		TaskID: testTask.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get persisted execution context: %v", err)
	}
	if len(contextView.Workflow.UpstreamTasks) != 2 {
		t.Fatalf("upstream tasks: %#v", contextView.Workflow.UpstreamTasks)
	}

	claim, err = service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: testTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim test: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: testTask.ID, ClaimID: claim.ID, Identity: agent, Result: "Tests passed",
	}); err != nil {
		t.Fatalf("submit test: %v", err)
	}

	err = repository.View(ctx, func(store application.ReadStore) error {
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Status != domain.WorkItemStatusCompleted || persisted.CompletedAt == nil {
			return fmt.Errorf("persisted work item is %#v", persisted)
		}
		sequence, err := store.LastWorkItemEventSequence(workItem.ID)
		if err != nil {
			return err
		}
		if sequence < 10 {
			return fmt.Errorf("event sequence %d is unexpectedly short", sequence)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify completed workflow: %v", err)
	}
}

func testTransactionRollback(
	t *testing.T,
	repository *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	workItem := domain.WorkItem{
		ID: "rolled-back-work", Definition: definition.Binding(), Status: domain.WorkItemStatusOpen,
		Title: "Rollback", Goal: "Must not persist", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
	}
	sentinel := errors.New("rollback sentinel")
	err := repository.Update(ctx, func(store application.WriteStore) error {
		if err := store.CreateWorkItem(workItem); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error: got %v", err)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		_, err := store.GetWorkItem(workItem.ID)
		return err
	})
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("rolled back work item: got %v", err)
	}
}

func testConcurrentClaim(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	creator := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "creator"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: creator, Title: "Concurrent claim", Goal: "Choose one owner",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: creator, Title: "Claim once", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-b"}, Role: "generalist"},
	}
	services := []*application.Service{service, repositoryTestService(t, peer)}
	start := make(chan struct{})
	errorsByAgent := make([]error, len(identities))
	var wait sync.WaitGroup
	for index, identity := range identities {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByAgent[index] = services[index].ClaimTask(ctx, application.ClaimTaskCommand{
				TaskID: task.ID, Identity: identity,
			})
		}()
	}
	close(start)
	wait.Wait()

	succeeded := 0
	conflicted := 0
	for _, err := range errorsByAgent {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, application.ErrConflict):
			conflicted++
		default:
			t.Fatalf("claim result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("claim results: success=%d conflict=%d", succeeded, conflicted)
	}
}

func repositoryTaskByDefinition(
	t *testing.T,
	repository *SQLRepository,
	workItemID domain.WorkItemID,
	definitionID domain.WorkflowTaskID,
) domain.Task {
	t.Helper()
	var found domain.Task
	err := repository.View(context.Background(), func(store application.ReadStore) error {
		tasks, err := store.ListTasks(workItemID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.WorkflowTaskID != nil && *task.WorkflowTaskID == definitionID {
				found = task
				return nil
			}
		}
		return application.ErrNotFound
	})
	if err != nil {
		t.Fatalf("find workflow task %q: %v", definitionID, err)
	}
	return found
}

type repositoryTestClock struct{}

func (repositoryTestClock) Now() time.Time { return repositoryTestTime }

type repositoryTestIDs struct{}

func (g *repositoryTestIDs) NewID() string {
	return fmt.Sprintf("sql-generated-%d", repositoryTestIDSequence.Add(1))
}

func repositoryTestService(t *testing.T, repository application.Repository) *application.Service {
	t.Helper()
	service, err := application.NewService(repository, repositoryTestClock{}, &repositoryTestIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func repositoryWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "sql-workflow", Version: 1, Name: "SQL workflow", Status: domain.DefinitionStatusPublished,
			CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "test", Title: "Test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "docs-test", FromTaskID: "docs", ToTaskID: "test"},
			},
		},
	}
}

func repositoryBlackboardDefinition() domain.BlackboardDefinition {
	return domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{
		ID: "sql-blackboard", Version: 1, Name: "SQL blackboard", Status: domain.DefinitionStatusPublished,
		CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
	}}
}
