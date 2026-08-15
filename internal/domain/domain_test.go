package domain

import (
	"errors"
	"testing"
	"time"
)

var testTime = time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)

func TestWorkItemValidateDefinitionBinding(t *testing.T) {
	t.Parallel()

	workItem := WorkItem{
		ID:         "work-1",
		Definition: DefinitionBinding{ID: "workflow-1", Version: 3, Mode: CoordinationModeWorkflow},
		Status:     WorkItemStatusOpen,
		Title:      "Implement login",
		Goal:       "Users can log in securely",
		CreatedAt:  testTime,
		UpdatedAt:  testTime,
	}
	if err := workItem.Validate(); err != nil {
		t.Fatalf("valid workflow work item: %v", err)
	}
	if got := workItem.CoordinationMode(); got != CoordinationModeWorkflow {
		t.Fatalf("coordination mode: got %q", got)
	}

	workItem.Definition.Mode = "unknown"
	if err := workItem.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("work item with invalid definition mode: got %v", err)
	}
}

func TestDefinitionsShareSuggestedTagGuidance(t *testing.T) {
	t.Parallel()

	metadata := DefinitionMetadata{
		ID:                "definition-1",
		Version:           1,
		Name:              "Kairos Development",
		AgentInstructions: "Read the design documents before changing the model.",
		SuggestedTags:     []string{"module:*", "kind:*", "testing"},
		Status:            DefinitionStatusPublished,
		CreatedAt:         testTime,
		UpdatedAt:         testTime,
	}

	blackboard := BlackboardDefinition{DefinitionMetadata: metadata}
	if err := blackboard.Validate(); err != nil {
		t.Fatalf("valid blackboard definition: %v", err)
	}
	if got := blackboard.Binding().Mode; got != CoordinationModeBlackboard {
		t.Fatalf("blackboard binding mode: got %q", got)
	}

	workflow := WorkflowDefinition{
		DefinitionMetadata: metadata,
		Graph: WorkflowGraph{
			StartTaskIDs: []WorkflowTaskID{"start"},
			Tasks: []WorkflowTaskDefinition{
				{
					ID:           "start",
					Title:        "Start",
					Executor:     ExecutorAgent,
					Execution:    ExecutionRequired,
					ReviewPolicy: ReviewNone,
				},
			},
		},
	}
	if err := workflow.Validate(); err != nil {
		t.Fatalf("valid workflow definition: %v", err)
	}
	if got := workflow.Binding().Mode; got != CoordinationModeWorkflow {
		t.Fatalf("workflow binding mode: got %q", got)
	}
}

func TestTaskValidateModeSpecificConfiguration(t *testing.T) {
	t.Parallel()

	execution := ExecutionRequired
	reviewPolicy := ReviewExecutorDecides
	workflowTaskID := WorkflowTaskID("workflow-task-1")
	workflowActivationID := WorkflowTaskActivationID("activation-1")
	task := Task{
		ID:                   "task-1",
		WorkItemID:           "work-1",
		WorkflowTaskID:       &workflowTaskID,
		WorkflowActivationID: &workflowActivationID,
		Status:               TaskStatusPending,
		Title:                "Implement login endpoint",
		Executor:             ExecutorAgent,
		AllowedRoles:         []string{"backend"},
		Tags:                 []string{"backend", "auth"},
		Execution:            &execution,
		ReviewPolicy:         &reviewPolicy,
		CreatedAt:            testTime,
		UpdatedAt:            testTime,
	}
	if err := task.Validate(CoordinationModeWorkflow); err != nil {
		t.Fatalf("valid workflow task: %v", err)
	}

	if err := task.Validate(CoordinationModeBlackboard); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("blackboard task with workflow fields: got %v", err)
	}

	task.WorkflowTaskID = nil
	task.WorkflowActivationID = nil
	task.Execution = nil
	task.ReviewPolicy = nil
	if err := task.Validate(CoordinationModeBlackboard); err != nil {
		t.Fatalf("valid blackboard task: %v", err)
	}
}

func TestTaskValidateActiveClaimInvariant(t *testing.T) {
	t.Parallel()

	task := Task{
		ID:         "task-1",
		WorkItemID: "work-1",
		Status:     TaskStatusWorking,
		Title:      "Implement login endpoint",
		Executor:   ExecutorAgent,
		CreatedAt:  testTime,
		UpdatedAt:  testTime,
	}
	if err := task.Validate(CoordinationModeBlackboard); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("working task without active claim: got %v", err)
	}

	claimID := ClaimID("claim-1")
	task.ActiveClaimID = &claimID
	if err := task.Validate(CoordinationModeBlackboard); err != nil {
		t.Fatalf("working task with active claim: %v", err)
	}

	task.Status = TaskStatusPending
	if err := task.Validate(CoordinationModeBlackboard); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("pending task with active claim: got %v", err)
	}
}

func TestClaimValidateLifecycle(t *testing.T) {
	t.Parallel()

	claim := Claim{
		ID:        "claim-1",
		TaskID:    "task-1",
		Executor:  ActorRef{Kind: ActorAgent, ID: "agent-1"},
		ClaimedAt: testTime,
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("active claim: %v", err)
	}

	endedAt := testTime.Add(time.Minute)
	claim.EndedAt = &endedAt
	if err := claim.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("ended claim without reason: got %v", err)
	}

	claim.EndReason = ClaimEndSubmittedForReview
	if err := claim.Validate(); err != nil {
		t.Fatalf("claim submitted for review: %v", err)
	}
}

func TestValidateTaskContextAfterSubmitForReview(t *testing.T) {
	t.Parallel()

	claimID := ClaimID("claim-1")
	submissionID := SubmissionID("submission-1")
	endedAt := testTime.Add(time.Minute)
	reviewedAt := endedAt.Add(time.Minute)
	task := Task{
		ID:         "task-1",
		WorkItemID: "work-1",
		Status:     TaskStatusInReview,
		Title:      "Implement login endpoint",
		Executor:   ExecutorAgent,
		Reviews: []Review{
			{
				ID:           "review-1",
				TaskID:       "task-1",
				SubmissionID: &submissionID,
				Status:       ReviewStatusPending,
				RequestedBy:  "agent-1",
				RequestedAt:  reviewedAt,
			},
		},
		Submissions: []TaskSubmission{
			{
				ID:          submissionID,
				TaskID:      "task-1",
				ClaimID:     claimID,
				Result:      "Implemented login endpoint",
				SubmittedAt: endedAt,
			},
		},
		CreatedAt: testTime,
		UpdatedAt: reviewedAt,
	}
	claims := []Claim{
		{
			ID:        claimID,
			TaskID:    task.ID,
			Executor:  ActorRef{Kind: ActorAgent, ID: "agent-1"},
			ClaimedAt: testTime,
			EndedAt:   &endedAt,
			EndReason: ClaimEndSubmittedForReview,
		},
	}

	if err := ValidateTaskContext(CoordinationModeBlackboard, task, claims); err != nil {
		t.Fatalf("task submitted for review: %v", err)
	}
}

func TestValidateClaimHistoryMatchesTaskActiveClaim(t *testing.T) {
	t.Parallel()

	activeClaimID := ClaimID("claim-2")
	firstEndedAt := testTime.Add(time.Minute)
	task := Task{
		ID:            "task-1",
		WorkItemID:    "work-1",
		Status:        TaskStatusWorking,
		ActiveClaimID: &activeClaimID,
	}
	claims := []Claim{
		{
			ID:        "claim-1",
			TaskID:    task.ID,
			Executor:  ActorRef{Kind: ActorAgent, ID: "agent-1"},
			ClaimedAt: testTime,
			EndedAt:   &firstEndedAt,
			EndReason: ClaimEndReleased,
		},
		{
			ID:        activeClaimID,
			TaskID:    task.ID,
			Executor:  ActorRef{Kind: ActorAgent, ID: "agent-2"},
			ClaimedAt: firstEndedAt.Add(time.Minute),
		},
	}

	if err := ValidateClaimHistory(task, claims); err != nil {
		t.Fatalf("claim history: %v", err)
	}
}

func TestTaskValidateCompleteReviewHistory(t *testing.T) {
	t.Parallel()

	reviewer := ActorID("human-1")
	firstSubmissionID := SubmissionID("submission-1")
	secondSubmissionID := SubmissionID("submission-2")
	thirdSubmissionID := SubmissionID("submission-3")
	firstDecision := testTime.Add(2 * time.Minute)
	secondRequest := testTime.Add(3 * time.Minute)
	secondDecision := testTime.Add(4 * time.Minute)
	thirdRequest := testTime.Add(5 * time.Minute)

	task := Task{
		ID:         "task-1",
		WorkItemID: "work-1",
		Status:     TaskStatusInReview,
		Title:      "Implement login endpoint",
		Executor:   ExecutorAgent,
		Reviews: []Review{
			{
				ID:           "review-1",
				TaskID:       "task-1",
				SubmissionID: &firstSubmissionID,
				Status:       ReviewStatusRejected,
				RequestedBy:  "agent-1",
				RequestedAt:  testTime.Add(time.Minute),
				DecidedBy:    &reviewer,
				DecidedAt:    &firstDecision,
				Feedback:     "Add error handling",
			},
			{
				ID:           "review-2",
				TaskID:       "task-1",
				SubmissionID: &secondSubmissionID,
				Status:       ReviewStatusRejected,
				RequestedBy:  "agent-1",
				RequestedAt:  secondRequest,
				DecidedBy:    &reviewer,
				DecidedAt:    &secondDecision,
				Feedback:     "Add missing tests",
			},
			{
				ID:           "review-3",
				TaskID:       "task-1",
				SubmissionID: &thirdSubmissionID,
				Status:       ReviewStatusPending,
				RequestedBy:  "agent-1",
				RequestedAt:  thirdRequest,
			},
		},
		Submissions: []TaskSubmission{
			{ID: firstSubmissionID, TaskID: "task-1", ClaimID: "claim-1", Result: "First result", SubmittedAt: testTime.Add(time.Minute)},
			{ID: secondSubmissionID, TaskID: "task-1", ClaimID: "claim-2", Result: "Second result", SubmittedAt: secondRequest},
			{ID: thirdSubmissionID, TaskID: "task-1", ClaimID: "claim-3", Result: "Third result", SubmittedAt: thirdRequest},
		},
		CreatedAt: testTime,
		UpdatedAt: thirdRequest,
	}

	if err := task.Validate(CoordinationModeBlackboard); err != nil {
		t.Fatalf("task with complete review history: %v", err)
	}
	if got := len(task.Reviews); got != 3 {
		t.Fatalf("review history length: got %d, want 3", got)
	}
}

func TestReviewValidateRequiresFeedbackOnRejection(t *testing.T) {
	t.Parallel()

	reviewer := ActorID("human-1")
	decidedAt := testTime.Add(time.Minute)
	review := Review{
		ID:          "review-1",
		TaskID:      "task-1",
		Status:      ReviewStatusRejected,
		RequestedBy: "agent-1",
		RequestedAt: testTime,
		DecidedBy:   &reviewer,
		DecidedAt:   &decidedAt,
	}
	if err := review.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("rejected review without feedback: got %v", err)
	}
}

func TestValidateRuntimeTaskGraphUnfoldsWorkflowLoop(t *testing.T) {
	t.Parallel()

	workItemID := WorkItemID("work-1")
	tasks := []Task{
		{ID: "implement-1", WorkItemID: workItemID},
		{ID: "test-1", WorkItemID: workItemID},
		{ID: "implement-2", WorkItemID: workItemID},
		{ID: "test-2", WorkItemID: workItemID},
	}
	relations := []TaskRelation{
		{WorkItemID: workItemID, FromTaskID: "implement-1", ToTaskID: "test-1", CreatedAt: testTime},
		{WorkItemID: workItemID, FromTaskID: "test-1", ToTaskID: "implement-2", CreatedAt: testTime},
		{WorkItemID: workItemID, FromTaskID: "implement-2", ToTaskID: "test-2", CreatedAt: testTime},
	}

	if err := ValidateRuntimeTaskGraph(workItemID, tasks, relations); err != nil {
		t.Fatalf("unfolded workflow loop: %v", err)
	}
}

func TestValidateRuntimeTaskGraphRejectsCycle(t *testing.T) {
	t.Parallel()

	workItemID := WorkItemID("work-1")
	tasks := []Task{
		{ID: "task-1", WorkItemID: workItemID},
		{ID: "task-2", WorkItemID: workItemID},
	}
	relations := []TaskRelation{
		{WorkItemID: workItemID, FromTaskID: "task-1", ToTaskID: "task-2", CreatedAt: testTime},
		{WorkItemID: workItemID, FromTaskID: "task-2", ToTaskID: "task-1", CreatedAt: testTime},
	}

	if err := ValidateRuntimeTaskGraph(workItemID, tasks, relations); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("cyclic runtime graph: got %v", err)
	}
}
