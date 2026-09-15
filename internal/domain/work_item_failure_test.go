package domain

import (
	"strings"
	"testing"
)

func TestWorkItemFailureAndRecoveryLimitValidation(t *testing.T) {
	fixture := func() WorkItem {
		return WorkItem{ID: "work", Definition: DefinitionBinding{ID: "workflow", Version: 1, Mode: CoordinationModeWorkflow}, Status: WorkItemStatusFailed, Title: "Failure", Goal: "Explain failure", CreatedAt: testTime, UpdatedAt: testTime, Failure: &WorkItemFailure{Kind: FailureWorkflowExecutionLimit, Message: "节点达到上限", WorkflowTaskID: "dev", Executions: 10, Limit: 10}}
	}
	for _, limit := range []int{0, 1, 500} {
		w := fixture()
		w.WorkflowMaxTaskExecutions = limit
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 501} {
		w := fixture()
		w.WorkflowMaxTaskExecutions = limit
		if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "workflow_max_task_executions") {
			t.Fatalf("override %d: %v", limit, err)
		}
	}
	for _, mutate := range []func(*WorkItem){
		func(w *WorkItem) { w.Status = WorkItemStatusOpen },
		func(w *WorkItem) { w.Failure.WorkflowTaskID = "" },
		func(w *WorkItem) { w.Failure.Executions = 9 },
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
	w.WorkflowMaxTaskExecutions = 1
	if err := w.Validate(); err == nil {
		t.Fatal("Blackboard override accepted")
	}
}
