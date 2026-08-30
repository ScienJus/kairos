package domain

import (
	"testing"
	"time"
)

func TestValidateCoordinationClaimHistory(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	claim := CoordinationClaim{
		ID: "coordination-1", WorkItemID: "work-1", Kind: CoordinationClaimEmptyBlackboard,
		Executor: ActorRef{Kind: ActorAgent, ID: "planner"}, ClaimedAt: now, LastHeartbeatAt: now,
		LeaseUntil: now.Add(time.Minute), LeaseSeconds: 60,
	}
	if err := ValidateCoordinationClaimHistory("work-1", []CoordinationClaim{claim}); err != nil {
		t.Fatalf("valid active history: %v", err)
	}
	second := claim
	second.ID = "coordination-2"
	second.ClaimedAt = now.Add(time.Second)
	second.LastHeartbeatAt = second.ClaimedAt
	second.LeaseUntil = second.ClaimedAt.Add(time.Minute)
	if err := ValidateCoordinationClaimHistory("work-1", []CoordinationClaim{claim, second}); err == nil {
		t.Fatal("multiple active coordination claims unexpectedly validated")
	}
	claim.Executor.Kind = ActorHuman
	if err := claim.Validate(); err == nil {
		t.Fatal("human coordination claim unexpectedly validated")
	}
}

func TestCoordinationClaimEndReasonMatchesKind(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	common := []CoordinationClaimEndReason{
		CoordinationClaimEndReleased,
		CoordinationClaimEndExpired,
		CoordinationClaimEndRevoked,
		CoordinationClaimEndWorkItemCancelled,
		CoordinationClaimEndWorkItemFailed,
	}
	allowed := map[CoordinationClaimKind][]CoordinationClaimEndReason{
		CoordinationClaimEmptyBlackboard: append(append([]CoordinationClaimEndReason{}, common...),
			CoordinationClaimEndTaskCreated, CoordinationClaimEndCompletionSubmitted),
		CoordinationClaimBlackboardCompletion: append(append([]CoordinationClaimEndReason{}, common...),
			CoordinationClaimEndTaskCreated, CoordinationClaimEndCompletionSubmitted),
		CoordinationClaimWorkItemAcceptance: append(append([]CoordinationClaimEndReason{}, common...),
			CoordinationClaimEndTaskCreated, CoordinationClaimEndCompletionAccepted),
	}
	allReasons := []CoordinationClaimEndReason{
		CoordinationClaimEndTaskCreated,
		CoordinationClaimEndCompletionSubmitted,
		CoordinationClaimEndCompletionAccepted,
		CoordinationClaimEndReleased,
		CoordinationClaimEndExpired,
		CoordinationClaimEndRevoked,
		CoordinationClaimEndWorkItemCancelled,
		CoordinationClaimEndWorkItemFailed,
	}
	for kind, validReasons := range allowed {
		for _, reason := range allReasons {
			claim := CoordinationClaim{
				ID: "coordination-1", WorkItemID: "work-1", Kind: kind,
				Executor: ActorRef{Kind: ActorAgent, ID: "planner"}, ClaimedAt: now, LastHeartbeatAt: now,
				LeaseUntil: now.Add(time.Minute), LeaseSeconds: 60, EndedAt: &now, EndReason: reason,
			}
			wantValid := false
			for _, validReason := range validReasons {
				wantValid = wantValid || reason == validReason
			}
			if err := claim.Validate(); (err == nil) != wantValid {
				t.Errorf("kind %q reason %q validation error = %v, want valid=%v", kind, reason, err, wantValid)
			}
		}
	}
}
