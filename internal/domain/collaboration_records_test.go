package domain

import (
	"errors"
	"testing"
	"time"
)

func TestTaskFailureReopensWithRetryPrompt(t *testing.T) {
	t.Parallel()

	failedAt := testTime.Add(time.Minute)
	task := Task{
		ID:         "task-1",
		WorkItemID: "work-1",
		Status:     TaskStatusPending,
		Title:      "Implement login endpoint",
		Executor:   ExecutorAgent,
		Failures: []TaskFailure{
			{
				ID:          "failure-1",
				TaskID:      "task-1",
				ClaimID:     "claim-1",
				Action:      TaskFailureReopen,
				Reason:      "The migration failed",
				RetryPrompt: "Inspect the existing schema before retrying.",
				FailedAt:    failedAt,
			},
		},
		CreatedAt: testTime,
		UpdatedAt: failedAt,
	}
	claims := []Claim{
		{
			ID:        "claim-1",
			TaskID:    "task-1",
			Executor:  ActorRef{Kind: ActorAgent, ID: "agent-1"},
			ClaimedAt: testTime,
			EndedAt:   &failedAt,
			EndReason: ClaimEndTaskFailed,
		},
	}

	if err := ValidateTaskContext(CoordinationModeBlackboard, task, claims); err != nil {
		t.Fatalf("reopened task failure: %v", err)
	}
}

func TestTaskFailureCanFailWorkItem(t *testing.T) {
	t.Parallel()

	failedAt := testTime.Add(time.Minute)
	task := Task{
		ID:         "task-1",
		WorkItemID: "work-1",
		Status:     TaskStatusFailed,
		Title:      "Implement login endpoint",
		Executor:   ExecutorAgent,
		Failures: []TaskFailure{
			{
				ID:       "failure-1",
				TaskID:   "task-1",
				ClaimID:  "claim-1",
				Action:   TaskFailureFailWorkItem,
				Reason:   "The requested change is impossible under the stated constraints",
				FailedAt: failedAt,
			},
		},
		CreatedAt: testTime,
		UpdatedAt: failedAt,
	}
	claims := []Claim{
		{
			ID:        "claim-1",
			TaskID:    "task-1",
			Executor:  ActorRef{Kind: ActorAgent, ID: "agent-1"},
			ClaimedAt: testTime,
			EndedAt:   &failedAt,
			EndReason: ClaimEndTaskFailed,
		},
	}

	if err := ValidateTaskContext(CoordinationModeBlackboard, task, claims); err != nil {
		t.Fatalf("work item failure: %v", err)
	}
}

func TestTaskFailureRejectsRetryPromptForGlobalFailure(t *testing.T) {
	t.Parallel()

	failure := TaskFailure{
		ID:          "failure-1",
		TaskID:      "task-1",
		ClaimID:     "claim-1",
		Action:      TaskFailureFailWorkItem,
		Reason:      "Cannot continue",
		RetryPrompt: "Retry anyway",
		FailedAt:    testTime,
	}
	if err := failure.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("retry prompt on global failure: got %v", err)
	}
}

func TestWorkflowGraphValidatesContinueAndExitDecisions(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"implementation"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("implementation", ExecutionRequired),
			workflowTask("test", ExecutionOptional),
			workflowTask("documentation", ExecutionOptional),
			workflowTask("release", ExecutionRequired),
		},
		Relations: []WorkflowRelationDefinition{
			workflowRelation("implementation-test", "implementation", "test"),
			workflowRelation("test-implementation", "test", "implementation"),
			workflowRelation("implementation-documentation", "implementation", "documentation"),
			workflowRelation("implementation-release", "implementation", "release"),
		},
	}

	continueDecision := workflowDecision(
		"continue:implementation-test",
		[]WorkflowRelationID{"implementation-test"},
		nil,
	)
	if err := graph.ValidateDecision("implementation", continueDecision); err != nil {
		t.Fatalf("continue decision keeps optional target: %v", err)
	}

	continueDecision.TriggeredRelationIDs = nil
	continueDecision.SkippedRelationIDs = []WorkflowRelationID{"implementation-test"}
	if err := graph.ValidateDecision("implementation", continueDecision); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("skipped continue relation: got %v", err)
	}

	exitDecision := workflowDecision(
		"exit:implementation",
		[]WorkflowRelationID{"implementation-release"},
		[]WorkflowRelationID{"implementation-documentation"},
	)
	exitDecision.SkipTaskIDs = []WorkflowTaskID{"documentation"}
	if err := graph.ValidateDecision("implementation", exitDecision); err != nil {
		t.Fatalf("exit decision skips optional target: %v", err)
	}

	exitDecision.TriggeredRelationIDs = []WorkflowRelationID{"implementation-documentation"}
	exitDecision.SkippedRelationIDs = []WorkflowRelationID{"implementation-release"}
	if err := graph.ValidateDecision("implementation", exitDecision); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("skipped required exit relation: got %v", err)
	}
}

func TestTaskValidatesAppliedTransitionDecision(t *testing.T) {
	t.Parallel()

	execution := ExecutionRequired
	reviewPolicy := ReviewNone
	workflowTaskID := WorkflowTaskID("implementation")
	workflowActivationID := WorkflowTaskActivationID("activation-1")
	submissionID := SubmissionID("submission-1")
	completedAt := testTime.Add(time.Minute)
	decision := workflowDecision(
		"exit:implementation",
		[]WorkflowRelationID{"implementation-release"},
		nil,
	)
	decision.SourceSubmissionID = &submissionID
	decision.AppliedAt = &completedAt
	task := Task{
		ID:                   "task-1",
		WorkItemID:           "work-1",
		WorkflowTaskID:       &workflowTaskID,
		WorkflowActivationID: &workflowActivationID,
		Status:               TaskStatusCompleted,
		Title:                "Implementation",
		Executor:             ExecutorAgent,
		Execution:            &execution,
		ReviewPolicy:         &reviewPolicy,
		TransitionDecisions:  []TransitionDecision{decision},
		Submissions: []TaskSubmission{
			{
				ID:          submissionID,
				TaskID:      "task-1",
				ClaimID:     "claim-1",
				Result:      "Implemented login",
				SubmittedAt: completedAt,
			},
		},
		CreatedAt:   testTime,
		UpdatedAt:   completedAt,
		CompletedAt: &completedAt,
	}

	if err := task.Validate(CoordinationModeWorkflow); err != nil {
		t.Fatalf("task with transition decision: %v", err)
	}
}

func TestValidateWorkItemEventHistory(t *testing.T) {
	t.Parallel()

	taskID := TaskID("task-1")
	actor := ActorRef{Kind: ActorAgent, ID: "agent-1"}
	events := []WorkItemEvent{
		{
			ID:         "event-1",
			WorkItemID: "work-1",
			Sequence:   1,
			Type:       WorkItemEventWorkItemCreated,
			EntityID:   "work-1",
			OccurredAt: testTime,
		},
		{
			ID:         "event-2",
			WorkItemID: "work-1",
			Sequence:   2,
			Type:       WorkItemEventTaskClaimed,
			TaskID:     &taskID,
			EntityID:   "claim-1",
			Actor:      &actor,
			OccurredAt: testTime.Add(time.Second),
		},
	}

	if err := ValidateWorkItemEventHistory("work-1", events); err != nil {
		t.Fatalf("valid event history: %v", err)
	}
	events[1].Sequence = 1
	if err := ValidateWorkItemEventHistory("work-1", events); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("duplicate event sequence: got %v", err)
	}
}

func workflowDecision(
	groupID WorkflowChoiceGroupID,
	triggered []WorkflowRelationID,
	skipped []WorkflowRelationID,
) TransitionDecision {
	return TransitionDecision{
		ID:           "decision-1",
		WorkItemID:   "work-1",
		SourceTaskID: "task-1",
		WorkflowTransition: WorkflowTransition{
			ChoiceGroupID:        groupID,
			TriggeredRelationIDs: triggered,
			SkippedRelationIDs:   skipped,
			Reason:               "Proceed based on the completed implementation",
		},
		DecidedBy: ActorRef{Kind: ActorAgent, ID: "agent-1"},
		DecidedAt: testTime,
	}
}
