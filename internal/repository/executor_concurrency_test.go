package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

// Hooks surround real SQL transactions; no domain operation or lock is mocked.
type executorTransactionObserver struct {
	application.Repository
	started      func()
	beforeCommit func() error
	startOnce    sync.Once
	commitOnce   sync.Once
}

func (r *executorTransactionObserver) Update(ctx context.Context, operation func(application.WriteStore) error) error {
	if r.started != nil {
		r.startOnce.Do(r.started)
	}
	return r.Repository.Update(ctx, func(store application.WriteStore) error {
		if err := operation(store); err != nil {
			return err
		}
		var err error
		if r.beforeCommit != nil {
			r.commitOnce.Do(func() { err = r.beforeCommit() })
		}
		return err
	})
}

func TestExecutorMutationTerminationTransactions(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		blackboard := repositoryBlackboardDefinition()
		workflow := repositoryWorkflowDefinition()
		workflow.ID = "executor-terminal"
		workflow.Graph.Tasks = workflow.Graph.Tasks[:1]
		workflow.Graph.Relations = nil
		if err := workflow.Validate(); err != nil {
			t.Fatalf("invalid terminal Workflow fixture: %v", err)
		}
		if err := repo.CreateBlackboardDefinition(context.Background(), blackboard); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(context.Background(), workflow); err != nil {
			t.Fatal(err)
		}
		peer := openPeer(t)
		for _, mutation := range []string{"artifact", "planning"} {
			for _, ending := range []string{"release", "reaper", "cancel", "fail", "complete"} {
				for _, order := range []string{"mutation_first", "termination_first", "mutation_rollback"} {
					t.Run(mutation+"/"+ending+"/"+order, func(t *testing.T) {
						testExecutorTerminationOrder(t, repo, peer, blackboard, workflow, mutation, ending, order)
					})
				}
			}
		}
	})
}

func testExecutorTerminationOrder(t *testing.T, repo, peer *SQLRepository, blackboard domain.BlackboardDefinition, workflow domain.WorkflowDefinition, mutation, ending, order string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setup := repositoryTestService(t, repo)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: domain.ActorID(t.Name())}, Role: "backend"}
	human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	binding := blackboard.Binding()
	if ending == "complete" && mutation == "artifact" {
		binding = workflow.Binding()
	}
	workItem, err := setup.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: binding, Identity: agent, Title: "Executor fencing", Goal: "Serialize writes and termination"})
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if binding.Mode == domain.CoordinationModeWorkflow {
		view, err := setup.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: workItem.ID, Identity: agent})
		if err != nil || len(view.Tasks) != 1 {
			t.Fatalf("terminal Workflow tasks=%v err=%v", view.Tasks, err)
		}
		task = view.Tasks[0]
	} else {
		task, err = createBlackboardTaskForTest(setup, ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Bound task", Executor: domain.ExecutorAgent})
		if err != nil {
			t.Fatal(err)
		}
	}
	seed := sha256.Sum256([]byte(t.Name()))
	token := identity.ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(seed[:])
	claim, err := setup.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent, ExecutorToken: token, LeaseSeconds: 15})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := setup.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	baseTaskCount := 1
	failureTask, failureClaim := task, claim
	if ending == "fail" {
		failureTask, err = setup.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Failure source", Executor: domain.ExecutorAgent})
		if err != nil {
			t.Fatal(err)
		}
		failureClaim, err = setup.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: failureTask.ID, Identity: agent})
		if err != nil {
			t.Fatal(err)
		}
		baseTaskCount++
	}

	ready, proceed, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(proceed) }) }
	defer unblock()
	rollback := errors.New("executor mutation rollback")
	firstRepo := &executorTransactionObserver{Repository: repo, beforeCommit: func() error {
		close(ready)
		select {
		case <-proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
		if order == "mutation_rollback" {
			return rollback
		}
		return nil
	}}
	secondRepo := &executorTransactionObserver{Repository: peer, started: func() { close(secondStarted) }}
	var mutationRepo, endingRepo application.Repository = firstRepo, secondRepo
	if order == "termination_first" {
		mutationRepo, endingRepo = secondRepo, firstRepo
	}
	writer := repositoryTestService(t, mutationRepo)
	ender, err := application.NewService(endingRepo, repositoryTestClockAt{now: repositoryTestTime.Add(time.Minute)}, &repositoryTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	const operationID = "executor-write"
	write := func() error {
		if mutation == "artifact" {
			_, err := writer.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: principal, OperationID: operationID, Name: "result", URI: "https://example.test/result"})
			return err
		}
		_, err := writer.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: principal, OperationID: operationID, Title: "Durable follow-up", Executor: domain.ExecutorAgent})
		return err
	}
	end := func() error {
		switch ending {
		case "release":
			return ender.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent})
		case "reaper":
			return ender.ReapExpiredClaims(ctx)
		case "cancel":
			_, err := ender.CancelWorkItem(ctx, application.CancelWorkItemCommand{WorkItemID: workItem.ID, Identity: human, Reason: "Stop work"})
			return err
		case "fail":
			_, err := ender.FailTask(ctx, application.FailTaskCommand{TaskID: failureTask.ID, ClaimID: failureClaim.ID, Identity: agent, Action: domain.TaskFailureFailWorkItem, Reason: "Business failure"})
			return err
		case "complete":
			if _, err := ender.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "Done"}); err != nil {
				return err
			}
			if mutation == "planning" {
				_, err := ender.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{WorkItemID: workItem.ID, Identity: human, Result: "Done"})
				// A committed plan survives the Claim and blocks premature completion.
				if order == "mutation_first" {
					if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "not ready for completion") {
						return fmt.Errorf("completion with pending plan: %v", err)
					}
					return nil
				}
				return err
			}
			return nil
		}
		return fmt.Errorf("unknown ending %s", ending)
	}
	first, second := write, end
	if order == "termination_first" {
		first, second = end, write
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first() }()
	select {
	case <-ready:
	case err := <-firstDone:
		t.Fatalf("first operation failed before reaching commit: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() { secondDone <- second() }()
	select {
	case <-secondStarted:
	case err := <-secondDone:
		t.Fatalf("second operation failed before starting transaction: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-secondDone:
		t.Fatalf("competing operation completed before the held transaction ended: %v", err)
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	unblock()
	writeErr, endErr := <-firstDone, <-secondDone
	if order == "termination_first" {
		writeErr, endErr = endErr, writeErr
	}
	if endErr != nil {
		t.Fatalf("termination failed: %v", endErr)
	}
	var expectedWriteErr error
	if order == "termination_first" {
		expectedWriteErr = identity.ErrUnauthenticated
	} else if order == "mutation_rollback" {
		expectedWriteErr = rollback
	}
	if !errors.Is(writeErr, expectedWriteErr) {
		t.Fatalf("mutation error=%v want=%v", writeErr, expectedWriteErr)
	}
	committed := order == "mutation_first"
	if err := repo.View(ctx, func(store application.ReadStore) error {
		wi, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		wantStatus := domain.WorkItemStatusOpen
		switch ending {
		case "cancel":
			wantStatus = domain.WorkItemStatusCancelled
		case "fail":
			wantStatus = domain.WorkItemStatusFailed
		case "complete":
			if mutation != "planning" || !committed {
				wantStatus = domain.WorkItemStatusCompleted
			}
		}
		if wi.Status != wantStatus {
			return fmt.Errorf("WorkItem status=%s want=%s", wi.Status, wantStatus)
		}
		persisted, err := store.GetTask(task.ID)
		if err != nil {
			return err
		}
		wantTaskStatus := domain.TaskStatusPending
		if ending == "complete" {
			wantTaskStatus = domain.TaskStatusCompleted
		}
		if persisted.Status != wantTaskStatus || persisted.ActiveClaimID != nil {
			return fmt.Errorf("unexpected bound Task state: %+v", persisted)
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return err
		}
		reasons := map[string]domain.ClaimEndReason{"release": domain.ClaimEndReleased, "reaper": domain.ClaimEndExpired, "cancel": domain.ClaimEndWorkItemCancelled, "fail": domain.ClaimEndRevoked, "complete": domain.ClaimEndTaskCompleted}
		if len(claims) != 1 || claims[0].Active() || claims[0].EndReason != reasons[ending] {
			return fmt.Errorf("unexpected Claim history: %+v", claims)
		}
		if err := domain.ValidateTaskContext(binding.Mode, persisted, claims); err != nil {
			return err
		}
		allClaims, err := store.ListClaimsByWorkItem(wi.ID)
		if err != nil {
			return err
		}
		for _, endedClaim := range allClaims {
			if endedClaim.Active() {
				return fmt.Errorf("termination left an active Claim: %s", endedClaim.ID)
			}
		}
		artifacts, err := store.ListArtifacts(application.ArtifactFilter{WorkItemID: wi.ID})
		if err != nil {
			return err
		}
		wantArtifacts := 0
		if committed && mutation == "artifact" {
			wantArtifacts = 1
		}
		if len(artifacts) != wantArtifacts {
			return fmt.Errorf("Artifacts=%d want=%d", len(artifacts), wantArtifacts)
		}
		tasks, err := store.ListTasks(wi.ID)
		if err != nil {
			return err
		}
		wantTasks := baseTaskCount
		if committed && mutation == "planning" {
			wantTasks++
		}
		if len(tasks) != wantTasks {
			return fmt.Errorf("Tasks=%d want=%d", len(tasks), wantTasks)
		}
		record, err := store.GetIdempotencyRecord(agent.Actor, operationID)
		if !committed {
			if !errors.Is(err, application.ErrNotFound) {
				return fmt.Errorf("rejected/rolled-back write retained idempotency: record=%+v err=%v", record, err)
			}
			return nil
		}
		if err != nil {
			return err
		}
		wantOperation := application.CreateArtifactOperation
		if mutation == "planning" {
			wantOperation = "create_blackboard_task"
		}
		if record.Status != application.IdempotencyCompleted || record.Operation != wantOperation || record.Response == "" {
			return fmt.Errorf("unexpected idempotency record: %+v", record)
		}
		if mutation == "artifact" {
			var replay domain.Artifact
			if err := json.Unmarshal([]byte(record.Response), &replay); err != nil {
				return err
			}
			if replay.ID != artifacts[0].ID || replay.ClaimID != claim.ID || replay.WorkItemID != wi.ID {
				return fmt.Errorf("Artifact replay does not match committed row: %+v", replay)
			}
		} else {
			var replay domain.Task
			if err := json.Unmarshal([]byte(record.Response), &replay); err != nil {
				return err
			}
			persistedPlan, err := store.GetTask(replay.ID)
			if err != nil {
				return err
			}
			if persistedPlan.WorkItemID != wi.ID || persistedPlan.Title != "Durable follow-up" || persistedPlan.Status != domain.TaskStatusPending {
				return fmt.Errorf("Task replay does not match committed plan: %+v", persistedPlan)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Cached principals and completed idempotency records cannot revive execution.
	if err := write(); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("post-termination replay error=%v", err)
	}
	if _, err := setup.Authenticate(ctx, token); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("post-termination authentication error=%v", err)
	}
}
