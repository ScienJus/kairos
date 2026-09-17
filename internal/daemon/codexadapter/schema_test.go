package codexadapter

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestOutcomeSchemaKinds(t *testing.T) {
	for _, tc := range []struct {
		kind daemon.CandidateKind
		mode domain.CoordinationMode
		want []string
	}{
		{daemon.TaskCandidate, domain.CoordinationModeWorkflow, []string{"completed", "retryable_failure", "work_item_failure", "candidate_declined", "human_intervention_required"}},
		{daemon.TaskCandidate, domain.CoordinationModeBlackboard, []string{"completed", "retryable_failure", "work_item_failure", "candidate_declined", "decomposed"}},
		{daemon.EmptyBlackboard, domain.CoordinationModeBlackboard, []string{"create_task", "candidate_declined", "submit_completion"}},
		{daemon.BlackboardCompletion, domain.CoordinationModeBlackboard, []string{"create_task", "candidate_declined", "submit_completion"}},
		{daemon.WorkItemAcceptance, domain.CoordinationModeBlackboard, []string{"create_task", "candidate_declined", "accept_completion"}},
	} {
		candidate := daemon.Candidate{Kind: tc.kind, Mode: tc.mode, WorkItemID: "work"}
		branch := "coordination"
		if tc.kind == daemon.TaskCandidate {
			candidate.TaskID = "task"
			branch = "task"
		}
		data, err := outcomeSchema(candidate)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			t.Fatal(err)
		}
		schema := root["properties"].(map[string]any)[branch].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
		kinds := schema["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
		got := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			got = append(got, kind.(string))
		}
		if !slices.Equal(got, tc.want) {
			t.Fatalf("mode %s kind %s: got %v want %v", tc.mode, tc.kind, got, tc.want)
		}
	}
}
