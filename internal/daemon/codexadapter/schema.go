package codexadapter

import (
	"encoding/json"
	"slices"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
)

func object(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	// Keep the schema stable for logs, caching, and tests.
	for key := range properties {
		required = append(required, key)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func outcomeSchema(candidate daemon.Candidate) ([]byte, error) {
	str := map[string]any{"type": "string"}
	array := func(items any) any { return map[string]any{"type": "array", "items": items} }
	nullable := func(value any) any { return map[string]any{"anyOf": []any{value, map[string]any{"type": "null"}}} }
	spec := object(map[string]any{"title": str, "description": str, "acceptance_criteria": str, "executor": map[string]any{"type": "string", "enum": []string{"agent", "human", "either"}}, "allowed_roles": array(str), "tags": array(str)})
	var root map[string]any
	if candidate.Kind == daemon.TaskCandidate {
		kinds := []string{"completed", "retryable_failure", "terminal_failure", "abandoned"}
		if candidate.Mode == domain.CoordinationModeBlackboard {
			kinds = append(kinds, "decomposed")
		}
		transition := object(map[string]any{"choice_group_id": str, "skip_optional_task_ids": array(str), "review_skipped_task_ids": array(str), "reason": str})
		var transitionSchema any = nullable(transition)
		if candidate.Mode == domain.CoordinationModeBlackboard {
			transitionSchema = map[string]any{"type": "null"}
		}
		root = object(map[string]any{"task": object(map[string]any{"kind": map[string]any{"type": "string", "enum": kinds}, "result": str, "artifact_ids": array(str), "request_review": map[string]any{"type": "boolean"}, "transition": transitionSchema, "children": array(spec), "reason": str, "retry_prompt": str})})
	} else {
		kinds := []string{"create_task", "abandoned"}
		if candidate.Kind == daemon.WorkItemAcceptance {
			kinds = append(kinds, "accept_completion")
		} else {
			kinds = append(kinds, "submit_completion")
		}
		root = object(map[string]any{"coordination": object(map[string]any{"kind": map[string]any{"type": "string", "enum": kinds}, "task": nullable(spec), "result": str})})
	}
	properties := root["properties"].(map[string]any)
	for key, value := range properties {
		properties[key] = nullable(value)
	}
	properties["runtime_failure"] = nullable(object(map[string]any{"system": map[string]any{"type": "boolean"}}))
	return json.MarshalIndent(object(properties), "", "  ")
}
