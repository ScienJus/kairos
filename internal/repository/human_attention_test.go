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
