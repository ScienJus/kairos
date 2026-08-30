package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestCoordinationClaimLifecycleFencesBlackboardDecisions(t *testing.T) {
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	clock := &leaseTestClock{now: applicationTestTime}
	service, err := NewService(repository, clock, &testIDs{})
	if err != nil {
		t.Fatal(err)
	}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	other := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "other"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   agent, Title: "Coordinate lifecycle", Goal: "Prevent duplicate decisions", AcceptanceMode: domain.WorkItemAcceptanceAgent,
	})
	if err != nil {
		t.Fatal(err)
	}

	claim, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{
		WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: agent, LeaseSeconds: 300,
	})
	if err != nil {
		t.Fatalf("claim empty blackboard: %v", err)
	}
	if claim.LeaseSeconds != 300 || !claim.LeaseUntil.Equal(clock.now.Add(5*time.Minute)) {
		t.Fatalf("initial coordination lease = %#v", claim)
	}
	candidates, err := service.FindWork(context.Background(), FindWorkQuery{Identity: other})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidate remained visible after claim: %#v, err=%v", candidates, err)
	}
	if _, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: other}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second coordination claim error = %v, want conflict", err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: other, Title: "Duplicate plan", Executor: domain.ExecutorAgent,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unclaimed planning mutation error = %v, want conflict", err)
	}

	clock.now = clock.now.Add(time.Minute)
	claim, err = service.HeartbeatCoordinationClaim(context.Background(), HeartbeatCoordinationClaimCommand{
		WorkItemID: workItem.ID, ClaimID: claim.ID, Identity: agent, LeaseSeconds: 30,
	})
	if err != nil || claim.LeaseSeconds != 30 || !claim.LeaseUntil.Equal(clock.now.Add(30*time.Second)) {
		t.Fatalf("coordination heartbeat = %#v, err=%v", claim, err)
	}
	if err := service.ReleaseCoordinationClaim(context.Background(), ReleaseCoordinationClaimCommand{WorkItemID: workItem.ID, ClaimID: claim.ID, Identity: agent}); err != nil {
		t.Fatalf("release coordination claim: %v", err)
	}
	candidates, err = service.FindWork(context.Background(), FindWorkQuery{Identity: other})
	if err != nil || len(candidates) != 1 || candidates[0].Kind != WorkCandidateEmptyBlackboard {
		t.Fatalf("released candidate = %#v, err=%v", candidates, err)
	}

	expiring, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{
		WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: other, LeaseSeconds: 15,
	})
	if err != nil {
		t.Fatalf("claim expiring candidate: %v", err)
	}
	clock.now = expiring.LeaseUntil
	if err := service.ReapExpiredClaims(context.Background()); err != nil {
		t.Fatalf("reap coordination claim: %v", err)
	}
	if repository.coordinationClaims[expiring.ID].EndReason != domain.CoordinationClaimEndExpired {
		t.Fatalf("expired coordination claim = %#v", repository.coordinationClaims[expiring.ID])
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, CoordinationClaimID: expiring.ID, Identity: other, Title: "Late plan", Executor: domain.ExecutorAgent,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired claim mutation error = %v, want conflict", err)
	}

	planning, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: agent})
	if err != nil {
		t.Fatalf("reclaim planning: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, CoordinationClaimID: planning.ID, Identity: agent, Title: "Implement", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("resolve planning claim: %v", err)
	}
	if repository.coordinationClaims[planning.ID].EndReason != domain.CoordinationClaimEndTaskCreated {
		t.Fatalf("planning end reason = %#v", repository.coordinationClaims[planning.ID])
	}
	taskClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{TaskID: task.ID, ClaimID: taskClaim.ID, Identity: agent, Result: "done"}); err != nil {
		t.Fatal(err)
	}

	completion, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{WorkItemID: workItem.ID, Kind: WorkCandidateBlackboardCompletion, Identity: agent})
	if err != nil {
		t.Fatalf("claim completion: %v", err)
	}
	awaiting, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, CoordinationClaimID: completion.ID, Identity: agent, Result: "complete",
	})
	if err != nil || awaiting.Status != domain.WorkItemStatusAwaitingAgentAcceptance {
		t.Fatalf("submit completion = %#v, err=%v", awaiting, err)
	}
	if repository.coordinationClaims[completion.ID].EndReason != domain.CoordinationClaimEndCompletionSubmitted {
		t.Fatalf("completion end reason = %#v", repository.coordinationClaims[completion.ID])
	}

	acceptance, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{WorkItemID: workItem.ID, Kind: WorkCandidateWorkItemAcceptance, Identity: other})
	if err != nil {
		t.Fatalf("claim acceptance: %v", err)
	}
	completed, err := service.AcceptBlackboardCompletion(context.Background(), AcceptBlackboardCompletionCommand{
		WorkItemID: workItem.ID, CoordinationClaimID: acceptance.ID, Identity: other,
	})
	if err != nil || completed.Status != domain.WorkItemStatusCompleted {
		t.Fatalf("accept completion = %#v, err=%v", completed, err)
	}
	if repository.coordinationClaims[acceptance.ID].EndReason != domain.CoordinationClaimEndCompletionAccepted {
		t.Fatalf("acceptance end reason = %#v", repository.coordinationClaims[acceptance.ID])
	}
	contextView, err := service.GetWorkItemExecutionContext(context.Background(), GetWorkItemExecutionContextQuery{WorkItemID: workItem.ID, Identity: agent})
	if err != nil {
		t.Fatal(err)
	}
	if len(contextView.CoordinationClaims) != 5 || contextView.ActiveCoordinationClaim != nil {
		t.Fatalf("coordination context = %#v", contextView)
	}
}

func TestHumanDecisionRevokesActiveCoordinationClaim(t *testing.T) {
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   human, Title: "Human override", Goal: "Keep management authoritative",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: agent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: human, Title: "Managed task", Executor: domain.ExecutorHuman}); err != nil {
		t.Fatalf("human planning override: %v", err)
	}
	if repository.coordinationClaims[claim.ID].EndReason != domain.CoordinationClaimEndRevoked {
		t.Fatalf("revoked claim = %#v", repository.coordinationClaims[claim.ID])
	}
}

func TestCoordinationClaimHistoryLimitFailsWorkItem(t *testing.T) {
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   agent, Title: "Bound coordination history", Goal: "Stop abandoned decision churn",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimedAt := applicationTestTime.Add(-time.Hour)
	endedAt := claimedAt.Add(time.Second)
	for index := 0; index < MaxCoordinationClaimsPerWorkItem-1; index++ {
		id := domain.CoordinationClaimID(fmt.Sprintf("old-coordination-%03d", index))
		repository.coordinationClaims[id] = domain.CoordinationClaim{
			ID: id, WorkItemID: workItem.ID, Kind: domain.CoordinationClaimEmptyBlackboard,
			Executor: agent.Actor, ClaimedAt: claimedAt, LastHeartbeatAt: claimedAt,
			LeaseUntil: claimedAt.Add(time.Minute), LeaseSeconds: 60,
			EndedAt: &endedAt, EndReason: domain.CoordinationClaimEndReleased,
		}
	}
	boundary, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{
		WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: agent,
	})
	if err != nil {
		t.Fatalf("claim at boundary: %v", err)
	}
	if err := service.ReleaseCoordinationClaim(context.Background(), ReleaseCoordinationClaimCommand{
		WorkItemID: workItem.ID, ClaimID: boundary.ID, Identity: agent,
	}); err != nil {
		t.Fatalf("release boundary claim: %v", err)
	}
	if _, err := service.ClaimWorkCandidate(context.Background(), ClaimWorkCandidateCommand{
		WorkItemID: workItem.ID, Kind: WorkCandidateEmptyBlackboard, Identity: agent,
	}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "coordination claims") {
		t.Fatalf("claim beyond limit error = %v, want coordination-claim conflict", err)
	}
	if repository.workItems[workItem.ID].Status != domain.WorkItemStatusFailed {
		t.Fatalf("work item status = %q, want failed", repository.workItems[workItem.ID].Status)
	}
	claims, err := repository.ListCoordinationClaims(workItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != MaxCoordinationClaimsPerWorkItem {
		t.Fatalf("coordination claim count = %d, want %d", len(claims), MaxCoordinationClaimsPerWorkItem)
	}
}
