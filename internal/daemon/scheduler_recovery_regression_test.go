package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type schedulerFaultTransport struct {
	failContext atomic.Bool
	dropClaim   atomic.Bool
	posts       atomic.Int64
}

func (r *schedulerFaultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/context") && r.failContext.Swap(false) {
		return nil, errors.New("preflight did not send Claim")
	}
	isClaim := req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/claims")
	if isClaim {
		r.posts.Add(1)
	}
	response, err := http.DefaultTransport.RoundTrip(req)
	if err == nil && isClaim && response.StatusCode < 300 && r.dropClaim.Swap(false) {
		response.Body.Close()
		return nil, errors.New("committed Claim response lost")
	}
	return response, err
}

func TestSchedulerUncertainClaimStillReconcilesDuringHealthPause(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	transport := &schedulerFaultTransport{}
	transport.dropClaim.Store(true)
	core, err := NewHTTPClient(f.client.base, f.client.token, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	a := &schedulerTestAdapter{}
	o := DefaultSchedulerOptions()
	o.Dispatch = testOptions()
	o.WorkspaceRoot = t.TempDir()
	s, err := NewScheduler(core, a, o)
	if err != nil {
		t.Fatal(err)
	}
	var run *scheduledRun
	s.admit(context.Background(), func(r *scheduledRun) { run = r })
	if run == nil || run.dispatch.Snapshot().ClaimID != "" {
		t.Fatal("expected unknown Claim result")
	}
	before, err := core.context(context.Background(), f.candidate)
	if err != nil || len(before.Claims) != 1 {
		t.Fatalf("expected committed SQL Claim: %v %v", before.Claims, err)
	}
	a.unhealthy = true
	if s.probe(context.Background(), true) {
		t.Fatal("expected health pause")
	}
	transport.failContext.Store(true)
	err = run.dispatch.step(context.Background())
	var attempt *ClaimAttemptError
	if !errors.As(err, &attempt) || attempt.State != ClaimNotSent {
		t.Fatalf("expected unsent retry: %v", err)
	}
	if err = run.dispatch.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := run.dispatch.Snapshot(); got.ClaimID != string(before.Claims[0].ID) {
		t.Fatalf("paused reconciliation did not recover existing Claim: %+v", got)
	}
	after, err := core.context(context.Background(), f.candidate)
	if err != nil || len(after.Claims) != 1 || transport.posts.Load() != 2 || !s.Stats().Paused {
		t.Fatalf("retry replaced Claim or bypassed pause incorrectly: claims=%v posts=%d stats=%+v err=%v", after.Claims, transport.posts.Load(), s.Stats(), err)
	}
	run.dispatch.RequestStop(StopRequested)
	if result := drain(t, run.dispatch); !result.ClaimEnded {
		t.Fatalf("cleanup: %+v", result)
	}
}

func TestSchedulerUnsentClaimRecoversWithFullSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, adapter, options := schedulerFixture(t)
		core.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: errors.New("preflight offline")}
		s, err := NewScheduler(core, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		synctest.Wait()
		adapter.mu.Lock()
		adapter.unhealthy = true
		adapter.mu.Unlock()
		time.Sleep(5 * time.Second)
		if !s.Stats().Paused || s.Stats().Claims != 0 {
			t.Fatalf("pause: %+v", s.Stats())
		}
		core.mu.Lock()
		core.claimError = nil
		core.mu.Unlock()
		adapter.mu.Lock()
		adapter.unhealthy = false
		adapter.mu.Unlock()
		time.Sleep(10 * time.Second)
		if s.Stats().Claims != 1 || s.Stats().Paused {
			t.Fatalf("recovery: %+v", s.Stats())
		}
		stop()
	})
}

type slowProbeAdapter struct{ fakeAdapter }

func (*slowProbeAdapter) Probe(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(80 * time.Millisecond):
		return nil
	}
}

func TestSchedulerProbeDoesNotConsumeClaimRequestBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, _, options := schedulerFixture(t)
		options.Dispatch.RequestTimeout = 120 * time.Millisecond
		s, err := NewScheduler(&slowClaimCore{core}, &slowProbeAdapter{}, options)
		if err != nil {
			t.Fatal(err)
		}
		var run *scheduledRun
		s.admit(context.Background(), func(r *scheduledRun) { run = r })
		if run == nil || run.dispatch.Snapshot().ClaimID == "" {
			t.Fatalf("healthy Probe consumed Core budget: %+v", s.Stats())
		}
		run.dispatch.RequestStop(StopRequested)
		if result := drain(t, run.dispatch); !result.ClaimEnded {
			t.Fatalf("cleanup: %+v", result)
		}
	})
}

type slowClaimCore struct{ *schedulerTestCore }

func (c *slowClaimCore) Claim(ctx context.Context, candidate Candidate, op string, token Secret, lease int64) (Claim, error) {
	select {
	case <-ctx.Done():
		return Claim{}, &ClaimAttemptError{State: ClaimNotSent, Err: ctx.Err()}
	case <-time.After(80 * time.Millisecond):
		return c.schedulerTestCore.Claim(ctx, candidate, op, token, lease)
	}
}

type partialDiscoveryCore struct {
	*schedulerTestCore
	err error
}

func (c *partialDiscoveryCore) Discover(ctx context.Context, tags []string, limit int, timeout time.Duration) ([]DiscoveredCandidate, error) {
	rows, _ := c.schedulerTestCore.Discover(ctx, tags, limit, timeout)
	return rows, c.err
}

func TestSchedulerPartialDiscoveryHonorsCancellationAndAuthority(t *testing.T) {
	for _, kind := range []string{"timeout", "cancelled", "unauthorized", "forbidden"} {
		t.Run(kind, func(t *testing.T) {
			core, adapter, options := schedulerFixture(t)
			partial := &partialDiscoveryCore{schedulerTestCore: core, err: context.DeadlineExceeded}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "cancelled":
				cancel()
			case "unauthorized":
				partial.err = &APIError{Status: 401}
			case "forbidden":
				partial.err = &APIError{Status: 403}
			}
			s, err := NewScheduler(partial, adapter, options)
			if err != nil {
				t.Fatal(err)
			}
			s.admit(ctx, func(*scheduledRun) {})
			want := uint64(0)
			if kind == "timeout" {
				want = 1
			}
			if got := s.Stats().Claims; got != want {
				t.Fatalf("Claims=%d, want %d", got, want)
			}
		})
	}
}

func TestSchedulerCandidateStopErrorDoesNotPauseAdmissions(t *testing.T) {
	core, adapter, options := schedulerFixture(t)
	failure := errors.New("candidate stop failed")
	adapter.stop = func(context.Context, RunRef, StopReason) error { return failure }
	s, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &schedulerAdapter{Adapter: adapter, scheduler: s}
	if err = wrapped.Stop(context.Background(), RunRef{ID: "run"}, StopRequested); !errors.Is(err, failure) || s.Stats().Paused {
		t.Fatalf("candidate error changed global health: %v %+v", err, s.Stats())
	}
}
