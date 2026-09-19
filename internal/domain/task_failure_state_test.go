package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRetryFailureStatusDependsOnCoordinationMode(t *testing.T) {
	t.Parallel()
	failure := TaskFailure{ID: "failure", TaskID: "task", ClaimID: "failed-claim", Action: TaskFailureRetry, Reason: "Retry needed", FailedAt: testTime.Add(time.Minute)}
	fixture := func(mode CoordinationMode, status TaskStatus) Task {
		task := Task{ID: "task", WorkItemID: "work", Status: status, Title: "Retry", Executor: ExecutorAgent, CreatedAt: testTime, UpdatedAt: testTime.Add(3 * time.Minute)}
		if mode == CoordinationModeWorkflow {
			node, activation := WorkflowTaskID("node"), WorkflowTaskActivationID("activation")
			execution, review := ExecutionOptional, ReviewExecutorDecides
			task.WorkflowTaskID, task.WorkflowActivationID = &node, &activation
			task.Execution, task.ReviewPolicy = &execution, &review
		}
		if status == TaskStatusWorking {
			claim := ClaimID("next-claim")
			task.ActiveClaimID = &claim
		}
		if status == TaskStatusCompleted || status == TaskStatusSkipped {
			task.CompletedAt = &task.UpdatedAt
		}
		if status == TaskStatusCompleted || status == TaskStatusInReview {
			task.Submissions = []TaskSubmission{{ID: "submission", TaskID: task.ID, ClaimID: "next-claim", Result: "Completed on retry", SubmittedAt: testTime.Add(2 * time.Minute)}}
		}
		if status == TaskStatusInReview {
			task.Reviews = []Review{{ID: "review", TaskID: task.ID, SubmissionID: &task.Submissions[0].ID, Status: ReviewStatusPending, RequestedBy: "agent", RequestedAt: task.UpdatedAt}}
		}
		return task
	}
	for _, mode := range []CoordinationMode{CoordinationModeWorkflow, CoordinationModeBlackboard} {
		for _, status := range []TaskStatus{TaskStatusPending, TaskStatusWorking, TaskStatusCompleted, TaskStatusSkipped, TaskStatusInReview} {
			t.Run(string(mode)+"/"+string(status), func(t *testing.T) {
				task := fixture(mode, status)
				if err := task.Validate(mode); err != nil {
					t.Fatalf("fixture must be valid before adding the retry failure: %v", err)
				}
				task.Failures = []TaskFailure{failure}
				err := task.Validate(mode)
				if mode == CoordinationModeWorkflow {
					if !errors.Is(err, ErrInvalidModel) || !strings.Contains(err.Error(), "status: must remain failed after retry in Workflow") {
						t.Fatalf("Workflow retry source with status %s: %v", status, err)
					}
				} else if err != nil {
					t.Fatalf("Blackboard must allow the same Task to progress after retry: %v", err)
				}
			})
		}
	}
	task := fixture(CoordinationModeWorkflow, TaskStatusFailed)
	task.Failures = []TaskFailure{failure}
	if err := task.Validate(CoordinationModeWorkflow); err != nil {
		t.Fatalf("failed Workflow retry source must remain valid: %v", err)
	}
}
