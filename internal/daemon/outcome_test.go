package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestOutcomeRejectsMixedAndInapplicableFields(t *testing.T) {
	for _, o := range []HarnessOutcome{
		{},
		{Task: &TaskOutcome{Kind: Completed, Result: "done"}, Coordination: &CoordinationDecision{Kind: Abandoned}},
		{Task: &TaskOutcome{Kind: Completed, Result: "done", Transition: &Transition{ChoiceGroupID: "continue"}}},
		{Task: &TaskOutcome{Kind: Completed, Result: "done", Reason: "unexpected"}},
		{Task: &TaskOutcome{Kind: Completed, Result: "done", ArtifactIDs: []domain.ArtifactID{"a", "a"}}},
		{Task: &TaskOutcome{Kind: Abandoned, RetryPrompt: "unexpected"}},
		{Task: &TaskOutcome{Kind: TerminalFailure, Reason: "failure", RetryPrompt: "unexpected"}},
		{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{{Title: "child", Executor: domain.ExecutorAgent}}, RequestReview: true}},
	} {
		if o.Validate(taskCandidate()) == nil {
			t.Errorf("accepted invalid outcome: %+v", o)
		}
	}
}

func TestDecodeOutcomeStrictJSON(t *testing.T) {
	for _, data := range []string{`null`, `{}`, `{"task":{"kind":"completed","result":"done","cancel":true}}`, `{"task":{"kind":"completed","result":"done"}} {}`, `{"task":{"kind":"completed","result":7}}`} {
		if _, err := DecodeOutcome([]byte(data), taskCandidate()); err == nil {
			t.Fatalf("accepted invalid outcome: %s", data)
		}
	}
	if _, err := DecodeOutcome([]byte(`{"task":{"kind":"completed","result":"done"}}`), taskCandidate()); err != nil {
		t.Fatal(err)
	}
}

func TestObserveErrorsDoNotProveExitAndEventuallyStop(t *testing.T) {
	clock := &manualClock{now: time.Now()}
	options := testOptions()
	options.Clock = clock
	core := newFakeCore()
	a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
		return RunObservation{}, errors.New("temporarily unreachable")
	}}
	d := dispatchForTest(t, core, a, taskCandidate(), options)
	steps(t, d, 2)
	_ = d.step(context.Background())
	if d.Snapshot().State != Running {
		t.Fatal("single Observe error stopped the run")
	}
	clock.now = clock.now.Add(options.StopTimeout)
	_ = d.step(context.Background())
	if d.Snapshot().State != Stopping || d.Snapshot().RunState == RunStopped || d.Snapshot().Attempts != 1 {
		t.Fatalf("Observe error treated as exit: %+v", d.Snapshot())
	}
	clock.now = clock.now.Add(options.StopTimeout)
	s := drain(t, d)
	if s.State != Lost || core.applies != 0 || s.Attempts != 1 {
		t.Fatalf("Observe failure=%+v", s)
	}
}

func TestFinalizationRejectsConflictingHistory(t *testing.T) {
	core := newFakeCore()
	o := completedOutcome()
	d := dispatchForTest(t, core, outcomeAdapter(o), taskCandidate(), testOptions())
	steps(t, d, 3)
	core.claim.Active = false
	core.claim.EndReason = "task_completed"
	core.status.Task.Submissions = []domain.TaskSubmission{{ClaimID: "claim", Result: "a different outcome"}}
	s := drain(t, d)
	if s.OutcomeApplied || core.applies != 0 {
		t.Fatalf("claimed unrelated outcome: %+v", s)
	}
}

func TestCoreRejectionDoesNotBecomeBusinessFailure(t *testing.T) {
	for _, status := range []int{400, 403, 409, 413, 422} {
		core := newFakeCore()
		core.applyError = &APIError{Status: status, Code: "invalid_request"}
		d := dispatchForTest(t, core, outcomeAdapter(completedOutcome()), taskCandidate(), testOptions())
		s := drain(t, d)
		if s.OutcomeApplied || core.applies != 1 || core.releases != 1 || s.StopReason != StopCoreRejected {
			t.Fatalf("Core rejection %d=%+v", status, s)
		}
	}
}

type stalledHeartbeatCore struct {
	*fakeCore
	acknowledged bool
}

func (c *stalledHeartbeatCore) Heartbeat(ctx context.Context, candidate Candidate, id string, lease int64) (Claim, error) {
	if !c.acknowledged {
		c.acknowledged = true
		return c.fakeCore.Heartbeat(ctx, candidate, id, lease)
	}
	<-ctx.Done()
	return Claim{}, ctx.Err()
}

func TestHeartbeatTimeoutCannotOverrunSafetyDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		core := &stalledHeartbeatCore{fakeCore: newFakeCore()}
		var stoppedAt time.Time
		a := &fakeAdapter{
			stop: func(context.Context, RunRef, StopReason) error { stoppedAt = time.Now(); return nil },
			observe: func(context.Context, RunRef) (RunObservation, error) {
				if !stoppedAt.IsZero() {
					return RunObservation{State: RunStopped}, nil
				}
				return RunObservation{State: RunRunning}, nil
			},
		}
		d := dispatchForTest(t, core, a, taskCandidate(), testOptions())
		steps(t, d, 2)
		s, err := d.Run(context.Background())
		if err != nil || !s.ClaimEnded || s.StopReason != StopLeaseLost {
			t.Fatalf("deadline=%+v err=%v", s, err)
		}
		if stoppedAt.IsZero() || stoppedAt.Sub(start) > 14*time.Second {
			t.Fatalf("Stop after safety deadline: %s", stoppedAt.Sub(start))
		}
	})
}

func TestHumanInterventionOutcomeValidation(t *testing.T) {
	candidate := taskCandidate()
	candidate.Mode = domain.CoordinationModeWorkflow
	valid := HarnessOutcome{Task: &TaskOutcome{Kind: HumanInterventionRequired, Reason: "Human decision needed"}}
	if err := valid.Validate(candidate); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*TaskOutcome)
		want   string
	}{
		{"missing reason", func(o *TaskOutcome) { o.Reason = " " }, "business failure requires a reason"},
		{"retry prompt", func(o *TaskOutcome) { o.RetryPrompt = "retry" }, "retry prompt requires retryable_failure"},
		{"whitespace prompt", func(o *TaskOutcome) { o.RetryPrompt = " " }, "retry prompt requires retryable_failure"},
		{"result", func(o *TaskOutcome) { o.Result = "done" }, "invalid failure or abandoned fields"},
		{"artifacts", func(o *TaskOutcome) { o.ArtifactIDs = []domain.ArtifactID{"artifact"} }, "invalid failure or abandoned fields"},
		{"review", func(o *TaskOutcome) { o.RequestReview = true }, "invalid failure or abandoned fields"},
		{"transition", func(o *TaskOutcome) { o.Transition = &Transition{} }, "invalid failure or abandoned fields"},
		{"children", func(o *TaskOutcome) { o.Children = []TaskSpec{{Title: "child"}} }, "invalid failure or abandoned fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := *valid.Task
			tc.mutate(&task)
			if err := (HarnessOutcome{Task: &task}).Validate(candidate); err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	for _, reason := range []string{strings.Repeat("a", domain.MaxHistoryTextBytes), strings.Repeat("界", domain.MaxHistoryTextBytes/3) + strings.Repeat("a", domain.MaxHistoryTextBytes%3)} {
		task := *valid.Task
		task.Reason = reason
		if err := (HarnessOutcome{Task: &task}).Validate(candidate); err != nil {
			t.Fatal(err)
		}
		task.Reason += "a"
		if err := (HarnessOutcome{Task: &task}).Validate(candidate); err == nil || err.Error() != "outcome history text exceeds the Core byte limit" {
			t.Fatalf("byte limit: %v", err)
		}
	}
	candidate.Mode = domain.CoordinationModeBlackboard
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, NewSecret("test-token"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Apply(context.Background(), candidate, "claim", "operation", valid); err == nil || !strings.Contains(err.Error(), "human_intervention_required requires a Workflow Task") {
		t.Fatalf("Blackboard rejection: %v", err)
	}
	if calls != 0 {
		t.Fatalf("invalid outcome sent %d Core requests", calls)
	}
}
