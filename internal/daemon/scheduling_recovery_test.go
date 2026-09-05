package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestLongObservePollDoesNotDelayHeartbeatOrStop(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		t.Run(fmt.Sprintf("stalled_%t", stalled), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				o := testOptions()
				o.PollInterval = time.Minute
				core := newFakeCore()
				var transport Core = core
				if stalled {
					transport = &stalledHeartbeatCore{fakeCore: core}
				}
				var stoppedAt time.Time
				observations := 0
				a := &fakeAdapter{
					stop: func(context.Context, RunRef, StopReason) error { stoppedAt = time.Now(); return nil },
					observe: func(context.Context, RunRef) (RunObservation, error) {
						observations++
						if !stoppedAt.IsZero() {
							return RunObservation{State: RunStopped}, nil
						}
						return RunObservation{State: RunRunning}, nil
					},
				}
				d := dispatchForTest(t, transport, a, taskCandidate(), o)
				start := time.Now()
				if !stalled {
					go func() { time.Sleep(2 * o.Lease); d.RequestStop(StopRequested) }()
				}
				s, err := d.Run(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				want := start.Add(2 * o.Lease)
				if stalled {
					want = start.Add(o.Lease - leaseSafetyMargin(o.Lease))
				}
				if stoppedAt != want || !s.Terminal() || s.State == Lost || !s.ClaimEnded || observations != 2 {
					t.Fatalf("Stop=%s want=%s observations=%d state=%+v", stoppedAt.Sub(start), want.Sub(start), observations, s)
				}
				if !stalled && core.heartbeats < 9 {
					t.Fatalf("slow Observe starved heartbeat: %d", core.heartbeats)
				}
			})
		})
	}
}

func TestExpiredStopWindowStillRequestsTermination(t *testing.T) {
	for _, ended := range []bool{false, true} {
		for _, stopFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("ended_%t/stop_fails_%t", ended, stopFails), func(t *testing.T) {
				clock := &manualClock{now: time.Now()}
				o := testOptions()
				o.Clock = clock
				core := newFakeCore()
				calls := 0
				a := &fakeAdapter{stop: func(context.Context, RunRef, StopReason) error {
					calls++
					if stopFails {
						return errors.New("Stop unavailable")
					}
					return nil
				}}
				d := dispatchForTest(t, core, a, taskCandidate(), o)
				steps(t, d, 2)
				d.RequestStop(StopRequested)
				clock.now = clock.now.Add(o.StopTimeout)
				if ended {
					core.claim.Active = false
					core.claim.EndReason = "expired"
				}
				s := drain(t, d)
				wantReleases := 1
				if ended {
					wantReleases = 0
				}
				if calls != 1 || s.State != Lost || !s.ClaimEnded || core.releases != wantReleases {
					t.Fatalf("Stop calls=%d releases=%d state=%+v", calls, core.releases, s)
				}
			})
		}
	}
}

// Fail before forwarding, either during context preflight or during Claim POST.
// In the latter case HTTPClient cannot know whether a real server committed.
type failAcquisitionTransport struct {
	base   http.RoundTripper
	method string
	failed bool
}

func (r *failAcquisitionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !r.failed && req.Method == r.method && (r.method == http.MethodGet || strings.HasSuffix(req.URL.Path, "/claims") || strings.HasSuffix(req.URL.Path, "/coordination-claims")) {
		r.failed = true
		return nil, errors.New("connection failed")
	}
	return r.base.RoundTrip(req)
}

func TestHTTPAcquisitionConflictResolvesAfterConnectionFailure(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard, BlackboardCompletion, WorkItemAcceptance} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(string(kind)+"/"+method, func(t *testing.T) {
				f := newHTTPFixture(t, domain.CoordinationModeBlackboard, kind)
				f.transport.claimLost = true
				f.client.http.Transport = &failAcquisitionTransport{base: f.transport, method: method}
				d := dispatchForTest(t, f.client, &fakeAdapter{}, f.candidate, testOptions())
				if err := d.Step(context.Background()); err == nil {
					t.Fatal("expected interrupted acquisition")
				}
				if d.uncertainClaim != (method == http.MethodPost) {
					t.Fatal("incorrect acquisition uncertainty")
				}
				ctx := context.Background()
				if kind == TaskCandidate {
					_, err := f.service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: f.candidate.TaskID, Identity: f.agent})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, err := f.service.ClaimWorkCandidate(ctx, application.ClaimWorkCandidateCommand{WorkItemID: f.candidate.WorkItemID, Kind: application.WorkCandidateKind(kind), Identity: f.agent})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := d.Step(ctx); !statusIs(err, 409) {
					t.Fatalf("expected Core refusal, got %v", err)
				}
				s := d.Snapshot()
				if !s.Terminal() || s.ClaimID != "" || s.Attempts != 0 || d.uncertainClaim {
					t.Fatalf("unresolved refused acquisition=%+v", s)
				}
				view, err := f.service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
				if err != nil {
					t.Fatal(err)
				}
				active := 0
				for _, claim := range view.Claims {
					if claim.ExecutorTokenHash != "" {
						t.Fatal("Dispatch created a Claim")
					}
					if claim.Active() {
						active++
					}
				}
				for _, claim := range view.CoordinationClaims {
					if claim.ExecutorTokenHash != "" {
						t.Fatal("Dispatch created a Coordination Claim")
					}
					if claim.Active() {
						active++
					}
				}
				if active != 1 {
					t.Fatal("competitor Claim was changed")
				}
			})
		}
	}
}

func TestPreflightFailureCanStopWithoutClaim(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	f.client.http.Transport = &failAcquisitionTransport{base: f.transport, method: http.MethodGet}
	d := dispatchForTest(t, f.client, &fakeAdapter{}, f.candidate, testOptions())
	if err := d.Step(context.Background()); err == nil {
		t.Fatal("expected preflight failure")
	}
	d.RequestStop(StopRequested)
	steps(t, d, 1)
	if !d.Snapshot().Terminal() || f.transport.claimPosts != 0 || d.uncertainClaim {
		t.Fatalf("preflight-only stop=%+v", d.Snapshot())
	}
}

func TestEarlierUncertainClaimSurvivesLaterPreflightFailure(t *testing.T) {
	for _, status := range []int{401, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			core := newFakeCore()
			core.dropClaim = true
			d := dispatchForTest(t, core, &fakeAdapter{}, taskCandidate(), testOptions())
			if err := d.Step(context.Background()); err == nil {
				t.Fatal("expected lost Claim response")
			}
			core.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: &APIError{Status: status}}
			d.RequestStop(StopRequested)
			_ = d.Step(context.Background())
			if !d.uncertainClaim || d.Snapshot().Terminal() {
				t.Fatal("later preflight error erased an unresolved Claim")
			}
			core.claimError = nil
			s := drain(t, d)
			if !s.ClaimEnded || s.Attempts != 0 || core.releases != 1 {
				t.Fatalf("replayed Claim not cleaned up: %+v", s)
			}
			for i := range core.tokens {
				if core.tokens[i] != core.tokens[0] || core.claimOperations[i] != core.claimOperations[0] {
					t.Fatal("recovery changed operation or token")
				}
			}
		})
	}
}

func TestOnlyCoreClaimConflictIsDefinitive(t *testing.T) {
	for _, api := range []*APIError{{Status: 401, Code: "unauthenticated"}, {Status: 403, Code: "forbidden"}, {Status: 408}, {Status: 409}, {Status: 429}, {Status: 500}, {Status: 409, Code: "conflict"}} {
		err := claimMutationError(api)
		var attempt *ClaimAttemptError
		got := errors.As(err, &attempt) && attempt.State == ClaimRejected
		if want := api.Status == 409 && api.Code == "conflict"; got != want {
			t.Fatalf("%+v definitive=%t", api, got)
		}
	}
}
