package codexadapter

import (
	"encoding/json"
	"testing"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestHumanInterventionSchemaIsWorkflowOnly(t *testing.T) {
	for _, mode := range []domain.CoordinationMode{domain.CoordinationModeBlackboard, domain.CoordinationModeWorkflow} {
		candidate := daemon.Candidate{Kind: daemon.TaskCandidate, Mode: mode, WorkItemID: "work", TaskID: "task"}
		data, err := outcomeSchema(candidate)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			t.Fatal(err)
		}
		task := root["properties"].(map[string]any)["task"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
		kinds := task["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
		found := false
		for _, kind := range kinds {
			if kind == "human_intervention_required" {
				found = true
			}
		}
		if found != (mode == domain.CoordinationModeWorkflow) {
			t.Fatalf("mode %s kinds: %v", mode, kinds)
		}
	}
}
