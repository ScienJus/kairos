package daemon

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

func sameSet[T cmp.Ordered](a, b []T) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func outcomeMatches(candidate Candidate, status ClaimStatus, intent HarnessOutcome, acknowledged bool) bool {
	if status.Claim.Active {
		return false
	}
	reason := status.Claim.EndReason
	if candidate.Kind != TaskCandidate {
		expected := map[OutcomeKind]string{CreateTask: "task_created", SubmitCompletion: "completion_submitted", AcceptCompletion: "completion_accepted", Abandoned: "released"}
		if reason != expected[intent.Kind()] {
			return false
		}
		if acknowledged {
			return true
		}
		switch intent.Kind() {
		case CreateTask:
			if status.Claim.EndedAt == nil {
				return false
			}
			for _, task := range status.Tasks {
				if task.ParentTaskID == nil && task.CreatedAt.Equal(*status.Claim.EndedAt) && taskSpecMatches(task, *intent.Coordination.Task) {
					return true
				}
			}
			return false
		case SubmitCompletion:
			return status.WorkItemResult == strings.TrimSpace(intent.Coordination.Result)
		default:
			return true
		}
	}
	t := intent.Task
	if t.Kind == Abandoned {
		return reason == "released"
	}
	if status.Task == nil {
		return false
	}
	switch t.Kind {
	case Decomposed:
		if reason != "task_decomposed" || status.Task.DecomposedAt == nil {
			return false
		}
		if acknowledged {
			return true
		}
		if status.Claim.EndedAt == nil || !status.Task.DecomposedAt.Equal(*status.Claim.EndedAt) {
			return false
		}
		used := map[domain.TaskID]bool{}
		for _, spec := range t.Children {
			found := false
			for _, child := range status.Tasks {
				if !used[child.ID] && child.ParentTaskID != nil && *child.ParentTaskID == candidate.TaskID && child.CreatedAt.Equal(*status.Claim.EndedAt) && taskSpecMatches(child, spec) {
					used[child.ID] = true
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case RetryableFailure, TerminalFailure:
		if reason != "task_failed" {
			return false
		}
		for _, failure := range status.Task.Failures {
			if string(failure.ClaimID) == status.Claim.ID && failure.Action == failureAction(t.Kind) && failure.Reason == t.Reason && failure.RetryPrompt == t.RetryPrompt {
				return true
			}
		}
	case Completed:
		expected, valid := expectedCompletionEndReason(candidate.Mode, *status.Task, t.RequestReview)
		if !valid || reason != string(expected) {
			return false
		}
		for _, submission := range status.Task.Submissions {
			if string(submission.ClaimID) != status.Claim.ID || submission.Result != strings.TrimSpace(t.Result) {
				continue
			}
			// Successful responses prove the original Artifact binding even if a
			// later retention policy has removed the content/metadata.
			if !acknowledged {
				ids := []domain.ArtifactID{}
				for _, artifact := range status.Artifacts {
					if artifact.SubmissionID != nil && *artifact.SubmissionID == submission.ID {
						ids = append(ids, artifact.ID)
					}
				}
				if !sameSet(ids, t.ArtifactIDs) {
					continue
				}
			}
			if t.Transition != nil {
				matched := false
				for _, decision := range status.Task.TransitionDecisions {
					if decision.SourceSubmissionID != nil && *decision.SourceSubmissionID == submission.ID && decision.ChoiceGroupID == t.Transition.ChoiceGroupID && decision.Reason == strings.TrimSpace(t.Transition.Reason) && sameSet(decision.SkipTaskIDs, t.Transition.SkipOptionalTaskIDs) && sameSet(decision.ReviewRequestedTaskIDs, t.Transition.ReviewSkippedTaskIDs) {
						matched = true
					}
				}
				if !matched {
					continue
				}
			}
			return true
		}
	}
	return false
}

// Match the effective Review decision, including Workflow's required policy.
// Missing or invalid policy cannot establish that our intent was applied.
func expectedCompletionEndReason(mode domain.CoordinationMode, task domain.Task, requested bool) (domain.ClaimEndReason, bool) {
	review := requested
	switch mode {
	case domain.CoordinationModeBlackboard:
	case domain.CoordinationModeWorkflow:
		if task.ReviewPolicy == nil {
			return "", false
		}
		switch *task.ReviewPolicy {
		case domain.ReviewNone:
			if requested {
				return "", false
			}
			review = false
		case domain.ReviewExecutorDecides:
		case domain.ReviewRequired:
			review = true
		default:
			return "", false
		}
	default:
		return "", false
	}
	if review {
		return domain.ClaimEndSubmittedForReview, true
	}
	return domain.ClaimEndTaskCompleted, true
}

func taskSpecMatches(task domain.Task, spec TaskSpec) bool {
	return task.Title == strings.TrimSpace(spec.Title) && task.Description == strings.TrimSpace(spec.Description) &&
		task.AcceptanceCriteria == strings.TrimSpace(spec.AcceptanceCriteria) && task.Executor == spec.Executor &&
		sameSet(task.AllowedRoles, spec.AllowedRoles) && sameSet(task.Tags, spec.Tags)
}
