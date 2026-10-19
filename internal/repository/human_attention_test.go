package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestHumanAttentionIncludesOwnActiveClaims(t *testing.T) {
	for _, mode := range []domain.CoordinationMode{domain.CoordinationModeBlackboard, domain.CoordinationModeWorkflow} {
		t.Run(string(mode), func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				owner := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "owner"}}
				other := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "other"}}
				agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "owner"}, Role: "developer"}
				board := repositoryBlackboardDefinition()
				if err := repo.CreateBlackboardDefinition(ctx, board); err != nil {
					t.Fatal(err)
				}
				create := func(name string, executor domain.ExecutorRequirement) (domain.WorkItem, domain.Task) {
					t.Helper()
					binding := board.Binding()
					if mode == domain.CoordinationModeWorkflow {
						definition := domain.WorkflowDefinition{
							DefinitionMetadata: domain.DefinitionMetadata{ID: domain.DefinitionID(name), Version: 1, Name: name, CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime},
							Graph:              domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"work"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "work", Title: name, Executor: executor, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewExecutorDecides}}},
						}
						if err := definition.Validate(); err != nil {
							t.Fatal(err)
						}
						if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
							t.Fatal(err)
						}
						binding = definition.Binding()
					}
					work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: binding, Identity: owner, Title: name, Goal: "Check ownership in the human attention feed"})
					if err != nil {
						t.Fatal(err)
					}
					if mode == domain.CoordinationModeWorkflow {
						return work, repositoryTaskByDefinition(t, repo, work.ID, "work")
					}
					task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: work.ID, Identity: owner, Title: name, Executor: executor})
					if err != nil {
						t.Fatal(err)
					}
					return work, task
				}
				claim := func(task domain.Task, actor application.Identity) domain.Claim {
					t.Helper()
					value, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					return value
				}
				_, pending := create("pending-human", domain.ExecutorHuman)
				create("pending-either", domain.ExecutorEither)
				_, ownHuman := create("own-human", domain.ExecutorHuman)
				humanClaim := claim(ownHuman, owner)
				_, ownEither := create("own-either", domain.ExecutorEither)
				eitherClaim := claim(ownEither, owner)
				_, otherHuman := create("other-human", domain.ExecutorHuman)
				claim(otherHuman, other)
				_, sameIDAgent := create("same-id-agent", domain.ExecutorEither)
				claim(sameIDAgent, agent)
				_, reassigned := create("reassigned-either", domain.ExecutorEither)
				oldClaim := claim(reassigned, owner)
				if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: reassigned.ID, ClaimID: oldClaim.ID, Identity: owner}); err != nil {
					t.Fatal(err)
				}
				claim(reassigned, other)
				cancelledWork, cancelledTask := create("cancelled", domain.ExecutorHuman)
				claim(cancelledTask, owner)
				if _, err := service.CancelWorkItem(ctx, application.CancelWorkItemCommand{WorkItemID: cancelledWork.ID, Identity: owner, Reason: "No longer needed"}); err != nil {
					t.Fatal(err)
				}
				_, completed := create("completed", domain.ExecutorHuman)
				completedClaim := claim(completed, owner)
				if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: completed.ID, ClaimID: completedClaim.ID, Identity: owner, Result: "Done"}); err != nil {
					t.Fatal(err)
				}
				_, review := create("review", domain.ExecutorHuman)
				reviewClaim := claim(review, owner)
				if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: review.ID, ClaimID: reviewClaim.ID, Identity: owner, Result: "Review this", RequestReview: true}); err != nil {
					t.Fatal(err)
				}

				// Ownership must be applied before LIMIT/cursor selection. A page of
				// somebody else's claims cannot hide later eligible entries.
				list := func(actor application.Identity) []domain.TaskID {
					t.Helper()
					ids := []domain.TaskID{}
					var cursor *application.HumanAttentionCursor
					for i := 0; i < 20; i++ {
						page, err := service.ListHumanAttention(ctx, actor, application.PageRequest[application.HumanAttentionCursor]{Limit: 1, After: cursor})
						if err != nil {
							t.Fatal(err)
						}
						for _, item := range page.Items {
							if item.Task == nil {
								t.Fatal("unexpected work item entry")
							}
							if slices.Contains(ids, item.Task.ID) {
								t.Fatal("duplicate page entry")
							}
							if item.Task.ID != review.ID && item.Kind != application.HumanAttentionTask {
								t.Fatalf("working Task kind = %s", item.Kind)
							}
							ids = append(ids, item.Task.ID)
						}
						if !page.HasMore {
							return ids
						}
						if len(page.Items) != 1 {
							t.Fatal("ownership filtering produced an empty intermediate page")
						}
						next := page.Items[0].Cursor()
						cursor = &next
					}
					t.Fatal("pagination did not terminate")
					return nil
				}
				assertIDs := func(actor application.Identity, want ...domain.TaskID) {
					t.Helper()
					got := list(actor)
					if len(got) == 0 || got[0] != review.ID {
						t.Fatalf("Reviews must retain priority: %v", got)
					}
					slices.Sort(got)
					slices.Sort(want)
					if !slices.Equal(got, want) {
						t.Fatalf("attention for %s/%s = %v, want %v", actor.Actor.Kind, actor.Actor.ID, got, want)
					}
				}
				assertIDs(owner, pending.ID, ownHuman.ID, ownEither.ID, review.ID)
				assertIDs(other, pending.ID, otherHuman.ID, reassigned.ID, review.ID)
				assertIDs(agent, pending.ID, review.ID)
				if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: ownHuman.ID, ClaimID: humanClaim.ID, Identity: owner}); err != nil {
					t.Fatal(err)
				}
				if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: ownEither.ID, ClaimID: eitherClaim.ID, Identity: owner}); err != nil {
					t.Fatal(err)
				}
				assertIDs(owner, pending.ID, ownHuman.ID, review.ID)
			})
		})
	}
}

func TestHumanAttentionWorkflowRecovery(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		for i := range def.Graph.Tasks {
			def.Graph.Tasks[i].Executor = domain.ExecutorEither
		}
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		read := func(id domain.WorkItemID) application.WorkItemExecutionContext {
			t.Helper()
			value, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: id, Identity: human})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		create := func(title string, action domain.TaskFailureAction, count int) domain.WorkItem {
			t.Helper()
			work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: human, Title: title, Goal: "Show one recovery action per Workflow"})
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range read(work.ID).Tasks[:count] {
				claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: human})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: human, Action: action, Reason: "缺少权限"}); err != nil {
					t.Fatal(err)
				}
			}
			return read(work.ID).WorkItem
		}
		open := create("two failed branches", domain.TaskFailureAwaitHuman, 2)
		failed := create("whole Workflow failed", domain.TaskFailureFailWorkItem, 1)
		startOver := create("start over", domain.TaskFailureAwaitHuman, 1)
		cancelled := create("cancelled", domain.TaskFailureAwaitHuman, 1)
		if _, err := service.CancelWorkItem(ctx, application.CancelWorkItemCommand{WorkItemID: cancelled.ID, Identity: human, Reason: "No longer needed"}); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateBlackboardDefinition(ctx, repositoryBlackboardDefinition()); err != nil {
			t.Fatal(err)
		}
		board, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: repositoryBlackboardDefinition().Binding(), Identity: human, Title: "Blackboard", Goal: "Not recoverable as a Workflow"})
		if err != nil {
			t.Fatal(err)
		}
		task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: board.ID, Identity: human, Title: "Fail", Executor: domain.ExecutorHuman})
		if err != nil {
			t.Fatal(err)
		}
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: human})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: human, Action: domain.TaskFailureFailWorkItem, Reason: "Stop"}); err != nil {
			t.Fatal(err)
		}

		// Every timestamp ties. Deduplication and eligibility must happen before
		// LIMIT so each page advances without duplicates or empty middle pages.
		var cursor *application.HumanAttentionCursor
		ids := []domain.WorkItemID{}
		for i := 0; i < 4; i++ {
			page, err := service.ListHumanAttention(ctx, human, application.PageRequest[application.HumanAttentionCursor]{Limit: 1, After: cursor})
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("recovery page: %+v, %v", page, err)
			}
			item := page.Items[0]
			if item.Kind != application.HumanAttentionRecovery || item.Task == nil || item.Task.Failures[0].Reason != "缺少权限" || slices.Contains(ids, item.WorkItem.ID) {
				t.Fatalf("wrong or duplicate recovery entry: %+v", item)
			}
			ids = append(ids, item.WorkItem.ID)
			if !page.HasMore {
				break
			}
			next := item.Cursor()
			cursor = &next
		}
		want := []domain.WorkItemID{open.ID, failed.ID, startOver.ID}
		slices.Sort(ids)
		slices.Sort(want)
		if !slices.Equal(ids, want) {
			t.Fatalf("recovery WorkItems = %v, want %v", ids, want)
		}
		agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "developer"}
		page, err := service.ListHumanAttention(ctx, agent, application.PageRequest[application.HumanAttentionCursor]{Limit: 100})
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("Agent must not receive recovery actions: %+v, %v", page, err)
		}
		for _, work := range []domain.WorkItem{open, failed} {
			if _, err := service.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: human, Version: work.Version}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.StartOverWorkflow(ctx, application.StartOverWorkflowCommand{WorkItemID: startOver.ID, Identity: human, Version: startOver.Version, OperationID: "replace-attention-workflow"}); err != nil {
			t.Fatal(err)
		}
		page, err = service.ListHumanAttention(ctx, human, application.PageRequest[application.HumanAttentionCursor]{Limit: 100})
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("continued, retried or replaced failures remain in attention: %+v, %v", page, err)
		}
		if read(startOver.ID).WorkItem.Status != domain.WorkItemStatusFailed {
			t.Fatal("removing a start-over source must preserve its failed history")
		}
	})
}
