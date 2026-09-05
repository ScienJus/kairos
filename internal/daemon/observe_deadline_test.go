package daemon

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestObserveFailureDeadlineIndependentOfPoll(t *testing.T) {
	for _, scenario := range []string{"errors", "blocked", "late_success"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				o := testOptions()
				o.PollInterval = time.Minute
				core := newFakeCore()
				var firstFailure, stoppedAt time.Time
				calls, stops := 0, 0
				a := &fakeAdapter{
					stop: func(context.Context, RunRef, StopReason) error { stops++; stoppedAt = time.Now(); return nil },
					observe: func(ctx context.Context, _ RunRef) (RunObservation, error) {
						calls++
						if !stoppedAt.IsZero() {
							return RunObservation{State: RunStopped}, nil
						}
						if firstFailure.IsZero() {
							firstFailure = time.Now()
							return RunObservation{}, errors.New("temporarily unavailable")
						}
						if !time.Now().Before(firstFailure.Add(o.StopTimeout)) {
							t.Error("new Observe request began after failure deadline")
						}
						if scenario != "errors" {
							deadline, ok := ctx.Deadline()
							if !ok || !deadline.Equal(firstFailure.Add(o.StopTimeout)) {
								t.Errorf("Observe request deadline=%v want=%v", deadline, firstFailure.Add(o.StopTimeout))
							}
							<-ctx.Done()
							if scenario == "late_success" {
								return RunObservation{State: RunRunning}, nil
							}
							return RunObservation{}, ctx.Err()
						}
						return RunObservation{}, errors.New("still unavailable")
					},
				}
				d := dispatchForTest(t, core, a, taskCandidate(), o)
				s, err := d.Run(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if !stoppedAt.Equal(firstFailure.Add(o.StopTimeout)) || stops != 1 || s.StopReason != StopRuntimeFailure || !s.ClaimEnded || s.State != Finished || s.Attempts != 1 || core.applies != 0 || core.releases != 1 {
					t.Fatalf("Stop delay=%s calls=%d stops=%d state=%+v", stoppedAt.Sub(firstFailure), calls, stops, s)
				}
				if core.heartbeats < 2 {
					t.Fatal("heartbeat stopped before observation recovery window ended")
				}
			})
		})
	}
}

func TestObserveRecoveryResetsFailureWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := testOptions()
		o.PollInterval = 10 * time.Second
		core := newFakeCore()
		var stoppedAt time.Time
		calls := []time.Time{}
		a := &fakeAdapter{
			stop: func(context.Context, RunRef, StopReason) error { stoppedAt = time.Now(); return nil },
			observe: func(context.Context, RunRef) (RunObservation, error) {
				if !stoppedAt.IsZero() {
					return RunObservation{State: RunStopped}, nil
				}
				calls = append(calls, time.Now())
				if len(calls) == 2 {
					return RunObservation{State: RunRunning}, nil
				}
				return RunObservation{}, errors.New("unavailable")
			},
		}
		d := dispatchForTest(t, core, a, taskCandidate(), o)
		s, err := d.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) < 3 || calls[1].Sub(calls[0]) != time.Second || calls[2].Sub(calls[1]) != o.PollInterval || !stoppedAt.Equal(calls[2].Add(o.StopTimeout)) || s.Attempts != 1 || s.State != Finished {
			t.Fatalf("recovery did not reset deadline/polling: calls=%v Stop=%s state=%+v", calls, stoppedAt, s)
		}
	})
}

func TestObserveDeadlineCheckedBeforeRequest(t *testing.T) {
	clock := &manualClock{now: time.Now()}
	o := testOptions()
	o.Clock = clock
	core := newFakeCore()
	calls := 0
	a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
		calls++
		return RunObservation{}, errors.New("unavailable")
	}}
	d := dispatchForTest(t, core, a, taskCandidate(), o)
	steps(t, d, 2)
	_ = d.Step(context.Background())
	clock.now = clock.now.Add(o.StopTimeout)
	_ = d.Step(context.Background())
	if calls != 1 || d.Snapshot().State != Stopping || core.releases != 0 {
		t.Fatalf("expired Observe request was sent or Claim released prematurely: calls=%d state=%+v", calls, d.Snapshot())
	}
}
