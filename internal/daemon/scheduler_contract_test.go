package daemon

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestSchedulerRunIsSingleUse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, adapter, options := schedulerFixture(t)
		core.rows = nil
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		first := make(chan error, 1)
		go func() { first <- s.Run(ctx) }()
		synctest.Wait()
		second := make(chan error, 1)
		go func() { second <- s.Run(context.Background()) }()
		synctest.Wait()
		select {
		case err := <-second:
			if !errors.Is(err, ErrSchedulerAlreadyRun) {
				t.Fatalf("concurrent Run: %v", err)
			}
		default:
			t.Fatal("concurrent Run blocked instead of being rejected")
		}
		cancel()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		stats := s.Stats()
		if err := s.Run(context.Background()); !errors.Is(err, ErrSchedulerAlreadyRun) {
			t.Fatalf("repeated Run: %v", err)
		}
		if s.Stats() != stats {
			t.Fatal("rejected Run changed scheduler state")
		}
	})
}

func TestSchedulerCancelledInitialRunIsStillConsumed(t *testing.T) {
	core, adapter, options := schedulerFixture(t)
	s, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Run(context.Background()); !errors.Is(err, ErrSchedulerAlreadyRun) {
		t.Fatalf("cancelled initial Run was reusable: %v", err)
	}
	if s.Stats().Claims != 0 {
		t.Fatal("cancelled initial Run acquired work")
	}
}

func TestHealthRecoveryPreservesCrossClaimBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, adapter, options := schedulerFixture(t)
		options.Cooldown = time.Minute
		options.MaxDispatches = 2
		adapter.observe = func(context.Context, RunRef) (RunObservation, error) {
			return RunObservation{State: RuntimeFailed}, nil
		}
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		time.Sleep(10 * time.Second)
		if stats := s.Stats(); stats.Claims != 1 || stats.Active != 0 {
			t.Fatalf("first failure: %+v", stats)
		}
		s.mu.Lock()
		record := s.records[core.rows[0].Candidate]
		s.mu.Unlock()
		if record.failures != 1 || record.quarantined {
			t.Fatalf("failure record: %+v", record)
		}
		adapter.mu.Lock()
		adapter.unhealthy = true
		adapter.mu.Unlock()
		time.Sleep(10 * time.Second)
		if !s.Stats().Paused {
			t.Fatal("expected unhealthy pause")
		}
		adapter.mu.Lock()
		adapter.unhealthy = false
		adapter.mu.Unlock()
		// Settle any Probe that observed the old health before reading its backoff.
		synctest.Wait()
		s.mu.Lock()
		recoveryWait := s.nextProbe.Sub(options.Dispatch.Clock.Now()) + options.DiscoveryInterval
		s.mu.Unlock()
		time.Sleep(recoveryWait)
		synctest.Wait()
		s.mu.Lock()
		after := s.records[core.rows[0].Candidate]
		s.mu.Unlock()
		if s.Stats().Paused || s.Stats().Claims != 1 || after != record {
			t.Fatalf("recovery changed cooldown/budget: stats=%+v record=%+v", s.Stats(), after)
		}
		time.Sleep(time.Minute)
		if s.Stats().Claims != 2 {
			t.Fatalf("remaining budget not used: %+v", s.Stats())
		}
		adapter.mu.Lock()
		adapter.unhealthy = true
		adapter.mu.Unlock()
		time.Sleep(time.Minute)
		adapter.mu.Lock()
		adapter.unhealthy = false
		adapter.mu.Unlock()
		time.Sleep(2 * time.Minute)
		if s.Stats().Claims != 2 || s.Stats().Paused {
			t.Fatalf("health recovery reset exhausted budget: %+v", s.Stats())
		}
		stop()
		// A new instance intentionally has no process-local suppression history.
		fresh, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := fresh.selectCandidate(core.rows); !ok {
			t.Fatal("new Scheduler inherited quarantine")
		}
	})
}
