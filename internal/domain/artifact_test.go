package domain

import (
	"errors"
	"testing"
	"time"
)

func TestWorkflowArtifactDefinitionsRequireUniqueNamesAndInstructions(t *testing.T) {
	task := WorkflowTaskDefinition{
		ID: "implement", Title: "Implement", Executor: ExecutorAgent,
		Execution: ExecutionRequired, ReviewPolicy: ReviewNone,
		Artifacts: []ArtifactDefinition{
			{Name: "commit", Description: "Provide the immutable commit."},
			{Name: "commit", Description: "Provide it again."},
		},
	}
	if err := task.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("duplicate Artifact names: %v", err)
	}
	task.Artifacts = []ArtifactDefinition{{Name: "commit"}}
	if err := task.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("missing Artifact instruction: %v", err)
	}
}

func TestArtifactRequiresAbsoluteURI(t *testing.T) {
	artifact := Artifact{
		ID: "artifact", WorkItemID: "work", TaskID: "task", ClaimID: "claim",
		Name: "commit", URI: "relative/path", CreatedAt: time.Now(),
	}
	if err := artifact.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("relative Artifact URI: %v", err)
	}
}
