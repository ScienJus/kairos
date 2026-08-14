package domain

import (
	"errors"
	"testing"
	"time"
)

func TestWorkflowTaskActivationWaitsForAllInputs(t *testing.T) {
	t.Parallel()

	decisionID := TransitionDecisionID("decision-b")
	sourceTaskID := TaskID("task-b")
	resolvedAt := testTime.Add(time.Minute)
	activation := WorkflowTaskActivation{
		ID:             "activation-d",
		WorkItemID:     "work-1",
		WorkflowTaskID: "task-definition-d",
		CorrelationID:  "correlation-1",
		Inputs: []WorkflowActivationInput{
			{
				RelationID:   "b-d",
				SourceTaskID: &sourceTaskID,
				DecisionID:   &decisionID,
				Outcome:      WorkflowActivationInputTriggered,
				ResolvedAt:   &resolvedAt,
			},
			{RelationID: "c-d"},
		},
		Status:    WorkflowActivationWaiting,
		CreatedAt: testTime,
		UpdatedAt: resolvedAt,
	}
	if err := activation.Validate(); err != nil {
		t.Fatalf("waiting activation: %v", err)
	}

	secondDecisionID := TransitionDecisionID("decision-c")
	secondSourceTaskID := TaskID("task-c")
	activation.Inputs[1].SourceTaskID = &secondSourceTaskID
	activation.Inputs[1].DecisionID = &secondDecisionID
	activation.Inputs[1].Outcome = WorkflowActivationInputTriggered
	activation.Inputs[1].ResolvedAt = &resolvedAt
	if err := activation.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("fully filled waiting activation: got %v", err)
	}

	activation.Status = WorkflowActivationResolved
	activation.Outcome = WorkflowActivationCreated
	activation.ResolvedAt = &resolvedAt
	if err := activation.Validate(); err != nil {
		t.Fatalf("resolved activation: %v", err)
	}
}

func TestWorkflowTaskActivationRejectsPartialInput(t *testing.T) {
	t.Parallel()

	sourceTaskID := TaskID("task-b")
	activation := WorkflowTaskActivation{
		ID:             "activation-d",
		WorkItemID:     "work-1",
		WorkflowTaskID: "task-definition-d",
		CorrelationID:  "correlation-1",
		Inputs: []WorkflowActivationInput{
			{RelationID: "b-d", SourceTaskID: &sourceTaskID},
		},
		Status:    WorkflowActivationWaiting,
		CreatedAt: testTime,
		UpdatedAt: testTime,
	}
	if err := activation.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("partial activation input: got %v", err)
	}
}
