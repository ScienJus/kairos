package domain

import (
	"strings"
	"time"
)

// ActorKind identifies whether work is performed by a human or an agent.
type ActorKind string

const (
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

// Valid reports whether the actor kind is recognized.
func (k ActorKind) Valid() bool {
	return k == ActorHuman || k == ActorAgent
}

// ActorRef identifies the human or agent responsible for a Claim.
type ActorRef struct {
	Kind ActorKind
	ID   ActorID
}

// Validate checks the ActorRef invariants.
func (a ActorRef) Validate() error {
	if !a.Kind.Valid() {
		return invalid("executor.kind", "unsupported value %q", a.Kind)
	}
	if strings.TrimSpace(string(a.ID)) == "" {
		return invalid("executor.id", "is required")
	}
	return nil
}

// ClaimEndReason records why an execution responsibility period ended.
type ClaimEndReason string

const (
	ClaimEndTaskCompleted      ClaimEndReason = "task_completed"
	ClaimEndSubmittedForReview ClaimEndReason = "submitted_for_review"
	ClaimEndReleased           ClaimEndReason = "released"
	ClaimEndRevoked            ClaimEndReason = "revoked"
	ClaimEndTaskFailed         ClaimEndReason = "task_failed"
)

// Valid reports whether the claim end reason is recognized.
func (r ClaimEndReason) Valid() bool {
	switch r {
	case ClaimEndTaskCompleted, ClaimEndSubmittedForReview, ClaimEndReleased, ClaimEndRevoked, ClaimEndTaskFailed:
		return true
	default:
		return false
	}
}

// Claim records one period in which an executor actively works on a Task.
type Claim struct {
	ID       ClaimID
	TaskID   TaskID
	Executor ActorRef

	ClaimedAt time.Time
	EndedAt   *time.Time
	EndReason ClaimEndReason
}

// Active reports whether the Claim is still the Task's current execution responsibility.
func (c Claim) Active() bool {
	return c.EndedAt == nil
}

// Validate checks the Claim invariants.
func (c Claim) Validate() error {
	if strings.TrimSpace(string(c.ID)) == "" {
		return invalid("claim.id", "is required")
	}
	if strings.TrimSpace(string(c.TaskID)) == "" {
		return invalid("claim.task_id", "is required")
	}
	if err := c.Executor.Validate(); err != nil {
		return err
	}
	if c.ClaimedAt.IsZero() {
		return invalid("claim.claimed_at", "is required")
	}

	if c.EndedAt == nil {
		if c.EndReason != "" {
			return invalid("claim.end_reason", "must be empty while the claim is active")
		}
		return nil
	}

	if c.EndedAt.IsZero() {
		return invalid("claim.ended_at", "must not be zero")
	}
	if c.EndedAt.Before(c.ClaimedAt) {
		return invalid("claim.ended_at", "must not be before claimed_at")
	}
	if !c.EndReason.Valid() {
		return invalid("claim.end_reason", "a valid reason is required after the claim ends")
	}
	return nil
}

// ValidateClaimHistory verifies the complete Claim history of a Task.
func ValidateClaimHistory(task Task, claims []Claim) error {
	seen := make(map[ClaimID]struct{}, len(claims))
	var previous time.Time
	var active *Claim

	for i := range claims {
		claim := &claims[i]
		if err := claim.Validate(); err != nil {
			return err
		}
		if claim.TaskID != task.ID {
			return invalid("claims", "claim %q belongs to another task", claim.ID)
		}
		if _, ok := seen[claim.ID]; ok {
			return invalid("claims", "contains duplicate claim %q", claim.ID)
		}
		seen[claim.ID] = struct{}{}

		if !previous.IsZero() && claim.ClaimedAt.Before(previous) {
			return invalid("claims", "must be ordered by claimed_at")
		}
		previous = claim.ClaimedAt

		if claim.Active() {
			if active != nil {
				return invalid("claims", "contains more than one active claim")
			}
			if i != len(claims)-1 {
				return invalid("claims", "the active claim must be the latest claim")
			}
			active = claim
		}
	}

	if task.ActiveClaimID == nil {
		if active != nil {
			return invalid("active_claim_id", "is missing while claim %q is active", active.ID)
		}
		return nil
	}
	if active == nil {
		return invalid("active_claim_id", "references a claim that is not active")
	}
	if active.ID != *task.ActiveClaimID {
		return invalid("active_claim_id", "does not match active claim %q", active.ID)
	}

	return nil
}

// ValidateTaskContext checks a Task together with its complete Claim history.
func ValidateTaskContext(mode CoordinationMode, task Task, claims []Claim) error {
	if err := task.Validate(mode); err != nil {
		return err
	}
	if err := ValidateClaimHistory(task, claims); err != nil {
		return err
	}

	claimsByID := make(map[ClaimID]Claim, len(claims))
	for _, claim := range claims {
		claimsByID[claim.ID] = claim
	}
	failuresByClaim := make(map[ClaimID]int, len(task.Failures))
	for _, failure := range task.Failures {
		claim, ok := claimsByID[failure.ClaimID]
		if !ok {
			return invalid("failure.claim_id", "claim %q does not exist in task history", failure.ClaimID)
		}
		if claim.Active() || claim.EndReason != ClaimEndTaskFailed {
			return invalid("failure.claim_id", "claim %q did not end with a failure", claim.ID)
		}
		if failure.FailedAt.Before(claim.ClaimedAt) || failure.FailedAt.After(*claim.EndedAt) {
			return invalid("failure.failed_at", "must fall within claim %q", claim.ID)
		}
		failuresByClaim[claim.ID]++
		if failuresByClaim[claim.ID] > 1 {
			return invalid("failures", "claim %q has more than one failure", claim.ID)
		}
	}
	submissionByID := make(map[SubmissionID]TaskSubmission, len(task.Submissions))
	submissionsByClaim := make(map[ClaimID]int, len(task.Submissions))
	for _, submission := range task.Submissions {
		claim, ok := claimsByID[submission.ClaimID]
		if !ok {
			return invalid("submission.claim_id", "claim %q does not exist in task history", submission.ClaimID)
		}
		if claim.Active() || (claim.EndReason != ClaimEndTaskCompleted && claim.EndReason != ClaimEndSubmittedForReview) {
			return invalid("submission.claim_id", "claim %q did not end with a submission", claim.ID)
		}
		if submission.SubmittedAt.Before(claim.ClaimedAt) || submission.SubmittedAt.After(*claim.EndedAt) {
			return invalid("submission.submitted_at", "must fall within claim %q", claim.ID)
		}
		submissionsByClaim[claim.ID]++
		if submissionsByClaim[claim.ID] > 1 {
			return invalid("submissions", "claim %q has more than one submission", claim.ID)
		}
		submissionByID[submission.ID] = submission
	}
	for _, claim := range claims {
		count := submissionsByClaim[claim.ID]
		switch claim.EndReason {
		case ClaimEndTaskCompleted, ClaimEndSubmittedForReview:
			if count != 1 {
				return invalid("submissions", "claim %q requires exactly one submission", claim.ID)
			}
		case ClaimEndReleased, ClaimEndRevoked, "":
			if count != 0 {
				return invalid("submissions", "claim %q must not have a submission", claim.ID)
			}
		case ClaimEndTaskFailed:
			if count != 0 {
				return invalid("submissions", "failed claim %q must not have a submission", claim.ID)
			}
		}

		failureCount := failuresByClaim[claim.ID]
		if claim.EndReason == ClaimEndTaskFailed {
			if failureCount != 1 {
				return invalid("failures", "claim %q requires exactly one failure", claim.ID)
			}
		} else if failureCount != 0 {
			return invalid("failures", "claim %q did not end with a failure", claim.ID)
		}
	}
	reviewsBySubmission := make(map[SubmissionID]int, len(task.Reviews))
	for _, review := range task.Reviews {
		if review.SubmissionID == nil {
			continue
		}
		submission, ok := submissionByID[*review.SubmissionID]
		if !ok {
			return invalid("review.submission_id", "submission %q does not exist in task history", *review.SubmissionID)
		}
		claim := claimsByID[submission.ClaimID]
		if claim.EndReason != ClaimEndSubmittedForReview {
			return invalid("review.submission_id", "submission %q was not submitted for review", submission.ID)
		}
		if review.RequestedAt.Before(submission.SubmittedAt) {
			return invalid("review.requested_at", "must not be before submission %q", submission.ID)
		}
		reviewsBySubmission[submission.ID]++
		if reviewsBySubmission[submission.ID] > 1 {
			return invalid("reviews", "submission %q has more than one review", submission.ID)
		}
	}
	for submissionID, submission := range submissionByID {
		claim := claimsByID[submission.ClaimID]
		if claim.EndReason == ClaimEndSubmittedForReview && reviewsBySubmission[submissionID] != 1 {
			return invalid("reviews", "submission %q requires exactly one review", submissionID)
		}
	}

	return nil
}
