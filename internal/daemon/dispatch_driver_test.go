package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestDispatchSnapshotDerivesClaimAndFrozenIntent(t *testing.T) {
	core := newFakeCore()
	outcome := completedOutcome()
	adapter := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
		return RunObservation{State: OutcomeReady, Outcome: &outcome}, nil
	}}
	d := dispatchForTest(t, core, adapter, taskCandidate(), testOptions())
	if s := d.Snapshot(); s.Candidate != taskCandidate() || s.ClaimID != "" || s.ClaimEnded || s.Outcome != "" {
		t.Fatalf("prepared snapshot: %+v", s)
	}
	if err := d.claimOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := d.Snapshot()
	if snapshot.ClaimID != core.claim.ID || snapshot.ClaimEnded {
		t.Fatalf("claimed snapshot: %+v", snapshot)
	}
	snapshot.Candidate.TaskID = "unrelated"
	snapshot.ClaimID = "unrelated"
	snapshot.Outcome = Abandoned
	if s := d.Snapshot(); s.Candidate != taskCandidate() || s.ClaimID != core.claim.ID || s.Outcome != "" {
		t.Fatalf("snapshot mutation escaped: %+v", s)
	}
	steps(t, d, 2)
	outcome.Task.Kind = Abandoned
	if s := d.Snapshot(); s.State != Finalizing || s.Outcome != Completed || s.OutcomeApplied {
		t.Fatalf("intent was not frozen: %+v", s)
	}
	steps(t, d, 1)
	if core.claim.Active || d.Snapshot().ClaimEnded {
		t.Fatal("Apply response alone must not mark the Claim ended")
	}
	steps(t, d, 1)
	snapshot = d.Snapshot()
	if !snapshot.Terminal() || !snapshot.ClaimEnded || snapshot.EndReason != "task_completed" || !snapshot.OutcomeApplied {
		t.Fatalf("reconciled snapshot: %+v", snapshot)
	}
	if d.claim.EndReason != snapshot.EndReason || d.claim.EndedAt == nil {
		t.Fatal("Claim is not the authoritative end-state source")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), d.executorToken.Reveal()) {
		t.Fatal("Snapshot leaked executor credential")
	}
}

func TestDispatchLocalLeaseExpiryDoesNotEndClaim(t *testing.T) {
	clock := &manualClock{now: time.Now()}
	options := testOptions()
	options.Clock = clock
	core := newFakeCore()
	d := dispatchForTest(t, core, &fakeAdapter{}, taskCandidate(), options)
	if err := d.claimOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(options.Lease)
	if err := d.heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := d.Snapshot(); s.StopReason != StopLeaseLost || s.ClaimEnded || s.EndReason != "" || s.Terminal() {
		t.Fatalf("local expiry fabricated Core end state: %+v", s)
	}
}

func TestDispatchInitialClaimHandoffExcludesOtherDrivers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core := newFakeCore()
		d := dispatchForTest(t, core, &fakeAdapter{}, taskCandidate(), testOptions())
		entered := make(chan struct{})
		d.claimAdmission = func(ctx context.Context) bool { close(entered); <-ctx.Done(); return false }
		first := make(chan error, 1)
		go func() { first <- d.claimOnce(context.Background()) }()
		<-entered
		if _, err := d.Run(context.Background()); !errors.Is(err, ErrDispatchAlreadyRunning) {
			t.Fatalf("Run overlapped initial acquisition: %v", err)
		}
		if err := d.claimOnce(context.Background()); !errors.Is(err, ErrDispatchAlreadyRunning) {
			t.Fatalf("initial acquisitions overlapped: %v", err)
		}
		d.RequestStop(StopRequested)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		result, err := d.Run(context.Background())
		if err != nil || !result.Terminal() || result.ClaimEnded || core.claimCalls != 0 {
			t.Fatalf("handoff cancellation: %+v %v", result, err)
		}
	})
}

func TestDispatchRunRejectsConcurrentDriverButAllowsSequentialResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core := newFakeCore()
		entered := make(chan struct{})
		adapter := &fakeAdapter{start: func(ctx context.Context, _ StartRequest) (RunRef, error) {
			close(entered)
			<-ctx.Done()
			return RunRef{}, ctx.Err()
		}}
		d := dispatchForTest(t, core, adapter, taskCandidate(), testOptions())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := d.Run(ctx); done <- err }()
		<-entered
		before := d.Snapshot()
		second := make(chan error, 1)
		go func() { _, err := d.Run(context.Background()); second <- err }()
		synctest.Wait()
		select {
		case err := <-second:
			if !errors.Is(err, ErrDispatchAlreadyRunning) {
				t.Fatalf("concurrent Run: %v", err)
			}
		default:
			t.Fatal("concurrent Run queued instead of being rejected")
		}
		if err := d.claimOnce(context.Background()); !errors.Is(err, ErrDispatchAlreadyRunning) {
			t.Fatalf("acquisition overlapped Run: %v", err)
		}
		if after := d.Snapshot(); after != before {
			t.Fatalf("rejected driver changed state: %+v", after)
		}
		core.mu.Lock()
		core.inspectError = errors.New("offline")
		core.mu.Unlock()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
		if d.Snapshot().Terminal() {
			t.Fatal("unknown Claim was marked terminal")
		}
		core.mu.Lock()
		core.inspectError = nil
		core.mu.Unlock()
		result, err := d.Run(context.Background())
		if err != nil || !result.Terminal() || !result.ClaimEnded {
			t.Fatalf("sequential resume: %+v %v", result, err)
		}
		if core.claimCalls != 1 || result.Attempts != 1 {
			t.Fatalf("resume restarted work: %+v claims=%d", result, core.claimCalls)
		}
	})
}

type trackedHeartbeatCore struct {
	*fakeCore
	active, maximum atomic.Int64
}

func (c *trackedHeartbeatCore) Heartbeat(ctx context.Context, candidate Candidate, id string, lease int64) (Claim, error) {
	n := c.active.Add(1)
	defer c.active.Add(-1)
	for old := c.maximum.Load(); n > old; old = c.maximum.Load() {
		if c.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	select {
	case <-ctx.Done():
		return Claim{}, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return c.fakeCore.Heartbeat(ctx, candidate, id, lease)
}

func TestDispatchStartupAndGuardSerializeHeartbeat(t *testing.T) {
	// Mutex contention is not durably blocked in synctest. Use real time only
	// to let the competing heartbeat callers overlap; assert counts, not timing.
	core := &trackedHeartbeatCore{fakeCore: newFakeCore()}
	d := dispatchForTest(t, core, outcomeAdapter(completedOutcome()), taskCandidate(), testOptions())
	if err := d.claimOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.claimOnce(context.Background()); err == nil {
		t.Fatal("repeated initial acquisition accepted")
	}
	result, err := d.Run(context.Background())
	if err != nil || !result.OutcomeApplied || core.maximum.Load() != 1 || core.claimCalls != 1 {
		t.Fatalf("heartbeat ownership: %+v %v max=%d", result, err, core.maximum.Load())
	}
}
