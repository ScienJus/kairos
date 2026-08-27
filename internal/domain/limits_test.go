package domain

import (
	"strings"
	"testing"
)

func TestHistoryTextLimit(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("x", MaxHistoryTextBytes)
	if err := validateHistoryText("value", valid); err != nil {
		t.Fatalf("text at limit: %v", err)
	}
	tooLong := valid + "x"
	if err := validateHistoryText("value", tooLong); err == nil {
		t.Fatal("text over limit was accepted")
	}
	multibyte := strings.Repeat("界", MaxHistoryTextBytes/len("界")+1)
	if len([]rune(multibyte)) >= MaxHistoryTextBytes || len(multibyte) <= MaxHistoryTextBytes {
		t.Fatal("test input does not distinguish Unicode characters from UTF-8 bytes")
	}
	if err := validateHistoryText("value", multibyte); err == nil {
		t.Fatal("multibyte text over the UTF-8 byte limit was accepted")
	}
}

func TestHistoryRecordsRejectOversizedText(t *testing.T) {
	t.Parallel()
	tooLong := strings.Repeat("x", MaxHistoryTextBytes+1)

	if err := (TaskSubmission{ID: "submission", TaskID: "task", ClaimID: "claim", Result: tooLong, SubmittedAt: testTime}).Validate(); err == nil {
		t.Fatal("oversized submission result was accepted")
	}
	if err := (TaskFailure{ID: "failure", TaskID: "task", ClaimID: "claim", Action: TaskFailureReopen, Reason: tooLong, FailedAt: testTime}).Validate(); err == nil {
		t.Fatal("oversized failure reason was accepted")
	}
	if err := (Review{ID: "review", TaskID: "task", Status: ReviewStatusRejected, RequestedBy: "reviewer", RequestedAt: testTime, Feedback: tooLong}).Validate(); err == nil {
		t.Fatal("oversized review feedback was accepted")
	}
	if err := (WorkflowTransition{ChoiceGroupID: "exit:task", TriggeredRelationIDs: []WorkflowRelationID{"relation"}, Reason: tooLong}).Validate(); err == nil {
		t.Fatal("oversized transition reason was accepted")
	}
	if err := (WorkItemEvent{ID: "event", WorkItemID: "work", Sequence: 1, Type: WorkItemEventWorkItemCreated, EntityID: "work", Message: tooLong, OccurredAt: testTime}).Validate(); err == nil {
		t.Fatal("oversized event message was accepted")
	}
	if err := (WorkItem{ID: "work", Definition: DefinitionBinding{ID: "definition", Version: 1, Mode: CoordinationModeBlackboard}, Status: WorkItemStatusOpen, Title: "title", Goal: "goal", Result: tooLong, CreatedAt: testTime, UpdatedAt: testTime}).Validate(); err == nil {
		t.Fatal("oversized work item result was accepted")
	}
	cancelledBy := ActorRef{Kind: ActorHuman, ID: "operator"}
	if err := (WorkItem{ID: "work", Definition: DefinitionBinding{ID: "definition", Version: 1, Mode: CoordinationModeBlackboard}, Status: WorkItemStatusCancelled, Title: "title", Goal: "goal", CreatedAt: testTime, UpdatedAt: testTime, CancelledAt: &testTime, CancelledBy: &cancelledBy, CancellationReason: tooLong}).Validate(); err == nil {
		t.Fatal("oversized cancellation reason was accepted")
	}
	if err := (Task{ID: "task", WorkItemID: "work", Status: TaskStatusSkipped, Title: "title", Executor: ExecutorAgent, SkipReason: tooLong, CreatedAt: testTime, UpdatedAt: testTime, CompletedAt: &testTime}).Validate(CoordinationModeBlackboard); err == nil {
		t.Fatal("oversized skip reason was accepted")
	}
}
