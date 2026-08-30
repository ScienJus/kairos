package domain

import (
	"strings"
	"time"
)

// CoordinationClaimKind identifies the Blackboard decision reserved by a Claim.
type CoordinationClaimKind string

const (
	CoordinationClaimEmptyBlackboard      CoordinationClaimKind = "empty_blackboard"
	CoordinationClaimBlackboardCompletion CoordinationClaimKind = "blackboard_completion"
	CoordinationClaimWorkItemAcceptance   CoordinationClaimKind = "work_item_acceptance"
)

func (k CoordinationClaimKind) Valid() bool {
	switch k {
	case CoordinationClaimEmptyBlackboard, CoordinationClaimBlackboardCompletion, CoordinationClaimWorkItemAcceptance:
		return true
	default:
		return false
	}
}

// CoordinationClaimEndReason records why WorkItem-level responsibility ended.
type CoordinationClaimEndReason string

const (
	CoordinationClaimEndTaskCreated         CoordinationClaimEndReason = "task_created"
	CoordinationClaimEndCompletionSubmitted CoordinationClaimEndReason = "completion_submitted"
	CoordinationClaimEndCompletionAccepted  CoordinationClaimEndReason = "completion_accepted"
	CoordinationClaimEndReleased            CoordinationClaimEndReason = "released"
	CoordinationClaimEndExpired             CoordinationClaimEndReason = "expired"
	CoordinationClaimEndRevoked             CoordinationClaimEndReason = "revoked"
	CoordinationClaimEndWorkItemCancelled   CoordinationClaimEndReason = "work_item_cancelled"
	CoordinationClaimEndWorkItemFailed      CoordinationClaimEndReason = "work_item_failed"
)

func (r CoordinationClaimEndReason) Valid() bool {
	switch r {
	case CoordinationClaimEndTaskCreated,
		CoordinationClaimEndCompletionSubmitted,
		CoordinationClaimEndCompletionAccepted,
		CoordinationClaimEndReleased,
		CoordinationClaimEndExpired,
		CoordinationClaimEndRevoked,
		CoordinationClaimEndWorkItemCancelled,
		CoordinationClaimEndWorkItemFailed:
		return true
	default:
		return false
	}
}

func (r CoordinationClaimEndReason) validFor(kind CoordinationClaimKind) bool {
	switch r {
	case CoordinationClaimEndReleased,
		CoordinationClaimEndExpired,
		CoordinationClaimEndRevoked,
		CoordinationClaimEndWorkItemCancelled,
		CoordinationClaimEndWorkItemFailed:
		return true
	case CoordinationClaimEndTaskCreated:
		return kind == CoordinationClaimEmptyBlackboard ||
			kind == CoordinationClaimBlackboardCompletion ||
			kind == CoordinationClaimWorkItemAcceptance
	case CoordinationClaimEndCompletionSubmitted:
		return kind == CoordinationClaimEmptyBlackboard || kind == CoordinationClaimBlackboardCompletion
	case CoordinationClaimEndCompletionAccepted:
		return kind == CoordinationClaimWorkItemAcceptance
	default:
		return false
	}
}

// CoordinationClaim reserves one Blackboard lifecycle candidate for an Agent.
type CoordinationClaim struct {
	ID         CoordinationClaimID   `json:"id"`
	WorkItemID WorkItemID            `json:"work_item_id"`
	Kind       CoordinationClaimKind `json:"kind"`
	Executor   ActorRef              `json:"executor"`

	ClaimedAt       time.Time                  `json:"claimed_at"`
	LastHeartbeatAt time.Time                  `json:"last_heartbeat_at"`
	LeaseUntil      time.Time                  `json:"lease_until"`
	LeaseSeconds    int64                      `json:"lease_seconds"`
	EndedAt         *time.Time                 `json:"ended_at"`
	EndReason       CoordinationClaimEndReason `json:"end_reason"`
}

func (c CoordinationClaim) Active() bool {
	return c.EndedAt == nil
}

func (c CoordinationClaim) Validate() error {
	if strings.TrimSpace(string(c.ID)) == "" {
		return invalid("coordination_claim.id", "is required")
	}
	if strings.TrimSpace(string(c.WorkItemID)) == "" {
		return invalid("coordination_claim.work_item_id", "is required")
	}
	if !c.Kind.Valid() {
		return invalid("coordination_claim.kind", "unsupported value %q", c.Kind)
	}
	if err := c.Executor.Validate(); err != nil {
		return err
	}
	if c.Executor.Kind != ActorAgent {
		return invalid("coordination_claim.executor.kind", "must be agent")
	}
	if c.ClaimedAt.IsZero() || c.LastHeartbeatAt.IsZero() || c.LeaseUntil.IsZero() {
		return invalid("coordination_claim.lease", "claimed_at, last_heartbeat_at and lease_until are required")
	}
	if c.LastHeartbeatAt.Before(c.ClaimedAt) || c.LeaseUntil.Before(c.LastHeartbeatAt) {
		return invalid("coordination_claim.lease", "timestamps are out of order")
	}
	if c.LeaseSeconds <= 0 {
		return invalid("coordination_claim.lease_seconds", "must be positive")
	}
	if c.EndedAt == nil {
		if c.EndReason != "" {
			return invalid("coordination_claim.end_reason", "must be empty while active")
		}
		return nil
	}
	if c.EndedAt.IsZero() || c.EndedAt.Before(c.ClaimedAt) {
		return invalid("coordination_claim.ended_at", "must not be zero or before claimed_at")
	}
	if !c.EndReason.Valid() {
		return invalid("coordination_claim.end_reason", "a valid reason is required after the claim ends")
	}
	if !c.EndReason.validFor(c.Kind) {
		return invalid("coordination_claim.end_reason", "%q is not valid for kind %q", c.EndReason, c.Kind)
	}
	return nil
}

// ValidateCoordinationClaimHistory verifies one WorkItem's complete history.
func ValidateCoordinationClaimHistory(workItemID WorkItemID, claims []CoordinationClaim) error {
	seen := make(map[CoordinationClaimID]struct{}, len(claims))
	var previous time.Time
	active := 0
	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return err
		}
		if claim.WorkItemID != workItemID {
			return invalid("coordination_claims", "claim %q belongs to another work item", claim.ID)
		}
		if _, exists := seen[claim.ID]; exists {
			return invalid("coordination_claims", "contains duplicate claim %q", claim.ID)
		}
		seen[claim.ID] = struct{}{}
		if !previous.IsZero() && claim.ClaimedAt.Before(previous) {
			return invalid("coordination_claims", "must be ordered by claimed_at")
		}
		previous = claim.ClaimedAt
		if claim.Active() {
			active++
		}
	}
	if active > 1 {
		return invalid("coordination_claims", "contains more than one active claim")
	}
	return nil
}
