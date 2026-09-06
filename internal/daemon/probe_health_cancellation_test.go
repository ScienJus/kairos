package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type cancelledHealthAdapter struct {
	fakeAdapter
	block       atomic.Bool
	entered     chan struct{}
	nilOnCancel bool
	failure     error
}

func (a *cancelledHealthAdapter) Probe(ctx context.Context) error {
	if a.block.Load() {
		select {
		case a.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		if a.nilOnCancel {
			return nil
		}
		return ctx.Err()
	}
	return a.failure
}

func TestCancelledRunningProbePreservesQuarantine(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				core, _, options := schedulerFixture(t)
				adapter := &cancelledHealthAdapter{entered: make(chan struct{}, 1)}
				adapter.observe = func(context.Context, RunRef) (RunObservation, error) {
					return RunObservation{State: OutcomeReady, Outcome: &HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}}, nil
				}
				s, err := NewScheduler(core, adapter, options)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				if mode == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), 6*time.Second)
				}
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- s.Run(ctx) }()
				time.Sleep(3 * time.Second)
				if stats := s.Stats(); stats.Claims != 1 || stats.Active != 0 {
					t.Fatalf("bad initial quarantine: %+v", stats)
				}
				s.mu.Lock()
				record := s.records[core.rows[0].Candidate]
				s.mu.Unlock()
				if !record.quarantined {
					t.Fatal("initial abandonment did not quarantine")
				}
				adapter.block.Store(true)
				<-adapter.entered
				if mode == "cancel" {
					cancel()
				}
				if err = <-done; err != nil {
					t.Fatal(err)
				}
				afterCancel := s.Stats()
				if afterCancel.Paused || afterCancel.ProbeFailures != 0 {
					t.Fatalf("caller abort changed health: %+v", afterCancel)
				}
				if err = s.Run(context.Background()); !errors.Is(err, ErrSchedulerAlreadyRun) {
					t.Fatalf("cancelled Scheduler accepted another Run: %v", err)
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.records[core.rows[0].Candidate] != record {
					t.Fatal("caller abort changed suppression")
				}
			})
		})
	}
}

func TestProbeOwnFailuresStillBackoffAndRecover(t *testing.T) {
	for _, kind := range []string{"timeout", "timeout_nil_result", "adapter_failure"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				core, _, options := schedulerFixture(t)
				adapter := &cancelledHealthAdapter{entered: make(chan struct{}, 1), nilOnCancel: kind == "timeout_nil_result"}
				if kind == "adapter_failure" {
					adapter.failure = errors.New("provider unavailable")
				} else {
					adapter.block.Store(true)
				}
				s, err := NewScheduler(core, adapter, options)
				if err != nil {
					t.Fatal(err)
				}
				candidate := core.rows[0]
				record := suppression{generation: candidate.Generation, quarantined: true, failures: options.MaxDispatches}
				s.records[candidate.Candidate] = record
				if s.probe(context.Background(), true) {
					t.Fatal("failed Probe reported healthy")
				}
				if stats := s.Stats(); !stats.Paused || stats.ProbeFailures != 1 {
					t.Fatalf("missing real failure: %+v", stats)
				}
				if s.probeBackoff <= 0 || !s.nextProbe.After(options.Dispatch.Clock.Now()) {
					t.Fatal("missing failure backoff")
				}
				adapter.block.Store(false)
				adapter.failure = nil
				if s.probe(context.Background(), true) {
					t.Fatal("bypassed failure backoff")
				}
				time.Sleep(2 * options.DiscoveryInterval)
				if !s.probe(context.Background(), true) || s.Stats().Paused || s.records[candidate.Candidate] != record {
					t.Fatal("health recovery must unpause without changing suppression")
				}
			})
		})
	}
}

func TestCancelledProbeCannotEraseExistingUnhealthyState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, _, options := schedulerFixture(t)
		adapter := &cancelledHealthAdapter{entered: make(chan struct{}, 1), failure: errors.New("provider unavailable"), nilOnCancel: true}
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		if s.probe(context.Background(), true) {
			t.Fatal("expected initial failure")
		}
		prior := s.Stats()
		next, backoff := s.nextProbe, s.probeBackoff
		time.Sleep(2 * options.DiscoveryInterval)
		adapter.block.Store(true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan bool, 1)
		go func() { done <- s.probe(ctx, true) }()
		<-adapter.entered
		cancel()
		if <-done {
			t.Fatal("aborted Probe reported healthy")
		}
		if stats := s.Stats(); !stats.Paused || stats.ProbeFailures != prior.ProbeFailures || s.nextProbe != next || s.probeBackoff != backoff {
			t.Fatalf("abort changed existing failure state: %+v", stats)
		}
	})
}
