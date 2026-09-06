package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type queuedProbeAdapter struct {
	fakeAdapter
	slow                   atomic.Bool
	calls, active, maximum atomic.Int64
}

func (a *queuedProbeAdapter) Probe(ctx context.Context) error {
	a.calls.Add(1)
	n := a.active.Add(1)
	defer a.active.Add(-1)
	for old := a.maximum.Load(); n > old; old = a.maximum.Load() {
		if a.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	if !a.slow.Load() {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(150 * time.Millisecond):
		return nil
	}
}

func TestQueuedProbesDoNotBlockShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, _, options := schedulerFixture(t)
		options.Slots = 4
		options.Dispatch.RequestTimeout = 200 * time.Millisecond
		options.ShutdownTimeout = 50 * time.Millisecond
		options.DiscoveryInterval = 10 * time.Millisecond
		core.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: errors.New("preflight offline")}
		for i := 1; i < 3; i++ {
			row := core.rows[0]
			row.Candidate.TaskID = domain.TaskID(fmt.Sprintf("task-%d", i))
			core.rows = append(core.rows, row)
		}
		adapter := &queuedProbeAdapter{}
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		time.Sleep(50 * time.Millisecond)
		if s.Stats().Active != 3 {
			t.Fatalf("retained acquisitions: %+v", s.Stats())
		}
		adapter.slow.Store(true)
		time.Sleep(time.Second)
		if adapter.active.Load() != 1 {
			t.Fatal("expected a running Probe with acquisition retries queued")
		}
		started := time.Now()
		cancel()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		if elapsed, bound := time.Since(started), options.ShutdownTimeout+options.Dispatch.RequestTimeout; elapsed > bound {
			t.Fatalf("shutdown took %s, bound %s", elapsed, bound)
		}
		if adapter.maximum.Load() != 1 || s.Stats().Claims != 0 || s.Stats().Active != 0 {
			t.Fatalf("serialized shutdown: max=%d stats=%+v", adapter.maximum.Load(), s.Stats())
		}
	})
}

func TestProbeQueueCancellationDoesNotEnterAdapter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, _, options := schedulerFixture(t)
		adapter := &queuedProbeAdapter{}
		adapter.slow.Store(true)
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		first := make(chan bool, 1)
		go func() { first <- s.probe(context.Background(), true) }()
		synctest.Wait()
		if adapter.active.Load() != 1 {
			t.Fatal("first Probe not running")
		}
		// Queued cancellation must finish before the running Probe is released.
		ctx, cancel := context.WithCancel(context.Background())
		second := make(chan bool, 1)
		go func() { second <- s.probe(ctx, true) }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case result := <-second:
			if result {
				t.Fatal("cancelled Probe succeeded")
			}
		default:
			t.Fatal("cancelled waiter still blocked")
		}
		if adapter.calls.Load() != 1 || s.Stats().Probes != 1 || s.Stats().Paused {
			t.Fatalf("queued cancellation changed health: calls=%d stats=%+v", adapter.calls.Load(), s.Stats())
		}
		if !<-first {
			t.Fatal("cancelled waiter interrupted running Probe")
		}
		if !s.probe(context.Background(), true) || adapter.maximum.Load() != 1 {
			t.Fatal("Probe gate was not released or serialization broke")
		}
		// An already-cancelled context cannot consume an available slot either.
		if s.probe(ctx, true) || adapter.calls.Load() != 2 {
			t.Fatal("already-cancelled Probe entered Adapter")
		}
	})
}

func TestQueuedProbeDeadlineAndHealthSerial(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, _, options := schedulerFixture(t)
		adapter := &queuedProbeAdapter{}
		adapter.slow.Store(true)
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		first := make(chan bool, 1)
		go func() { first <- s.probe(context.Background(), true) }()
		synctest.Wait()
		s.signalSystemFailure()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		started := time.Now()
		if s.probe(ctx, true) {
			t.Fatal("expired queue waiter succeeded")
		}
		if elapsed := time.Since(started); elapsed != 20*time.Millisecond {
			t.Fatalf("queue wait ignored deadline: %s", elapsed)
		}
		if adapter.calls.Load() != 1 || !s.Stats().Paused {
			t.Fatal("queued waiter changed health")
		}
		if <-first || !s.Stats().Paused {
			t.Fatal("old healthy Probe erased a newer system failure")
		}
		time.Sleep(options.DiscoveryInterval)
		if !s.probe(context.Background(), true) || s.Stats().Paused {
			t.Fatal("healthy recovery failed after cancelled waiter")
		}
	})
}
