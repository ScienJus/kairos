package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowRetryPreservesSuccessfulJoinInputs(t *testing.T) {
	for _, scenario := range []string{"a-fails", "both-fail", "c-fails", "workflow-fails", "automatic-retry"} {
		t.Run(scenario, func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				def := attemptDefinition()
				if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
					t.Fatal(err)
				}
				work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: scenario, Goal: "Reuse successful branches"})
				if err != nil {
					t.Fatal(err)
				}
				read := func() application.WorkItemExecutionContext {
					t.Helper()
					v, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					return v
				}
				pending := func(node domain.WorkflowTaskID) domain.Task {
					t.Helper()
					for _, task := range read().Tasks {
						if *task.WorkflowTaskID == node && task.Status == domain.TaskStatusPending {
							return task
						}
					}
					t.Fatalf("missing pending %s", node)
					return domain.Task{}
				}
				claim := func(task domain.Task) domain.Claim {
					t.Helper()
					c, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					return c
				}
				submit := func(task domain.Task) {
					t.Helper()
					c := claim(task)
					cmd := application.SubmitTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Result: "Useful result " + task.Title}
					if *task.WorkflowTaskID != "c" {
						cmd.Transition = &application.WorkflowTransitionCommand{ChoiceGroupID: domain.WorkflowChoiceGroupID("exit:" + string(*task.WorkflowTaskID))}
					}
					if _, err := service.SubmitTask(ctx, cmd); err != nil {
						t.Fatal(err)
					}
				}
				fail := func(task domain.Task, action domain.TaskFailureAction) domain.Claim {
					t.Helper()
					c := claim(task)
					if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Action: action, Reason: "执行失败，请复用已有成果"}); err != nil {
						t.Fatal(err)
					}
					return c
				}
				retry := func(task domain.Task) domain.Task {
					t.Helper()
					command := application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: read().WorkItem.Version, Instructions: "先检查已有 PR"}
					_, err := repositoryTestService(t, openPeer(t)).ContinueWorkflow(ctx, command)
					if err != nil {
						t.Fatal(err)
					}
					assertStoredRecoveryEvent(t, repo, work.ID, "work_item.continued")
					var replacement domain.Task
					for _, candidate := range read().Tasks {
						if candidate.RetryOfTaskID != nil && *candidate.RetryOfTaskID == task.ID {
							replacement = candidate
						}
					}
					if replacement.ID == task.ID || replacement.RetryOfTaskID == nil || *replacement.RetryOfTaskID != task.ID || replacement.RetryInstructions != "先检查已有 PR" || !strings.Contains(replacement.RetryContext, "执行失败") {
						t.Fatalf("retry context/source: %+v", replacement)
					}
					if _, err := service.ContinueWorkflow(ctx, command); !errors.Is(err, application.ErrConflict) {
						t.Fatalf("duplicate retry: %v", err)
					}
					return replacement
				}
				a, b := pending("a"), pending("b")
				var oldFailed domain.Task
				var oldClaim domain.Claim
				switch scenario {
				case "a-fails":
					oldClaim = fail(a, domain.TaskFailureAwaitHuman)
					submit(b)
					oldFailed = a
					submit(retry(a))
				case "both-fail":
					oldClaim = fail(a, domain.TaskFailureAwaitHuman)
					fail(b, domain.TaskFailureAwaitHuman)
					oldFailed = a
					submit(retry(a))
					if len(read().Tasks) != 4 {
						t.Fatal("join ran before B retry")
					}
					submit(pending("b"))
				case "c-fails":
					submit(a)
					submit(b)
					c := pending("c")
					oldClaim = fail(c, domain.TaskFailureAwaitHuman)
					oldFailed = c
					submit(retry(c))
				case "workflow-fails":
					submit(b)
					oldClaim = fail(a, domain.TaskFailureFailWorkItem)
					oldFailed = a
					failed := read()
					if failed.WorkItem.Status != domain.WorkItemStatusFailed {
						t.Fatal("expected workflow failure")
					}
					if _, err := service.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.WorkItem.Version}); err != nil {
						t.Fatal(err)
					}
					submit(pending("a"))
				case "automatic-retry":
					oldClaim = fail(a, domain.TaskFailureRetry)
					oldFailed = a
					submit(b)
					replacement := pending("a")
					if replacement.ID == a.ID {
						t.Fatal("retryed original task")
					}
					submit(replacement)
				}
				if scenario != "c-fails" {
					submit(pending("c"))
				}
				final := read()
				if final.WorkItem.Status != domain.WorkItemStatusCompleted {
					t.Fatalf("old failure blocks completion: %s", final.WorkItem.Status)
				}
				if _, err := service.HeartbeatClaim(ctx, application.HeartbeatClaimCommand{TaskID: oldFailed.ID, ClaimID: oldClaim.ID, Identity: actor}); !errors.Is(err, application.ErrConflict) {
					t.Fatalf("old claim revived: %v", err)
				}
				var finalC domain.Task
				for _, task := range final.Tasks {
					if task.ID == oldFailed.ID && task.Status != domain.TaskStatusFailed {
						t.Fatal("old failure overwritten")
					}
					if *task.WorkflowTaskID == "c" && task.Status == domain.TaskStatusCompleted {
						finalC = task
					}
				}
				if scenario != "c-fails" {
					for _, r := range final.Relations {
						if r.ToTaskID == finalC.ID && r.FromTaskID == oldFailed.ID {
							t.Fatal("join used failed source")
						}
					}
				}
				if scenario == "a-fails" || scenario == "workflow-fails" || scenario == "automatic-retry" {
					count := 0
					for _, task := range final.Tasks {
						if *task.WorkflowTaskID == "b" {
							count++
						}
					}
					if count != 1 {
						t.Fatal("successful B was rerun")
					}
					found := false
					for _, r := range final.Relations {
						if r.ToTaskID == finalC.ID && r.FromTaskID == b.ID {
							found = true
						}
					}
					if !found {
						t.Fatal("join lost successful B1")
					}
				}
			})
		})
	}
}

func attemptDefinition() domain.WorkflowDefinition {
	def := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "attempts", Version: 1, Name: "Attempts", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}, Graph: domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"a", "b"}, MaxTaskInstancesPerNode: 3, Relations: []domain.WorkflowRelationDefinition{{ID: "ac", FromTaskID: "a", ToTaskID: "c"}, {ID: "bc", FromTaskID: "b", ToTaskID: "c"}}}}
	for _, id := range []domain.WorkflowTaskID{"a", "b", "c"} {
		def.Graph.Tasks = append(def.Graph.Tasks, domain.WorkflowTaskDefinition{ID: id, Title: string(id), Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
	}
	return def
}

func TestWorkflowStartOverCopiesIntentNotExecution(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Original", Goal: "Ship feature", Context: "Repository context", Constraints: "Do not deploy", AcceptanceCriteria: "Reviewed", Tags: []string{"github"}})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		task := initial.Tasks[0]
		c, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Action: domain.TaskFailureFailWorkItem, Reason: strings.Repeat("界", domain.MaxHistoryTextBytes/3)}); err != nil {
			t.Fatal(err)
		}
		failed, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		def.Version = 2
		def.Name = "New incompatible version"
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		notes := strings.Repeat("界", domain.MaxHistoryTextBytes/3) + "xx"
		command := application.StartOverWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.WorkItem.Version, OperationID: "start-over-once", Instructions: notes}
		fresh, err := repositoryTestService(t, openPeer(t)).StartOverWorkflow(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.ID == work.ID || fresh.Definition != work.Definition || fresh.StartedOverFromWorkItemID == nil || *fresh.StartedOverFromWorkItemID != work.ID || fresh.Status != domain.WorkItemStatusOpen || fresh.Failure != nil || fresh.RecoveryInstructions != notes || fresh.Context != work.Context || fresh.Constraints != work.Constraints || len(fresh.StartOverContext) > domain.MaxHistoryTextBytes || !strings.Contains(fresh.StartOverContext, "full history") {
			t.Fatalf("start-over copy contract: %+v", fresh)
		}
		repeated, err := service.StartOverWorkflow(ctx, command)
		if err != nil || repeated.ID != fresh.ID {
			t.Fatalf("start-over idempotency: %s %v", repeated.ID, err)
		}

		assertStoredRecoveryEvent(t, repo, work.ID, "work_item.started_over")
		if err := repo.View(ctx, func(store application.ReadStore) error {
			record, err := store.GetIdempotencyRecord(actor.Actor, command.OperationID)
			if err != nil {
				return err
			}
			if record.Operation != "start_over_workflow" || record.Status != application.IdempotencyCompleted {
				return fmt.Errorf("unexpected start-over idempotency: %+v", record)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var sourceColumn string
		var limitColumn int
		if err := repo.db.QueryRowContext(ctx, rebind(repo.dialect, "SELECT started_over_from_work_item_id, workflow_max_task_instances_per_node FROM work_items WHERE id = ?"), fresh.ID).Scan(&sourceColumn, &limitColumn); err != nil {
			t.Fatal(err)
		}
		if sourceColumn != string(work.ID) || limitColumn != fresh.WorkflowMaxTaskInstancesPerNode {
			t.Fatalf("start-over columns = %s/%d", sourceColumn, limitColumn)
		}
		command.OperationID = "another"
		if _, err := service.StartOverWorkflow(ctx, command); !errors.Is(err, application.ErrConflict) {
			t.Fatalf("stale start-over: %v", err)
		}
		old, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if old.WorkItem.Status != domain.WorkItemStatusFailed || !reflect.DeepEqual(old.Tasks, failed.Tasks) || !reflect.DeepEqual(old.Claims, failed.Claims) {
			t.Fatal("start-over changed original execution")
		}
		current, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: fresh.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if len(current.Tasks) != 2 || len(current.Claims) != 0 || len(current.Relations) != 0 || len(current.Artifacts) != 0 {
			t.Fatal("copied execution state")
		}
		for _, task := range current.Tasks {
			if task.Status != domain.TaskStatusPending || task.RetryOfTaskID != nil || len(task.Submissions) != 0 || len(task.Reviews) != 0 {
				t.Fatal("copied task history")
			}
		}
	})
}

func TestWorkflowRetryLimitAndHumanAttention(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		def.Graph.MaxTaskInstancesPerNode = 1
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Retry cap", Goal: "Persist guard-specific failure"})
		if err != nil {
			t.Fatal(err)
		}
		read := func() application.WorkItemExecutionContext {
			t.Helper()
			v, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		task := read().Tasks[0]
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: "temporary failure"}); err != nil {
			t.Fatal(err)
		}
		failed := read()
		task = failed.Tasks[0]
		attention, err := service.ListHumanAttention(ctx, actor, application.PageRequest[application.HumanAttentionCursor]{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range attention.Items {
			if item.Task != nil && item.Task.ID == task.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("failed Workflow Task missing from Human Attention")
		}
		if _, err := service.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.WorkItem.Version}); !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "increase max_task_instances_per_node") {
			t.Fatalf("retry limit: %v", err)
		}
		if !reflect.DeepEqual(failed, read()) {
			t.Fatal("failed retry mutated persisted work")
		}
		_, err = repositoryTestService(t, openPeer(t)).ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.WorkItem.Version, MaxTaskInstancesPerNode: 2})
		if err != nil {
			t.Fatal(err)
		}
		var replacement domain.Task
		for _, candidate := range read().Tasks {
			if candidate.RetryOfTaskID != nil && *candidate.RetryOfTaskID == task.ID {
				replacement = candidate
			}
		}
		attention, err = service.ListHumanAttention(ctx, actor, application.PageRequest[application.HumanAttentionCursor]{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range attention.Items {
			if item.Task != nil && item.Task.ID == task.ID {
				t.Fatal("replaced failure remains actionable")
			}
		}
		claim, err = service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: replacement.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		failure, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: replacement.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: "retry also failed"})
		if err != nil {
			t.Fatal(err)
		}
		stopped := read()
		if failure.ID == "" || stopped.WorkItem.Status != domain.WorkItemStatusFailed || stopped.WorkItem.Failure == nil || stopped.WorkItem.Failure.Kind != domain.FailureWorkflowTaskInstanceLimit || stopped.WorkItem.Failure.TaskInstances != 2 || stopped.WorkItem.Failure.Limit != 2 {
			t.Fatalf("automatic retry did not commit cap failure: %+v", stopped.WorkItem)
		}
		if len(stopped.Tasks) != 3 || len(stopped.ActiveClaims) != 0 {
			t.Fatal("automatic retry exceeded cap or retained active Claim")
		}
		for _, v := range stopped.Tasks {
			if v.ID == replacement.ID && (v.Status != domain.TaskStatusFailed || len(v.Failures) != 1) {
				t.Fatal("automatic retry failure was not committed")
			}
		}
		if _, err := service.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: stopped.WorkItem.Version, MaxTaskInstancesPerNode: 3}); err != nil {
			t.Fatal(err)
		}
		if len(read().Tasks) != 4 {
			t.Fatal("continue did not create third attempt")
		}
	})
}

func TestWorkflowContinuePreservesUnaffectedBranches(t *testing.T) {
	for _, scenario := range []string{"released", "review-rejected", "working", "interrupted", "unclaimed"} {
		t.Run(scenario, func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				def := attemptDefinition()
				def.Graph.MaxTaskInstancesPerNode = 2
				def.Graph.Tasks[0].ReviewPolicy = domain.ReviewExecutorDecides
				if err := def.Validate(); err != nil {
					t.Fatal(err)
				}
				if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
					t.Fatal(err)
				}
				work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: scenario, Goal: "Continue only failed work"})
				if err != nil {
					t.Fatal(err)
				}
				read := func() application.WorkItemExecutionContext {
					t.Helper()
					v, e := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
					if e != nil {
						t.Fatal(e)
					}
					return v
				}
				claim := func(task domain.Task) domain.Claim {
					t.Helper()
					v, e := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
					if e != nil {
						t.Fatal(e)
					}
					return v
				}
				first := read()
				a, b := first.Tasks[0], first.Tasks[1]
				c := claim(a)
				if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: a.ID, ClaimID: c.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: "first attempt failed"}); err != nil {
					t.Fatal(err)
				}
				for _, task := range read().Tasks {
					if task.RetryOfTaskID != nil && *task.RetryOfTaskID == a.ID {
						a = task
						break
					}
				}
				if scenario != "unclaimed" {
					c = claim(a)
				}
				if scenario == "released" {
					if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: a.ID, ClaimID: c.ID, Identity: actor}); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "review-rejected" {
					if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: a.ID, ClaimID: c.ID, Identity: actor, Result: "needs review", RequestReview: true, Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "exit:a"}}); err != nil {
						t.Fatal(err)
					}
					for _, task := range read().Tasks {
						if task.ID == a.ID {
							a = task
						}
					}
					if _, err := service.DecideReview(ctx, application.DecideReviewCommand{TaskID: a.ID, ReviewID: a.Reviews[0].ID, Identity: actor, Decision: domain.ReviewStatusRejected, Feedback: "Preserve this correction"}); err != nil {
						t.Fatal(err)
					}
				}
				bc := claim(b)
				action := domain.TaskFailureFailWorkItem
				if scenario == "working" {
					action = domain.TaskFailureAwaitHuman
				}
				if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: b.ID, ClaimID: bc.ID, Identity: actor, Action: action, Reason: "B failed"}); err != nil {
					t.Fatal(err)
				}
				before := read()
				if !slices.Contains(before.RecoveryTaskIDs, b.ID) || slices.Contains(before.RecoveryTaskIDs, a.ID) != (scenario == "interrupted") {
					t.Fatalf("incorrect recovery projection: %v", before.RecoveryTaskIDs)
				}
				for _, task := range before.Tasks {
					if task.ID == a.ID {
						a = task
					}
				}
				command := application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: before.WorkItem.Version, MaxTaskInstancesPerNode: 2}
				if scenario == "interrupted" {
					if _, err := service.ContinueWorkflow(ctx, command); !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), `node "a" has 2`) {
						t.Fatalf("expected A retry limit: %v", err)
					}
					if !reflect.DeepEqual(before, read()) {
						t.Fatal("partial branch recovery committed on capacity conflict")
					}
					command.MaxTaskInstancesPerNode = 3
				}
				if _, err := repositoryTestService(t, openPeer(t)).ContinueWorkflow(ctx, command); err != nil {
					t.Fatal(err)
				}
				after := read()
				wantCount := 4
				if scenario == "interrupted" {
					wantCount = 5
				}
				if len(after.Tasks) != wantCount {
					t.Fatalf("unexpected replacements: %d", len(after.Tasks))
				}
				for _, task := range after.Tasks {
					if task.ID == a.ID && !reflect.DeepEqual(task, a) {
						t.Fatal("existing A attempt changed")
					}
					if task.RetryOfTaskID != nil && *task.RetryOfTaskID == a.ID && scenario != "interrupted" {
						t.Fatal("unaffected A was replaced")
					}
				}
				if scenario == "working" {
					if len(after.ActiveClaims) != 1 || after.ActiveClaims[0].ID != c.ID {
						t.Fatal("continuing B stopped A's active Claim")
					}
				} else if len(after.ActiveClaims) != 0 {
					t.Fatal("ended Claim revived")
				}
			})
		})
	}
}

func TestWorkflowStartOverPersistsCurrentFailureWithManyLinks(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Many historical links", Goal: "Retain current failure"})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		const reason = "CURRENT_FAILURE: merge conflict requires development"
		const oldArtifact = "https://example.com/old-artifact"
		var links strings.Builder
		for i := 0; i < 700; i++ {
			fmt.Fprintf(&links, "https://example.com/history/%03d\n", i)
		}
		for _, task := range initial.Tasks {
			claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			if *task.WorkflowTaskID == "a" {
				_, err = service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: reason})
			} else {
				artifact, createErr := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Name: "old-deliverable", URI: oldArtifact})
				if createErr != nil {
					t.Fatal(createErr)
				}
				_, err = service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Result: links.String(), ArtifactIDs: []domain.ArtifactID{artifact.ID}, Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "exit:b"}})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		current, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := service.StartOverWorkflow(ctx, application.StartOverWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: current.WorkItem.Version, OperationID: "start-over-many-links"})
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := repositoryTestService(t, openPeer(t)).GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: fresh.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{reason, "Start over uses the same Workflow version and initial nodes"} {
			if !strings.Contains(persisted.WorkItem.StartOverContext, required) {
				t.Fatalf("persisted start-over context lost %q", required)
			}
		}
		if strings.Contains(fresh.StartOverContext, "https://") || len(persisted.Artifacts) != 0 {
			t.Fatal("start-over copied old submission or Artifact links")
		}
		old, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(old.Tasks, current.Tasks) || !reflect.DeepEqual(old.Artifacts, current.Artifacts) || len(old.Artifacts) != 1 || old.Artifacts[0].URI != oldArtifact {
			t.Fatal("start-over changed source execution history")
		}
		if persisted.WorkItem.StartOverContext != fresh.StartOverContext || len(fresh.StartOverContext) > domain.MaxHistoryTextBytes {
			t.Fatal("start-over context changed or exceeded byte budget")
		}
	})
}

func TestWorkflowStartOverKeepsHistoryOnEachSource(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Copies", Goal: "Retain earlier issue"})
		if err != nil {
			t.Fatal(err)
		}
		const marker = "https://github.com/ScienJus/kairos/issues/37"
		for round := 0; round < 3; round++ {
			current, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			task := current.Tasks[0]
			c, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			reason := "First failure: reuse existing issue " + marker
			if round > 0 {
				reason = "Later configuration failure"
			}
			if round == 2 {
				reason += strings.Repeat("界", (domain.MaxHistoryTextBytes-len(reason))/3)
			}
			if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: reason}); err != nil {
				t.Fatal(err)
			}
			// The other branch is still working when Start over must stop the source.
			bc, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: current.Tasks[1].ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := repositoryTestService(t, openPeer(t)).StartOverWorkflow(ctx, application.StartOverWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: current.WorkItem.Version, OperationID: "start-over-" + string(work.ID)})
			if err != nil {
				t.Fatal(err)
			}
			if fresh.StartedOverFromWorkItemID == nil || *fresh.StartedOverFromWorkItemID != work.ID || !strings.Contains(fresh.StartOverContext, strings.Split(reason, "界")[0]) || len(fresh.StartOverContext) > domain.MaxHistoryTextBytes {
				t.Fatalf("round %d lost current failure/source", round)
			}
			if round > 0 && strings.Contains(fresh.StartOverContext, marker) {
				t.Fatal("start-over copied an earlier WorkItem failure")
			}
			old, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			if old.WorkItem.Status != domain.WorkItemStatusFailed || len(old.ActiveClaims) != 0 {
				t.Fatal("Start over left original running")
			}
			if old.WorkItem.StartOverContext != current.WorkItem.StartOverContext {
				t.Fatal("start-over altered the source's own failure summary")
			}
			for _, claim := range old.Claims {
				if claim.ID == bc.ID && claim.EndReason != domain.ClaimEndRevoked {
					t.Fatal("old worker not revoked")
				}
			}
			if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: bc.TaskID, ClaimID: bc.ID, Identity: actor, Result: "late"}); !errors.Is(err, application.ErrConflict) {
				t.Fatalf("old Claim not fenced: %v", err)
			}
			work = fresh
		}
	})
}

func TestWorkflowContinuePreservesLimitBlockedRetryGuidance(t *testing.T) {
	fullPrompt := strings.Repeat("界", domain.MaxHistoryTextBytes/3) + "xx"
	for _, scenario := range []struct {
		name, inherited, prompt, instructions, want string
	}{
		{"first-retry", "", fullPrompt, "", fullPrompt},
		{"latest-over-inherited", "old guidance", "latest guidance", "", "latest guidance"},
		{"human-over-latest", "old guidance", "latest guidance", "human guidance", "human guidance"},
		{"empty-prompt-inherits", "old guidance", "", "", "old guidance"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				def := attemptDefinition()
				def.Graph.MaxTaskInstancesPerNode = 1
				if scenario.inherited != "" {
					def.Graph.MaxTaskInstancesPerNode = 2
				}
				if err := def.Validate(); err != nil {
					t.Fatal(err)
				}
				if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
					t.Fatal(err)
				}
				work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: scenario.name, Goal: "Preserve blocked retry guidance"})
				if err != nil {
					t.Fatal(err)
				}
				read := func() application.WorkItemExecutionContext {
					t.Helper()
					value, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					return value
				}
				retry := func(prompt string) domain.TaskID {
					t.Helper()
					for _, task := range read().Tasks {
						if *task.WorkflowTaskID != "a" || task.Status != domain.TaskStatusPending {
							continue
						}
						claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: "Permission denied", RetryPrompt: prompt}); err != nil {
							t.Fatal(err)
						}
						return task.ID
					}
					t.Fatal("missing pending a")
					return ""
				}
				if scenario.inherited != "" {
					retry(scenario.inherited)
				}
				sourceID := retry(scenario.prompt)
				blocked := read()
				failure := blocked.WorkItem.Failure
				if blocked.WorkItem.Status != domain.WorkItemStatusFailed || failure == nil || failure.Kind != domain.FailureWorkflowTaskInstanceLimit || failure.TaskInstances != def.Graph.MaxTaskInstancesPerNode || failure.Limit != def.Graph.MaxTaskInstancesPerNode {
					t.Fatalf("expected committed node limit failure, got %+v", failure)
				}
				for _, task := range blocked.Tasks {
					if task.ID == sourceID && (len(task.Failures) != 1 || task.Failures[0].RetryPrompt != scenario.prompt) {
						t.Fatal("blocked retry did not persist its prompt")
					}
				}
				peer := repositoryTestService(t, openPeer(t))
				if _, err := peer.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: blocked.WorkItem.Version, MaxTaskInstancesPerNode: def.Graph.MaxTaskInstancesPerNode + 1, Instructions: scenario.instructions}); err != nil {
					t.Fatal(err)
				}
				after := read()
				if len(after.Tasks) != len(blocked.Tasks)+1 {
					t.Fatal("continue must create exactly one replacement")
				}
				for _, task := range after.Tasks {
					if task.RetryOfTaskID == nil || *task.RetryOfTaskID != sourceID {
						continue
					}
					execution, err := peer.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: task.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					if execution.Task.RetryInstructions != scenario.want {
						t.Fatalf("replacement guidance differs: got %d bytes, want %d bytes", len(execution.Task.RetryInstructions), len(scenario.want))
					}
					return
				}
				t.Fatal("missing replacement")
			})
		})
	}
}

func TestWorkflowRecoveryPreservesReviewAndRetryGuidance(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		def.Graph.Tasks[0].ReviewPolicy = domain.ReviewRequired
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Review recovery", Goal: "Keep corrections"})
		if err != nil {
			t.Fatal(err)
		}
		read := func() application.WorkItemExecutionContext {
			t.Helper()
			value, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		claim := func(task domain.Task) domain.Claim {
			t.Helper()
			value, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		replacement := func(previous domain.Task) domain.Task {
			t.Helper()
			for _, task := range read().Tasks {
				if task.RetryOfTaskID != nil && *task.RetryOfTaskID == previous.ID {
					return task
				}
			}
			t.Fatal("missing replacement")
			return domain.Task{}
		}
		task := read().Tasks[0]
		held := claim(task)
		if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Result: "Initial implementation", Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "exit:a"}}); err != nil {
			t.Fatal(err)
		}
		task = read().Tasks[0]
		const feedback = "必须处理分页 https://example.com/issues/review-only"
		if _, err := service.DecideReview(ctx, application.DecideReviewCommand{TaskID: task.ID, ReviewID: task.Reviews[0].ID, Identity: actor, Decision: domain.ReviewStatusRejected, Feedback: feedback}); err != nil {
			t.Fatal(err)
		}
		// Both strings are exactly the accepted UTF-8 byte boundary. Prompt remains
		// complete even though the generated history must truncate the error.
		prompt := strings.Repeat("界", domain.MaxHistoryTextBytes/3) + "xx"
		held = claim(task)
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: strings.Repeat("x", domain.MaxHistoryTextBytes), RetryPrompt: prompt}); err != nil {
			t.Fatal(err)
		}
		first := task
		task = replacement(task)
		if task.RetryInstructions != prompt || !strings.Contains(task.RetryContext, feedback) {
			t.Fatal("automatic retry lost guidance")
		}
		held = claim(task)
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: "temporary error"}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: read().WorkItem.Version}); err != nil {
			t.Fatal(err)
		}
		task = replacement(task)
		execution, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: task.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if execution.Task.RetryInstructions != prompt || !strings.Contains(execution.Task.RetryContext, feedback) {
			t.Fatal("continue lost inherited guidance")
		}
		if !strings.Contains(execution.Task.RetryContext, "temporary error") {
			t.Fatal("inherited guidance displaced the latest failure reason")
		}
		held = claim(task)
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: "later error"}); err != nil {
			t.Fatal(err)
		}
		fresh, err := service.StartOverWorkflow(ctx, application.StartOverWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: read().WorkItem.Version, OperationID: "start-over-review"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fresh.StartOverContext, feedback) || !strings.Contains(fresh.StartOverContext, "later error") {
			t.Fatal("start-over must copy current failure without old review feedback")
		}
		for _, old := range read().Tasks {
			if old.ID == first.ID && old.Reviews[0].Feedback != feedback {
				t.Fatal("source review changed")
			}
		}
	})
}

func TestWorkflowContinueCountsSameNodeReplacements(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Batch retry", Goal: "Count every replacement"})
		if err != nil {
			t.Fatal(err)
		}
		read := func() application.WorkItemExecutionContext {
			t.Helper()
			value, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		first := read().Tasks[0]
		// Seed another valid correlation for the same node. Sparse positions ensure
		// the batch starts after the maximum position, not after the Task count.
		second := first
		second.ID = domain.TaskID(string(first.ID) + "-parallel")
		second.Position = 20
		if err := repo.Update(ctx, func(store application.WriteStore) error {
			activation, err := store.GetWorkflowTaskActivation(*first.WorkflowActivationID)
			if err != nil {
				return err
			}
			activation.ID = domain.WorkflowTaskActivationID(string(activation.ID) + "-parallel")
			activation.CorrelationID = domain.WorkflowCorrelationID(string(activation.CorrelationID) + "-parallel")
			second.WorkflowActivationID = &activation.ID
			if err := activation.Validate(); err != nil {
				return err
			}
			if err := second.Validate(domain.CoordinationModeWorkflow); err != nil {
				return err
			}
			if err := store.CreateWorkflowTaskActivation(activation); err != nil {
				return err
			}
			return store.CreateTask(second)
		}); err != nil {
			t.Fatal(err)
		}
		for _, task := range []domain.Task{first, second} {
			claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureAwaitHuman, Reason: "retry required"}); err != nil {
				t.Fatal(err)
			}
		}
		before := read()
		if len(before.RecoveryTaskIDs) != 2 {
			t.Fatalf("expected two replacements: %v", before.RecoveryTaskIDs)
		}
		counted := &workflowRecoveryCountingRepository{Repository: openPeer(t), reads: make(map[string]int)}
		peer := repositoryTestService(t, counted)
		command := application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: before.WorkItem.Version, MaxTaskInstancesPerNode: 3}
		if _, err := peer.ContinueWorkflow(ctx, command); !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), `node "a" has 3 task instances at limit 3`) {
			t.Fatalf("expected second replacement to hit the per-node guard: %v", err)
		}
		if !reflect.DeepEqual(before, read()) {
			t.Fatal("insufficient batch capacity committed a partial retry")
		}
		counted.reads = make(map[string]int)
		command.MaxTaskInstancesPerNode = 4
		if _, err := peer.ContinueWorkflow(ctx, command); err != nil {
			t.Fatal(err)
		}
		// Counts include the final completion/progress reads, but do not scale
		// with the number of replacements. The retry loop reads no full history.
		wantReads := map[string]int{"tasks": 2, "activations": 2, "definition": 1, "relations": 1}
		if !reflect.DeepEqual(counted.reads, wantReads) {
			t.Fatalf("batch history reads: %v, want %v", counted.reads, wantReads)
		}
		after := read()
		positions := make(map[domain.TaskID]int64)
		for _, task := range after.Tasks {
			if task.RetryOfTaskID != nil {
				positions[*task.RetryOfTaskID] = task.Position
				if task.Status != domain.TaskStatusPending || task.WorkflowTaskID == nil || *task.WorkflowTaskID != "a" {
					t.Fatalf("invalid replacement: %+v", task)
				}
			}
		}
		if len(after.Tasks) != 5 || len(positions) != 2 || positions[first.ID] != 21 || positions[second.ID] != 22 {
			t.Fatalf("incorrect batch positions/count: %v (%d Tasks)", positions, len(after.Tasks))
		}
		if err := repo.View(ctx, func(store application.ReadStore) error {
			activations, err := store.ListWorkflowTaskActivations(work.ID)
			if err != nil {
				return err
			}
			count := 0
			for _, activation := range activations {
				if activation.WorkflowTaskID == "a" && activation.Status == domain.WorkflowActivationResolved {
					count++
				}
			}
			if count != 4 {
				t.Fatalf("persisted task instance count = %d, want 4", count)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// Count application-level snapshot reads while retaining real SQL transactions.
type workflowRecoveryCountingRepository struct {
	application.Repository
	reads map[string]int
}

func (r *workflowRecoveryCountingRepository) Update(ctx context.Context, fn func(application.WriteStore) error) error {
	return r.Repository.Update(ctx, func(store application.WriteStore) error {
		return fn(&workflowRecoveryCountingStore{WriteStore: store, reads: r.reads})
	})
}

type workflowRecoveryCountingStore struct {
	application.WriteStore
	reads map[string]int
}

func (s *workflowRecoveryCountingStore) ListTasks(id domain.WorkItemID) ([]domain.Task, error) {
	s.reads["tasks"]++
	return s.WriteStore.ListTasks(id)
}

func (s *workflowRecoveryCountingStore) ListWorkflowTaskActivations(id domain.WorkItemID) ([]domain.WorkflowTaskActivation, error) {
	s.reads["activations"]++
	return s.WriteStore.ListWorkflowTaskActivations(id)
}

func (s *workflowRecoveryCountingStore) GetWorkflowDefinition(id domain.DefinitionID, version int64) (domain.WorkflowDefinition, error) {
	s.reads["definition"]++
	return s.WriteStore.GetWorkflowDefinition(id, version)
}

func (s *workflowRecoveryCountingStore) ListTaskRelations(id domain.WorkItemID) ([]domain.TaskRelation, error) {
	s.reads["relations"]++
	return s.WriteStore.ListTaskRelations(id)
}

func assertStoredRecoveryEvent(t *testing.T, repo *SQLRepository, workID domain.WorkItemID, want domain.WorkItemEventType) {
	t.Helper()
	rows, err := repo.db.QueryContext(context.Background(), rebind(repo.dialect, "SELECT payload FROM work_item_events WHERE work_item_id = ? ORDER BY sequence"), workID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		event, err := decodeJSON[domain.WorkItemEvent](payload)
		if err != nil {
			t.Fatal(err)
		}
		if !event.Type.Valid() {
			t.Fatalf("invalid persisted event: %s", event.Type)
		}
		if event.Type == want {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("missing persisted event %s", want)
	}
}
