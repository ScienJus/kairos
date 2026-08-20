package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
		t.Run("definition metadata batch", func(t *testing.T) {
			testDefinitionMetadataBatch(t, repository, workflow, blackboard)
		})
		t.Run("blackboard lifecycle candidates", func(t *testing.T) {
			testBlackboardLifecycleCandidates(t, repository, blackboard)
		})
		t.Run("rollback", func(t *testing.T) {
			testTransactionRollback(t, repository, blackboard)
		})
		t.Run("concurrent claim", func(t *testing.T) {
			testConcurrentClaim(t, repository, openPeer(t), blackboard)
		})
		t.Run("claim lease columns", func(t *testing.T) {
			testClaimLeaseColumns(t, repository, blackboard)
		})
		t.Run("concurrent blackboard appends", func(t *testing.T) {
			testConcurrentBlackboardPlanning(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent idempotent planning", func(t *testing.T) {
			testConcurrentIdempotentPlanning(t, repository, openPeer(t), blackboard)
		})
		t.Run("hierarchy closure race", func(t *testing.T) {
			testHierarchyClosureRace(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent child appends", func(t *testing.T) {
			testConcurrentChildAppends(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent reciprocal relations", func(t *testing.T) {
			testConcurrentReciprocalRelations(t, repository, openPeer(t), blackboard)
		})
		if repository.dialect == dialectPostgres {
			t.Run("independent work items do not block", func(t *testing.T) {
				testIndependentWorkItemsDoNotBlock(t, repository, openPeer(t), blackboard)
			})
		}
	})
}

func testDefinitionMetadataBatch(t *testing.T, repository *SQLRepository, workflow domain.WorkflowDefinition, blackboard domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	bindings := []domain.DefinitionBinding{workflow.Binding(), blackboard.Binding()}
	var metadata map[domain.DefinitionBinding]domain.DefinitionMetadata
	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		metadata, err = store.GetDefinitionMetadata(bindings)
		return err
	}); err != nil {
		t.Fatalf("get definition metadata: %v", err)
	}
	if len(metadata) != 2 || metadata[workflow.Binding()].Name != workflow.Name || metadata[blackboard.Binding()].Name != blackboard.Name {
		t.Fatalf("definition metadata = %#v", metadata)
	}

	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		metadata, err = store.GetDefinitionMetadata(nil)
		return err
	}); err != nil {
		t.Fatalf("get empty definition metadata: %v", err)
	}
	if metadata == nil || len(metadata) != 0 {
		t.Fatalf("empty definition metadata = %#v, want non-nil empty map", metadata)
	}
}

func testBlackboardLifecycleCandidates(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "lifecycle-agent"}, Role: "generalist"}

	createWorkItem := func(title string, acceptanceMode domain.WorkItemAcceptanceMode) domain.WorkItem {
		t.Helper()
		workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
			Definition: definition.Binding(), Identity: agent, Title: title, Goal: "Exercise lifecycle discovery", AcceptanceMode: acceptanceMode,
		})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return workItem
	}

	empty := createWorkItem("Empty lifecycle board", domain.WorkItemAcceptanceNone)
	pending := createWorkItem("Pending lifecycle board", domain.WorkItemAcceptanceNone)
	if _, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: pending.ID, Identity: agent, Title: "Pending task", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	converged := createWorkItem("Converged lifecycle board", domain.WorkItemAcceptanceNone)
	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: converged.ID, Identity: agent, Title: "Completed task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim completed task: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done",
	}); err != nil {
		t.Fatalf("submit completed task: %v", err)
	}
	skipped := createWorkItem("Skipped lifecycle board", domain.WorkItemAcceptanceNone)
	skippedTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: skipped.ID, Identity: agent, Title: "Obsolete task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create skipped task: %v", err)
	}
	if _, err := service.SkipBlackboardTask(ctx, application.SkipBlackboardTaskCommand{
		TaskID: skippedTask.ID, Identity: agent, Reason: "no longer needed",
	}); err != nil {
		t.Fatalf("skip task: %v", err)
	}

	agentAcceptance := createWorkItem("Agent acceptance lifecycle board", domain.WorkItemAcceptanceAgent)
	if _, err := service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{
		WorkItemID: agentAcceptance.ID, Identity: agent, Result: "ready for agent acceptance",
	}); err != nil {
		t.Fatalf("submit agent completion: %v", err)
	}
	humanAcceptance := createWorkItem("Human acceptance lifecycle board", domain.WorkItemAcceptanceHuman)
	if _, err := service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{
		WorkItemID: humanAcceptance.ID, Identity: agent, Result: "ready for human acceptance",
	}); err != nil {
		t.Fatalf("submit human completion: %v", err)
	}

	var candidates []domain.WorkItem
	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		candidates, err = store.ListBlackboardsAwaitingLifecycleDecision()
		return err
	}); err != nil {
		t.Fatalf("list lifecycle candidates: %v", err)
	}
	found := make(map[domain.WorkItemID]bool, len(candidates))
	for _, candidate := range candidates {
		found[candidate.ID] = true
	}
	if !found[converged.ID] || !found[skipped.ID] || !found[agentAcceptance.ID] {
		t.Fatalf("lifecycle candidates = %+v, want completed %q, skipped %q, and agent acceptance %q", candidates, converged.ID, skipped.ID, agentAcceptance.ID)
	}
	for _, excluded := range []domain.WorkItemID{empty.ID, pending.ID, humanAcceptance.ID} {
		if found[excluded] {
			t.Fatalf("lifecycle candidates unexpectedly include %q: %+v", excluded, candidates)
		}
	}
}

func testClaimLeaseColumns(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "lease-agent"}, Role: "generalist"}
	human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "lease-human"}}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: agent, Title: "Lease columns", Goal: "Persist claim ownership"})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	agentTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Agent", Executor: domain.ExecutorAgent})
	if err != nil {
		t.Fatalf("create agent task: %v", err)
	}
	humanTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Human", Executor: domain.ExecutorHuman})
	if err != nil {
		t.Fatalf("create human task: %v", err)
	}
	agentClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: agentTask.ID, Identity: agent, LeaseSeconds: 300})
	if err != nil {
		t.Fatalf("claim agent task: %v", err)
	}
	humanClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: humanTask.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim human task: %v", err)
	}

	assertColumns := func(id domain.ClaimID, wantKind, wantID string, wantLease bool) {
		t.Helper()
		var kind, executorID string
		var heartbeat, until, seconds sql.NullInt64
		query := rebind(repository.dialect, "SELECT executor_kind, executor_id, last_heartbeat_at_ns, lease_until_ns, lease_seconds FROM claims WHERE id = ?")
		if err := repository.db.QueryRowContext(ctx, query, id).Scan(&kind, &executorID, &heartbeat, &until, &seconds); err != nil {
			t.Fatalf("query claim columns: %v", err)
		}
		if kind != wantKind || executorID != wantID {
			t.Fatalf("executor columns = %s/%s, want %s/%s", kind, executorID, wantKind, wantID)
		}
		if heartbeat.Valid != wantLease || until.Valid != wantLease || seconds.Valid != wantLease {
			t.Fatalf("lease nullability = %v/%v/%v, want %v", heartbeat.Valid, until.Valid, seconds.Valid, wantLease)
		}
	}
	assertColumns(agentClaim.ID, "agent", "lease-agent", true)
	assertColumns(humanClaim.ID, "human", "lease-human", false)

	listReapable := func(now time.Time) []domain.TaskID {
		t.Helper()
		var result []domain.TaskID
		if err := repository.View(ctx, func(store application.ReadStore) error {
			var err error
			result, err = store.ListReapableAgentClaimTasks(now)
			return err
		}); err != nil {
			t.Fatalf("list reapable claims: %v", err)
		}
		return result
	}
	if got := listReapable(agentClaim.LeaseUntil.Add(-time.Nanosecond)); slices.Contains(got, agentTask.ID) {
		t.Fatalf("reapable before deadline = %v, unexpectedly contains %q", got, agentTask.ID)
	}
	if got := listReapable(agentClaim.LeaseUntil); !slices.Contains(got, agentTask.ID) || slices.Contains(got, humanTask.ID) {
		t.Fatalf("reapable at deadline = %v, want agent task %q without human task %q", got, agentTask.ID, humanTask.ID)
	}
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
				idempotency_records,
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
		WorkItemID: workItem.ID,
		Identity:   creator, Title: "Claim once", Executor: domain.ExecutorAgent,
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

func testIndependentWorkItemsDoNotBlock(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	identity := application.Identity{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "parallel-work-creator"},
		Role:  "generalist",
	}
	first, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identity, Title: "First parallel work", Goal: "Hold one row lock",
	})
	if err != nil {
		t.Fatalf("create first work item: %v", err)
	}
	second, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identity, Title: "Second parallel work", Goal: "Acquire another row lock",
	})
	if err != nil {
		t.Fatalf("create second work item: %v", err)
	}

	firstLocked := make(chan error, 1)
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- repository.Update(ctx, func(store application.WriteStore) error {
			_, err := store.GetWorkItem(first.ID)
			firstLocked <- err
			if err != nil {
				return err
			}
			<-releaseFirst
			return nil
		})
	}()
	if err := <-firstLocked; err != nil {
		close(releaseFirst)
		<-firstDone
		t.Fatalf("lock first work item: %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- peer.Update(ctx, func(store application.WriteStore) error {
			_, err := store.GetWorkItem(second.ID)
			return err
		})
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			close(releaseFirst)
			<-firstDone
			t.Fatalf("lock independent work item: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		<-firstDone
		t.Fatal("an independent work item was blocked by another work item's transaction")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("finish first work item transaction: %v", err)
	}
}

func testConcurrentBlackboardPlanning(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	planners := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: planners[0],
		Title: "Concurrent planning", Goal: "Create one coherent first task",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
				WorkItemID: workItem.ID,
				Identity:   planners[index], Title: fmt.Sprintf("Plan part %d", index+1), Executor: domain.ExecutorAgent,
			})
		}()
	}
	close(start)
	wait.Wait()

	succeeded := 0
	for _, err := range errorsByPlanner {
		switch {
		case err == nil:
			succeeded++
		default:
			t.Fatalf("planning result: %v", err)
		}
	}
	if succeeded != 2 {
		t.Fatalf("planning results: success=%d", succeeded)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Version != 2 {
			return fmt.Errorf("work item version is %d, want 2", persisted.Version)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			return fmt.Errorf("task count is %d, want 2", len(tasks))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify concurrent planning: %v", err)
	}
}

func testConcurrentIdempotentPlanning(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	planner := application.Identity{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "idempotent-planner"}, Role: "generalist",
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: planner,
		Title: "Idempotent planning", Goal: "Retry one task creation safely",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	command := application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   planner, OperationID: "add-login-task",
		Title: "Implement login", Executor: domain.ExecutorAgent,
	}

	start := make(chan struct{})
	results := make([]domain.Task, len(services))
	errorsByRequest := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errorsByRequest[index] = service.CreateBlackboardTask(ctx, command)
		}()
	}
	close(start)
	wait.Wait()
	for _, err := range errorsByRequest {
		if err != nil {
			t.Fatalf("idempotent planning result: %v", err)
		}
	}
	if results[0].ID != results[1].ID {
		t.Fatalf("idempotent task ids differ: %q and %q", results[0].ID, results[1].ID)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("task count is %d, want 1", len(tasks))
		}
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Version != 1 {
			return fmt.Errorf("work item version is %d, want 1", persisted.Version)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify idempotent planning: %v", err)
	}
}

func testHierarchyClosureRace(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "hierarchy-owner"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "hierarchy-planner"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Hierarchy race", Goal: "Resolve append and closure atomically",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Deliver feature", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	rootClaim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: root.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	decomposition, err := services[0].DecomposeBlackboardTask(ctx, application.DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: rootClaim.ID, Identity: identities[0],
		Children: []application.BlackboardTaskSpec{{Title: "Implement feature", Executor: domain.ExecutorAgent}},
	})
	if err != nil {
		t.Fatalf("decompose root task: %v", err)
	}
	child := decomposition.Children[0]
	childClaim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: child.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim child task: %v", err)
	}

	start := make(chan struct{})
	submitDone := make(chan error, 1)
	addDone := make(chan struct {
		task domain.Task
		err  error
	}, 1)
	go func() {
		<-start
		_, err := services[0].SubmitTask(ctx, application.SubmitTaskCommand{
			TaskID: child.ID, ClaimID: childClaim.ID, Identity: identities[0], Result: "Feature implemented",
		})
		submitDone <- err
	}()
	go func() {
		<-start
		task, err := services[1].AddBlackboardChildTask(ctx, application.AddBlackboardChildTaskCommand{
			ParentTaskID: root.ID, Identity: identities[1],
			Task: application.BlackboardTaskSpec{Title: "Late integration check", Executor: domain.ExecutorAgent},
		})
		addDone <- struct {
			task domain.Task
			err  error
		}{task: task, err: err}
	}()
	close(start)
	if err := <-submitDone; err != nil {
		t.Fatalf("submit child: %v", err)
	}
	addResult := <-addDone
	if addResult.err != nil && !errors.Is(addResult.err, application.ErrConflict) {
		t.Fatalf("append child: %v", addResult.err)
	}

	err = repository.View(ctx, func(store application.ReadStore) error {
		parent, err := store.GetTask(root.ID)
		if err != nil {
			return err
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if addResult.err == nil {
			if parent.Status != domain.TaskStatusWaitingChildren {
				return fmt.Errorf("parent status is %s after successful append", parent.Status)
			}
			if addResult.task.ParentTaskID == nil || *addResult.task.ParentTaskID != root.ID {
				return fmt.Errorf("appended task has parent %v", addResult.task.ParentTaskID)
			}
		} else if parent.Status != domain.TaskStatusCompleted {
			return fmt.Errorf("parent status is %s after closure won", parent.Status)
		}
		return domain.ValidateBlackboardTaskHierarchy(workItem.ID, tasks)
	})
	if err != nil {
		t.Fatalf("verify hierarchy closure race: %v", err)
	}
}

func testConcurrentChildAppends(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "child-planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "child-planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Concurrent child planning", Goal: "Keep both useful child tasks",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Implement login", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	claim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: root.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	if _, err := services[0].DecomposeBlackboardTask(ctx, application.DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: claim.ID, Identity: identities[0],
		Children: []application.BlackboardTaskSpec{{Title: "Initial implementation", Executor: domain.ExecutorAgent}},
	}); err != nil {
		t.Fatalf("decompose root task: %v", err)
	}

	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.AddBlackboardChildTask(ctx, application.AddBlackboardChildTaskCommand{
				ParentTaskID: root.ID, Identity: identities[index],
				Task: application.BlackboardTaskSpec{
					Title: fmt.Sprintf("Concurrent child %d", index+1), Executor: domain.ExecutorAgent,
				},
			})
		}()
	}
	close(start)
	wait.Wait()
	for _, err := range errorsByPlanner {
		if err != nil {
			t.Fatalf("concurrent child append: %v", err)
		}
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		parent, err := store.GetTask(root.ID)
		if err != nil {
			return err
		}
		if parent.Status != domain.TaskStatusWaitingChildren {
			return fmt.Errorf("parent status is %s", parent.Status)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		children := 0
		for _, task := range tasks {
			if task.ParentTaskID != nil && *task.ParentTaskID == root.ID {
				children++
			}
		}
		if children != 3 {
			return fmt.Errorf("child count is %d, want 3", children)
		}
		return domain.ValidateBlackboardTaskHierarchy(workItem.ID, tasks)
	})
	if err != nil {
		t.Fatalf("verify concurrent child appends: %v", err)
	}
}

func testConcurrentReciprocalRelations(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "relation-planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "relation-planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Concurrent relations", Goal: "Keep the suggested graph acyclic",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	first, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "First task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Second task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}

	commands := []application.AddBlackboardRelationCommand{
		{WorkItemID: workItem.ID, FromTaskID: first.ID, ToTaskID: second.ID, Identity: identities[0]},
		{WorkItemID: workItem.ID, FromTaskID: second.ID, ToTaskID: first.ID, Identity: identities[1]},
	}
	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.AddBlackboardRelation(ctx, commands[index])
		}()
	}
	close(start)
	wait.Wait()
	succeeded := 0
	rejected := 0
	for _, err := range errorsByPlanner {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrInvalidModel):
			rejected++
		default:
			t.Fatalf("concurrent relation result: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("relation results: success=%d rejected=%d", succeeded, rejected)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		relations, err := store.ListTaskRelations(workItem.ID)
		if err != nil {
			return err
		}
		if len(relations) != 1 {
			return fmt.Errorf("relation count is %d, want 1", len(relations))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify reciprocal relations: %v", err)
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
