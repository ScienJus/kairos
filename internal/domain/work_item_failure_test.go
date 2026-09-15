package domain

import (
	"strings"
	"testing"
)

func TestWorkItemFailureAndRecoveryLimitValidation(t *testing.T) {
	fixture := func() WorkItem {
		return WorkItem{ID: "work", Definition: DefinitionBinding{ID: "workflow", Version: 1, Mode: CoordinationModeWorkflow}, Status: WorkItemStatusFailed, Title: "Failure", Goal: "Explain failure", CreatedAt: testTime, UpdatedAt: testTime, Failure: &WorkItemFailure{Kind: FailureWorkflowTaskInstanceLimit, Message: "节点达到上限", WorkflowTaskID: "dev", TaskInstances: 10, Limit: 10}}
	}
	for _, limit := range []int{0, 1, 500} {
		w := fixture()
		w.WorkflowMaxTaskInstancesPerNode = limit
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 501} {
		w := fixture()
		w.WorkflowMaxTaskInstancesPerNode = limit
		if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "workflow_max_task_instances_per_node") {
			t.Fatalf("override %d: %v", limit, err)
		}
	}
	for _, mutate := range []func(*WorkItem){
		func(w *WorkItem) { w.Status = WorkItemStatusOpen },
		func(w *WorkItem) { w.Failure.WorkflowTaskID = "" },
		func(w *WorkItem) { w.Failure.TaskInstances = 9 },
		func(w *WorkItem) { w.Failure.Kind = "unknown" },
		func(w *WorkItem) { w.Definition.Mode = CoordinationModeBlackboard },
	} {
		w := fixture()
		mutate(&w)
		if err := w.Validate(); err == nil {
			t.Fatal("invalid failure accepted")
		}
	}
	for _, text := range []string{strings.Repeat("x", MaxHistoryTextBytes), strings.Repeat("界", MaxHistoryTextBytes/3) + "xx"} {
		w := fixture()
		w.Failure.Message = text
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
		w.Failure.Message += "x"
		if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "failure.message") {
			t.Fatalf("oversized message: %v", err)
		}
	}
	w := fixture()
	w.Failure = nil
	w.Status = WorkItemStatusOpen
	w.Definition.Mode = CoordinationModeBlackboard
	w.WorkflowMaxTaskInstancesPerNode = 1
	if err := w.Validate(); err == nil {
		t.Fatal("Blackboard override accepted")
	}
}

func TestWorkflowRecoveryEventNames(t *testing.T) {
	for _, value := range []WorkItemEventType{"work_item.continued", "work_item.started_over"} {
		if !value.Valid() {
			t.Fatalf("missing event %q", value)
		}
	}
	for _, value := range []WorkItemEventType{"work_item.resumed", "work_item.restarted"} {
		if value.Valid() {
			t.Fatalf("removed event accepted: %q", value)
		}
	}
}
