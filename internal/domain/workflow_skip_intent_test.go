package domain

import (
	"errors"
	"testing"
)

func TestWorkflowSkipIntentValidatesReviewSubset(t *testing.T) {
	t.Parallel()

	intent := WorkflowSkipIntent{
		SkipTaskIDs:            []WorkflowTaskID{"docs", "examples"},
		ReviewRequestedTaskIDs: []WorkflowTaskID{"docs"},
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("validate skip intent: %v", err)
	}

	intent.ReviewRequestedTaskIDs = []WorkflowTaskID{"release"}
	if err := intent.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("review outside skip set: got %v", err)
	}
}
