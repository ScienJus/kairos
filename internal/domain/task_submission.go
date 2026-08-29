package domain

import (
	"strings"
	"time"
)

// TaskSubmission records one immutable result submitted from a Claim.
type TaskSubmission struct {
	ID      SubmissionID `json:"id"`
	TaskID  TaskID       `json:"task_id"`
	ClaimID ClaimID      `json:"claim_id"`

	// Result summarizes the delivered work. Artifacts bind to this immutable
	// Submission through their SubmissionID.
	Result string `json:"result"`

	SubmittedAt time.Time `json:"submitted_at"`
}

// Validate checks the TaskSubmission invariants.
func (s TaskSubmission) Validate() error {
	if strings.TrimSpace(string(s.ID)) == "" {
		return invalid("submission.id", "is required")
	}
	if strings.TrimSpace(string(s.TaskID)) == "" {
		return invalid("submission.task_id", "is required")
	}
	if strings.TrimSpace(string(s.ClaimID)) == "" {
		return invalid("submission.claim_id", "is required")
	}
	if strings.TrimSpace(s.Result) == "" {
		return invalid("submission.result", "is required")
	}
	if err := validateHistoryText("submission.result", s.Result); err != nil {
		return err
	}
	if s.SubmittedAt.IsZero() {
		return invalid("submission.submitted_at", "is required")
	}
	return nil
}

func validateSubmissionHistory(taskID TaskID, submissions []TaskSubmission) error {
	seen := make(map[SubmissionID]struct{}, len(submissions))
	var previous time.Time

	for _, submission := range submissions {
		if err := submission.Validate(); err != nil {
			return err
		}
		if submission.TaskID != taskID {
			return invalid("submissions", "submission %q belongs to another task", submission.ID)
		}
		if _, ok := seen[submission.ID]; ok {
			return invalid("submissions", "contains duplicate submission %q", submission.ID)
		}
		seen[submission.ID] = struct{}{}

		if !previous.IsZero() && submission.SubmittedAt.Before(previous) {
			return invalid("submissions", "must be ordered by submitted_at")
		}
		previous = submission.SubmittedAt
	}

	return nil
}
