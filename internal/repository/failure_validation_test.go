package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestBlackboardStopValidationPrecedesFailureLimit(t *testing.T) {
	for _, count := range []int{0, application.MaxFailuresPerTask - 1, application.MaxFailuresPerTask} {
		t.Run(fmt.Sprintf("failures-%d", count), func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				definition := repositoryBlackboardDefinition()
				if err := repo.CreateBlackboardDefinition(ctx, definition); err != nil {
					t.Fatal(err)
				}
				work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: actor, Title: "Validate failure action", Goal: "Reject invalid actions without mutation"})
				if err != nil {
					t.Fatal(err)
				}
				task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: work.ID, Identity: actor, Title: "Retry", Executor: domain.ExecutorHuman})
				if err != nil {
					t.Fatal(err)
				}
				var claim domain.Claim
				for i := 0; i <= count; i++ {
					service, err = application.NewService(repo, repositoryTestClockAt{now: repositoryTestTime.Add(time.Duration(i+1) * time.Second)}, &repositoryTestIDs{})
					if err != nil {
						t.Fatal(err)
					}
					claim, err = service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					if i < count {
						if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureReopen, Reason: "retry"}); err != nil {
							t.Fatal(err)
						}
					}
				}
				read := func() (application.WorkItemExecutionContext, int64) {
					t.Helper()
					value, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					var sequence int64
					if err := repo.View(ctx, func(store application.ReadStore) error {
						var err error
						sequence, err = store.LastWorkItemEventSequence(work.ID)
						return err
					}); err != nil {
						t.Fatal(err)
					}
					return value, sequence
				}
				before, sequence := read()
				if len(before.Tasks[0].Failures) != count || len(before.Claims) >= application.MaxClaimsPerTask {
					t.Fatal("fixture does not isolate failure history capacity")
				}
				if err := domain.ValidateTaskContext(domain.CoordinationModeBlackboard, before.Tasks[0], before.Claims); err != nil {
					t.Fatal(err)
				}
				peer, err := application.NewService(openPeer(t), repositoryTestClockAt{now: repositoryTestTime.Add(time.Duration(count+2) * time.Second)}, &repositoryTestIDs{})
				if err != nil {
					t.Fatal(err)
				}
				command := application.FailTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Action: domain.TaskFailureStop, Reason: "pause"}
				if _, err := peer.FailTask(ctx, command); !errors.Is(err, application.ErrInvalidCommand) || !strings.Contains(err.Error(), "fail_task is only supported for Workflow") {
					t.Fatalf("want mode validation error, got %v", err)
				}
				after, afterSequence := read()
				if !reflect.DeepEqual(before, after) || sequence != afterSequence {
					t.Fatal("invalid action changed persisted WorkItem, Task, Claims, or events")
				}
				if count == application.MaxFailuresPerTask {
					command.Action = domain.TaskFailureReopen
					if _, err := peer.FailTask(ctx, command); !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "failure records") {
						t.Fatalf("valid reopen must still enforce failure capacity, got %v", err)
					}
					terminal, _ := read()
					if terminal.WorkItem.Status != domain.WorkItemStatusFailed || terminal.WorkItem.Failure == nil || !strings.Contains(terminal.WorkItem.Failure.Message, "failure records") || len(terminal.ActiveClaims) != 0 {
						t.Fatal("valid capacity failure did not commit terminal state")
					}
				}
			})
		})
	}
}
