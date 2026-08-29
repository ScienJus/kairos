package domain

import (
	"strings"
	"time"
)

// ReviewStatus is the state of one review round.
type ReviewStatus string

const (
	ReviewStatusPending  ReviewStatus = "pending"
	ReviewStatusApproved ReviewStatus = "approved"
	ReviewStatusRejected ReviewStatus = "rejected"
)

// Valid reports whether the review status is recognized.
func (s ReviewStatus) Valid() bool {
	return s == ReviewStatusPending || s == ReviewStatusApproved || s == ReviewStatusRejected
}

// Review records one human review round for a Task.
type Review struct {
	ID     ReviewID `json:"id"`
	TaskID TaskID   `json:"task_id"`

	// SubmissionID identifies the immutable result being reviewed.
	// It is nil when reviewing a skip decision that has no Submission.
	SubmissionID *SubmissionID `json:"submission_id"`

	Status ReviewStatus `json:"status"`

	RequestedBy ActorID   `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`

	DecidedBy *ActorID   `json:"decided_by"`
	DecidedAt *time.Time `json:"decided_at"`

	// Feedback contains the reviewer's decision comment.
	// It is required when the review is rejected.
	Feedback string `json:"feedback"`
}

// Validate checks the Review invariants.
func (r Review) Validate() error {
	if strings.TrimSpace(string(r.ID)) == "" {
		return invalid("review.id", "is required")
	}
	if strings.TrimSpace(string(r.TaskID)) == "" {
		return invalid("review.task_id", "is required")
	}
	if r.SubmissionID != nil && strings.TrimSpace(string(*r.SubmissionID)) == "" {
		return invalid("review.submission_id", "must not be empty")
	}
	if !r.Status.Valid() {
		return invalid("review.status", "unsupported value %q", r.Status)
	}
	if strings.TrimSpace(string(r.RequestedBy)) == "" {
		return invalid("review.requested_by", "is required")
	}
	if r.RequestedAt.IsZero() {
		return invalid("review.requested_at", "is required")
	}

	if r.Status == ReviewStatusPending {
		if r.DecidedBy != nil || r.DecidedAt != nil {
			return invalid("review.decision", "must be empty while pending")
		}
		return nil
	}

	if r.DecidedBy == nil || strings.TrimSpace(string(*r.DecidedBy)) == "" {
		return invalid("review.decided_by", "is required after a decision")
	}
	if r.DecidedAt == nil || r.DecidedAt.IsZero() {
		return invalid("review.decided_at", "is required after a decision")
	}
	if r.DecidedAt.Before(r.RequestedAt) {
		return invalid("review.decided_at", "must not be before requested_at")
	}
	if r.Status == ReviewStatusRejected && strings.TrimSpace(r.Feedback) == "" {
		return invalid("review.feedback", "is required when rejected")
	}
	if err := validateHistoryText("review.feedback", r.Feedback); err != nil {
		return err
	}

	return nil
}

func validateReviewHistory(taskID TaskID, reviews []Review) error {
	seen := make(map[ReviewID]struct{}, len(reviews))
	var previous time.Time

	for i, review := range reviews {
		if err := review.Validate(); err != nil {
			return err
		}
		if review.TaskID != taskID {
			return invalid("reviews", "review %q belongs to another task", review.ID)
		}
		if _, ok := seen[review.ID]; ok {
			return invalid("reviews", "contains duplicate review %q", review.ID)
		}
		seen[review.ID] = struct{}{}

		if !previous.IsZero() && review.RequestedAt.Before(previous) {
			return invalid("reviews", "must be ordered by requested_at")
		}
		previous = review.RequestedAt

		if review.Status == ReviewStatusPending && i != len(reviews)-1 {
			return invalid("reviews", "a pending review must be the latest review")
		}
	}

	return nil
}
