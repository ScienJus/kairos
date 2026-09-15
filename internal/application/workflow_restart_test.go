package application

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowRestartSummaryCopiesOnlyCurrentFailures(t *testing.T) {
	old := domain.TaskID("old")
	source := domain.WorkItem{ID: "source", Failure: &domain.WorkItemFailure{Kind: domain.FailureExecution, Message: "current Workflow failure"}, RestartContext: "old summary https://example.com/old", RecoveryInstructions: "old operator instructions"}
	tasks := []domain.Task{
		{ID: old, Status: domain.TaskStatusFailed, Failures: []domain.TaskFailure{{Reason: "replaced failure"}}},
		{ID: "new", RetryOfTaskID: &old, Status: domain.TaskStatusFailed, RetryContext: "old retry context", RetryInstructions: "old retry instructions", Failures: []domain.TaskFailure{{Reason: "earlier failure"}, {Reason: "current Task failure", RetryPrompt: "old retry prompt"}}, Submissions: []domain.TaskSubmission{{Result: "old result https://example.com/result"}}, Reviews: []domain.Review{{Status: domain.ReviewStatusRejected, Feedback: "old review"}}},
		{ID: "completed", Status: domain.TaskStatusCompleted, Failures: []domain.TaskFailure{{Reason: "resolved failure"}}},
	}
	summary := workflowRestartSummary(source, tasks)
	for _, required := range []string{"WorkItem source failed", "same Workflow version and initial nodes", "current Workflow failure", "current Task failure"} {
		if !strings.Contains(summary, required) {
			t.Fatalf("missing %q", required)
		}
	}
	for _, omitted := range []string{"old ", "https://", "earlier failure", "replaced failure", "resolved failure"} {
		if strings.Contains(summary, omitted) {
			t.Fatalf("copied historical content %q", omitted)
		}
	}
}

func TestWorkflowRestartSummaryUTF8Boundary(t *testing.T) {
	source := domain.WorkItem{ID: "source", Failure: &domain.WorkItemFailure{Kind: domain.FailureExecution}}
	budget := domain.MaxHistoryTextBytes - len(workflowRestartSummary(source, nil))
	for _, reason := range []string{strings.Repeat("x", budget), strings.Repeat("x", budget+1), strings.Repeat("界", budget/3) + strings.Repeat("x", budget%3), strings.Repeat("界", budget/3) + strings.Repeat("x", budget%3+1)} {
		source.Failure.Message = reason
		summary := workflowRestartSummary(source, nil)
		if len(summary) > domain.MaxHistoryTextBytes || !utf8.ValidString(summary) {
			t.Fatal("invalid UTF-8 byte bound")
		}
		if len(reason) == budget && !strings.Contains(summary, reason) {
			t.Fatal("exact boundary was truncated")
		}
		if len(reason) > budget && !strings.Contains(summary, "full history") {
			t.Fatal("missing truncation notice")
		}
	}
}
