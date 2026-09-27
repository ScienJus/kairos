package daemonobs

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReportValidateClaimStatusRelationship(t *testing.T) {
	claimID := "claim-a"
	emptyClaimID := ""
	for _, test := range []struct {
		name      string
		status    string
		claimID   *string
		wantValid bool
	}{
		{"not attempted", "not_attempted", nil, true},
		{"uncertain", "uncertain", nil, true},
		{"active", "active", &claimID, true},
		{"ended", "ended", &claimID, true},
		{"active without claim", "active", nil, false},
		{"active with empty claim", "active", &emptyClaimID, false},
		{"ended without claim", "ended", nil, false},
		{"not attempted with claim", "not_attempted", &claimID, false},
		{"not attempted with empty claim", "not_attempted", &emptyClaimID, false},
		{"uncertain with claim", "uncertain", &claimID, false},
		{"uncertain with empty claim", "uncertain", &emptyClaimID, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := Report{
				Revision: 1, Lifecycle: "running", Health: "healthy", ActiveCount: 1,
				ActiveDispatches: []Dispatch{{
					ID: "4d383a89-154a-4794-bac3-e61cb8b3a6e4", CandidateKind: "empty_blackboard",
					WorkItemID: "work-a", ClaimID: test.claimID, ClaimStatus: test.status,
					State: "running", Attempts: 1, StartedAt: time.Now().UTC(),
				}},
			}
			err := report.Validate()
			if test.wantValid && err != nil {
				t.Fatalf("valid report rejected: %v", err)
			}
			if !test.wantValid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid report error = %v", err)
			}
		})
	}
}

func TestReportValidateTaskReferenceRelationship(t *testing.T) {
	taskID := "task-a"
	emptyTaskID := ""
	for _, test := range []struct {
		name          string
		candidateKind string
		taskID        *string
		wantValid     bool
	}{
		{"task with task id", "task", &taskID, true},
		{"task without task id", "task", nil, false},
		{"task with empty task id", "task", &emptyTaskID, false},
		{"coordination without task id", "empty_blackboard", nil, true},
		{"coordination with task id", "empty_blackboard", &taskID, false},
		{"coordination with empty task id", "empty_blackboard", &emptyTaskID, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := Report{
				Revision: 1, Lifecycle: "running", Health: "healthy", ActiveCount: 1,
				ActiveDispatches: []Dispatch{{
					ID: "4d383a89-154a-4794-bac3-e61cb8b3a6e4", CandidateKind: test.candidateKind,
					WorkItemID: "work-a", TaskID: test.taskID, ClaimStatus: "not_attempted",
					State: "prepared", StartedAt: time.Now().UTC(),
				}},
			}
			err := report.Validate()
			if test.wantValid && err != nil {
				t.Fatalf("valid report rejected: %v", err)
			}
			if !test.wantValid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid report error = %v", err)
			}
		})
	}
}

func TestReportValidateEventShape(t *testing.T) {
	for _, kind := range eventKinds {
		t.Run("valid "+kind, func(t *testing.T) {
			report := Report{Revision: 1, Lifecycle: "running", Health: "healthy", Events: []Event{eventFixture(kind)}}
			if err := report.Validate(); err != nil {
				t.Fatalf("valid report rejected: %v", err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		kind   string
		mutate func(*Event)
	}{
		{"candidate event with invalid kind", "claim_acquired", func(e *Event) { e.CandidateKind = stringPointer("not-a-kind") }},
		{"empty event reference", "claim_acquired", func(e *Event) { e.WorkItemID = stringPointer(" ") }},
		{"outcome event with unknown outcome", "outcome_applied", func(e *Event) { e.Details.Outcome = "unknown" }},
		{"ended event with negative attempts", "dispatch_ended", func(e *Event) { e.Details.Attempts = intPointer(-1) }},
		{"ended event with unknown reason", "dispatch_ended", func(e *Event) { e.Details.Reason = "provider error" }},
		{"applied event without outcome", "dispatch_ended", func(e *Event) { e.Details.Applied = boolPointer(true); e.Details.Outcome = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := eventFixture(test.kind)
			test.mutate(&event)
			report := Report{Revision: 1, Lifecycle: "running", Health: "healthy", Events: []Event{event}}
			if err := report.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid report error = %v", err)
			}
		})
	}
}

func TestEventDetailsPreserveExplicitFalseAndZero(t *testing.T) {
	encoded, err := json.Marshal(eventFixture("dispatch_ended"))
	if err != nil {
		t.Fatal(err)
	}
	value := string(encoded)
	for _, field := range []string{`"applied":false`, `"attempts":0`, `"duration_ms":0`} {
		if !strings.Contains(value, field) {
			t.Fatalf("event omitted %s: %s", field, value)
		}
	}
}

func eventFixture(kind string) Event {
	event := Event{Sequence: 1, Kind: kind, OccurredAt: time.Now().UTC()}
	if slices.Contains([]string{"admission_paused", "admission_resumed", "shutdown_started", "daemon_stopped"}, kind) {
		return event
	}
	event.CandidateKind = stringPointer("task")
	event.DispatchID = stringPointer("dispatch-a")
	event.WorkItemID = stringPointer("work-a")
	event.TaskID = stringPointer("task-a")
	if kind != "dispatch_ended" {
		event.ClaimID = stringPointer("claim-a")
	}
	switch kind {
	case "harness_started":
		event.Details.Attempts = intPointer(1)
	case "harness_retry":
		event.Details.Attempts = intPointer(2)
	case "outcome_applied":
		event.Details.Outcome = "completed"
	case "dispatch_ended":
		event.Details.State = "finished"
		event.Details.Applied = boolPointer(false)
		event.Details.Attempts = intPointer(0)
		event.Details.DurationMS = int64Pointer(0)
	}
	return event
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func intPointer(value int) *int          { return &value }
func int64Pointer(value int64) *int64    { return &value }
