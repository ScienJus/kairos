package application

import (
	"fmt"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// ArtifactSubmission keeps deliverables attached to the submission that produced
// them. None means that the submission did not request Review.
type ArtifactSubmission struct {
	TaskID       domain.TaskID       `json:"task_id"`
	SubmissionID domain.SubmissionID `json:"submission_id"`
	SubmittedAt  time.Time           `json:"submitted_at"`
	ReviewStatus string              `json:"review_status"`
	Artifacts    []domain.Artifact   `json:"artifacts"`
}

func artifactSubmissions(tasks []domain.Task, artifacts []domain.Artifact) []ArtifactSubmission {
	bySubmission := make(map[domain.SubmissionID][]domain.Artifact)
	for _, artifact := range artifacts {
		if artifact.SubmissionID != nil {
			bySubmission[*artifact.SubmissionID] = append(bySubmission[*artifact.SubmissionID], artifact)
		}
	}
	groups := []ArtifactSubmission{}
	for _, task := range tasks {
		for _, submission := range task.Submissions {
			values := bySubmission[submission.ID]
			if len(values) == 0 {
				continue
			}
			groups = append(groups, ArtifactSubmission{TaskID: task.ID, SubmissionID: submission.ID, SubmittedAt: submission.SubmittedAt, ReviewStatus: artifactReviewStatus(task, submission.ID), Artifacts: values})
		}
	}
	return groups
}

func artifactReviewStatus(task domain.Task, submissionID domain.SubmissionID) string {
	for _, review := range task.Reviews {
		if review.SubmissionID != nil && *review.SubmissionID == submissionID {
			return string(review.Status)
		}
	}
	return "none"
}

// workflowInputArtifacts resolves exact submission receipts, never the newest
// submission of a node or all transitive ancestors. A skipped source has no
// submission and contributes no artifacts. Retry activations retain these inputs.
func workflowInputArtifacts(store ReadStore, work domain.WorkItem, task domain.Task) ([]ArtifactSubmission, error) {
	if task.WorkflowActivationID == nil {
		return nil, conflict("workflow task is missing its activation")
	}
	activation, err := store.GetWorkflowTaskActivation(*task.WorkflowActivationID)
	if err != nil {
		return nil, fmt.Errorf("get artifact input activation: %w", err)
	}
	if activation.WorkItemID != work.ID || task.WorkflowTaskID == nil || activation.WorkflowTaskID != *task.WorkflowTaskID {
		return nil, conflict("workflow artifact input activation does not belong to task")
	}
	groups := []ArtifactSubmission{}
	seen := make(map[domain.SubmissionID]bool)
	for _, input := range activation.Inputs {
		if !input.Resolved() {
			continue
		}
		if input.SourceTaskID == nil {
			return nil, conflict("resolved artifact input is missing its source task")
		}
		source, err := store.GetTask(*input.SourceTaskID)
		if err != nil {
			return nil, fmt.Errorf("get artifact input source: %w", err)
		}
		if source.WorkItemID != work.ID {
			return nil, conflict("artifact input source belongs to another work item")
		}
		var decision *domain.TransitionDecision
		for i := range source.TransitionDecisions {
			if source.TransitionDecisions[i].ID == *input.DecisionID {
				decision = &source.TransitionDecisions[i]
				break
			}
		}
		if decision == nil || decision.AppliedAt == nil || decision.SourceTaskID != source.ID || decision.WorkItemID != work.ID {
			return nil, conflict("artifact input must reference an applied source decision")
		}
		if decision.SourceSubmissionID == nil {
			continue
		}
		id := *decision.SourceSubmissionID
		if seen[id] {
			continue
		}
		seen[id] = true
		var submission *domain.TaskSubmission
		for i := range source.Submissions {
			if source.Submissions[i].ID == id {
				submission = &source.Submissions[i]
				break
			}
		}
		if submission == nil {
			return nil, conflict("artifact input submission is missing from source task")
		}
		artifacts, err := store.ListArtifacts(ArtifactFilter{WorkItemID: work.ID, TaskID: source.ID, SubmittedOnly: true})
		if err != nil {
			return nil, fmt.Errorf("list artifact input source: %w", err)
		}
		values := []domain.Artifact{}
		for _, artifact := range artifacts {
			if artifact.SubmissionID != nil && *artifact.SubmissionID == id {
				values = append(values, artifact)
			}
		}
		if len(values) == 0 {
			continue
		}
		status := artifactReviewStatus(source, id)
		if status != "none" && status != string(domain.ReviewStatusApproved) {
			return nil, conflict("artifact input submission has not passed review")
		}
		groups = append(groups, ArtifactSubmission{TaskID: source.ID, SubmissionID: id, SubmittedAt: submission.SubmittedAt, ReviewStatus: status, Artifacts: values})
	}
	return groups, nil
}
