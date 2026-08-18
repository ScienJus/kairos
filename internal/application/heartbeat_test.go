package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type leaseTestClock struct{ now time.Time }

func (c *leaseTestClock) Now() time.Time { return c.now }

func TestNormalizeClaimLease(t *testing.T) {
	tests := []struct {
		name      string
		requested int64
		fallback  time.Duration
		want      time.Duration
	}{
		{"default", 0, DefaultClaimLease, DefaultClaimLease},
		{"minimum", 1, DefaultClaimLease, MinClaimLease},
		{"requested", 300, DefaultClaimLease, 5 * time.Minute},
		{"maximum", 86400, DefaultClaimLease, MaxClaimLease},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeClaimLease(tt.requested, tt.fallback); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAgentClaimLeaseLifecycle(t *testing.T) {
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	clock := &leaseTestClock{now: applicationTestTime}
	service, err := NewService(repository, clock, &testIDs{})
	if err != nil {
		t.Fatal(err)
	}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	other := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "other"}, Role: "generalist"}
	task := createLeaseTestTask(t, service, agent, domain.ExecutorAgent)

	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent, LeaseSeconds: 300})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.LeaseSeconds != 300 || !claim.LeaseUntil.Equal(clock.now.Add(5*time.Minute)) {
		t.Fatalf("claim lease: %#v", claim)
	}

	clock.now = clock.now.Add(time.Minute)
	claim, err = service.HeartbeatClaim(context.Background(), HeartbeatClaimCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent, LeaseSeconds: 30})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if claim.LeaseSeconds != 30 || !claim.LastHeartbeatAt.Equal(clock.now) || !claim.LeaseUntil.Equal(clock.now.Add(30*time.Second)) {
		t.Fatalf("heartbeat lease: %#v", claim)
	}

	clock.now = claim.LeaseUntil
	if err := service.ReapExpiredClaims(context.Background()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	expired := repository.claims[claim.ID]
	if expired.Active() || expired.EndReason != domain.ClaimEndExpired {
		t.Fatalf("expired claim: %#v", expired)
	}
	if got := repository.tasks[task.ID]; got.Status != domain.TaskStatusPending || got.ActiveClaimID != nil {
		t.Fatalf("reaped task: %#v", got)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("late submit: %v", err)
	}

	replacement, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: other})
	if err != nil {
		t.Fatalf("replacement claim: %v", err)
	}
	if replacement.ID == claim.ID {
		t.Fatal("replacement reused expired claim id")
	}
}

func TestHumanClaimDoesNotUseLease(t *testing.T) {
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	clock := &leaseTestClock{now: applicationTestTime}
	service, err := NewService(repository, clock, &testIDs{})
	if err != nil {
		t.Fatal(err)
	}
	planner := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "human"}}
	task := createLeaseTestTask(t, service, planner, domain.ExecutorHuman)
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.LeaseSeconds != 0 || !claim.LeaseUntil.IsZero() || !claim.LastHeartbeatAt.IsZero() {
		t.Fatalf("human lease: %#v", claim)
	}
	if _, err := service.HeartbeatClaim(context.Background(), HeartbeatClaimCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: human}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("human heartbeat: %v", err)
	}
	clock.now = clock.now.Add(24 * time.Hour)
	if err := service.ReapExpiredClaims(context.Background()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if !repository.claims[claim.ID].Active() {
		t.Fatal("human claim was reaped")
	}
	otherTask := createLeaseTestTask(t, service, planner, domain.ExecutorHuman)
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: otherTask.ID, Identity: human, LeaseSeconds: 60}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("human lease request: %v", err)
	}
}

func createLeaseTestTask(t *testing.T, service *Service, creator Identity, executor domain.ExecutorRequirement) domain.Task {
	t.Helper()
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard}, Identity: creator, Title: "Lease", Goal: "Test lease"})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: creator, Title: "Execute", Executor: executor})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}
