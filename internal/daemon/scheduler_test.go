package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

type schedulerTestCore struct {
	*fakeCore
	rows  []DiscoveredCandidate
	order []CandidateKind
}

func (c *schedulerTestCore) Discover(context.Context, []string, int, time.Duration) ([]DiscoveredCandidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]DiscoveredCandidate{}, c.rows...), nil
}
func (c *schedulerTestCore) Claim(ctx context.Context, row Candidate, op string, token Secret, lease int64) (Claim, error) {
	c.mu.Lock()
	if !c.claim.Active {
		c.claim = Claim{ID: fmt.Sprint("claim-", c.claimCalls), Executor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, LeaseSeconds: 15, Active: true}
	}
	c.order = append(c.order, row.Kind)
	c.mu.Unlock()
	return c.fakeCore.Claim(ctx, row, op, token, lease)
}

type schedulerTestAdapter struct {
	fakeAdapter
	mu        sync.Mutex
	unhealthy bool
}

func (a *schedulerTestAdapter) Probe(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.unhealthy {
		return errors.New("secret-provider-error")
	}
	return nil
}

func schedulerFixture(t *testing.T) (*schedulerTestCore, *schedulerTestAdapter, SchedulerOptions) {
	t.Helper()
	c := &schedulerTestCore{fakeCore: newFakeCore(), rows: []DiscoveredCandidate{{Candidate: Candidate{Kind: TaskCandidate, WorkItemID: "work", TaskID: "task", Mode: domain.CoordinationModeBlackboard}, Generation: "one"}}}
	a := &schedulerTestAdapter{}
	o := DefaultSchedulerOptions()
	o.WorkspaceRoot = t.TempDir()
	o.Dispatch = testOptions()
	o.DiscoveryInterval = time.Second
	o.ProbeInterval = 5 * time.Second
	o.Cooldown = 3 * time.Second
	o.ShutdownTimeout = 2 * time.Second
	return c, a, o
}

func runSchedulerTest(t *testing.T, s *Scheduler) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Run(ctx) }()
	t.Cleanup(func() { cancel() })
	return func() { cancel(); <-done }
}

func TestSchedulerBoundsFailedAndAbandonedClaims(t *testing.T) {
	for _, kind := range []string{"malformed", "overflow", "abandoned"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, a, o := schedulerFixture(t)
				a.observe = func(context.Context, RunRef) (RunObservation, error) {
					switch kind {
					case "overflow":
						return RunObservation{State: RuntimeFailed}, nil
					case "abandoned":
						return RunObservation{State: OutcomeReady, Outcome: &HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}}, nil
					default:
						return RunObservation{State: OutcomeReady, Outcome: &HarnessOutcome{}}, nil
					}
				}
				s, err := NewScheduler(c, a, o)
				if err != nil {
					t.Fatal(err)
				}
				stop := runSchedulerTest(t, s)
				time.Sleep(time.Hour)
				want := uint64(o.MaxDispatches)
				if kind == "abandoned" {
					want = 1
				}
				if stats := s.Stats(); stats.Claims != want || stats.Active != 0 || stats.Suppressed == 0 {
					t.Fatalf("stats: %+v", stats)
				}
				// Repeated successful probes cannot replenish an exhausted budget.
				c.mu.Lock()
				c.rows[0].Generation = "two"
				c.mu.Unlock()
				time.Sleep(time.Minute)
				if s.Stats().Claims != 2*want {
					t.Fatalf("new generation did not reset: %+v", s.Stats())
				}
				stop()
			})
		})
	}
}

func TestSchedulerProbeRecoveryAndSecretFreeLogs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		a.unhealthy = true
		var logs bytes.Buffer
		o.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
		a.observe = func(context.Context, RunRef) (RunObservation, error) {
			return RunObservation{State: OutcomeReady, Outcome: &HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}}, nil
		}
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		time.Sleep(time.Minute)
		if stats := s.Stats(); stats.Claims != 0 || !stats.Paused || stats.ProbeFailures == 0 {
			t.Fatalf("bad pause: %+v", stats)
		}
		a.mu.Lock()
		a.unhealthy = false
		a.mu.Unlock()
		time.Sleep(2 * time.Minute)
		if s.Stats().Claims != 1 {
			t.Fatalf("bad recovery: %+v", s.Stats())
		}
		a.mu.Lock()
		a.unhealthy = true
		a.mu.Unlock()
		time.Sleep(time.Minute)
		a.mu.Lock()
		a.unhealthy = false
		a.mu.Unlock()
		time.Sleep(2 * time.Minute)
		if s.Stats().Claims != 1 || s.Stats().Paused {
			t.Fatalf("health recovery must preserve quarantine: %+v", s.Stats())
		}
		stop()
		if strings.Contains(logs.String(), "secret-provider-error") || strings.Contains(logs.String(), "krs_claim_") {
			t.Fatal("secrets in logs")
		}
	})
}

func TestSchedulerUnknownStopRetainsSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		time.Sleep(5 * time.Second)
		if stats := s.Stats(); stats.Claims != 1 || stats.Active != 1 {
			t.Fatalf("slots: %+v", stats)
		}
		c.mu.Lock()
		c.inspectError = errors.New("offline")
		c.releaseError = errors.New("offline")
		c.mu.Unlock()
		stop()
		if stats := s.Stats(); stats.Active != 1 || stats.Finished != 0 {
			t.Fatalf("unknown Claim released slot: %+v", stats)
		}
		if err := s.Run(context.Background()); !errors.Is(err, ErrSchedulerAlreadyRun) {
			t.Fatalf("unresolved shutdown accepted another Run: %v", err)
		}
		if s.Stats().Active != 1 {
			t.Fatal("rejected Run cleared unresolved slot")
		}
	})
}

func TestSchedulerUnknownAcquisitionRetainsSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		c.claimError = errors.New("unknown mutation result")
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		time.Sleep(10 * time.Second)
		stop()
		if stats := s.Stats(); stats.Active != 1 || stats.Claims != 0 {
			t.Fatalf("unknown acquisition lost reservation: %+v", stats)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, op := range c.claimOperations {
			if op != c.claimOperations[0] {
				t.Fatal("unknown Claim replaced with a new operation")
			}
		}
	})
}

func TestSchedulerSystemFailurePausesWithoutRefreshingBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		a.start = func(context.Context, StartRequest) (RunRef, error) {
			return RunRef{}, &SystemError{Err: errors.New("provider outage")}
		}
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		synctest.Wait()
		if stats := s.Stats(); !stats.Paused || stats.Claims != 1 {
			t.Fatalf("not paused: %+v", stats)
		}
		time.Sleep(time.Hour)
		if stats := s.Stats(); stats.Claims != uint64(o.MaxDispatches) {
			t.Fatalf("healthy probes reset failing runtime budget: %+v", stats)
		}
		stop()
	})
}

func TestSchedulerRotationAndConflict(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		for _, kind := range []CandidateKind{EmptyBlackboard, WorkItemAcceptance, BlackboardCompletion} {
			c.rows = append(c.rows, DiscoveredCandidate{Candidate: Candidate{Kind: kind, WorkItemID: "work", Mode: domain.CoordinationModeBlackboard}, Generation: "one"})
		}
		a.start = func(_ context.Context, r StartRequest) (RunRef, error) {
			return RunRef{ID: string(r.Candidate.Kind)}, nil
		}
		a.observe = func(_ context.Context, r RunRef) (RunObservation, error) {
			outcome := &HarnessOutcome{Coordination: &CoordinationDecision{Kind: Abandoned}}
			if r.ID == string(TaskCandidate) {
				outcome = &HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}
			}
			return RunObservation{State: OutcomeReady, Outcome: outcome}, nil
		}
		c.claimError = &ClaimAttemptError{State: ClaimRejected, Err: &APIError{Status: 409}}
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		time.Sleep(3 * time.Second)
		c.mu.Lock()
		for _, kind := range c.order {
			if kind != WorkItemAcceptance {
				t.Fatalf("conflict advanced cursor: %v", c.order)
			}
		}
		c.claimError = nil
		c.order = nil
		c.mu.Unlock()
		time.Sleep(time.Minute)
		stop()
		c.mu.Lock()
		defer c.mu.Unlock()
		if fmt.Sprint(c.order) != fmt.Sprint(candidateOrder[:]) {
			t.Fatalf("order: %v", c.order)
		}
	})
}

func TestGenerationExcludesClaimsButIncludesBusinessChanges(t *testing.T) {
	c := Candidate{Kind: TaskCandidate, WorkItemID: "work", TaskID: "task", Mode: domain.CoordinationModeBlackboard}
	view := workContext{WorkItem: domain.WorkItem{ID: "work"}, Tasks: []domain.Task{{ID: "task", Status: domain.TaskStatusPending}}}
	initial, err := candidateGeneration(c, view)
	if err != nil {
		t.Fatal(err)
	}
	claim := domain.ClaimID("claim")
	view.Tasks[0].Status = domain.TaskStatusWorking
	view.Tasks[0].ActiveClaimID = &claim
	view.Tasks[0].UpdatedAt = time.Now()
	view.Tasks[0].Version++
	view.WorkItem.Version++
	view.WorkItem.UpdatedAt = time.Now()
	view.Claims = []domain.Claim{{ID: claim}}
	got, err := candidateGeneration(c, view)
	if err != nil || got != initial {
		t.Fatalf("churn changed generation: %s %v", got, err)
	}
	view.Tasks[0].Failures = []domain.TaskFailure{{RetryPrompt: "new context"}}
	got, err = candidateGeneration(c, view)
	if err != nil || got == initial {
		t.Fatal("Retry Prompt did not change generation")
	}
	view.Tasks[0].Failures = nil
	view.WorkItem.Result = "new result"
	got, err = candidateGeneration(c, view)
	if err != nil || got == initial {
		t.Fatal("business result did not change generation")
	}
}

func TestHTTPDiscoveryGenerationAcrossRealClaimChurn(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard, BlackboardCompletion, WorkItemAcceptance} {
		t.Run(string(kind), func(t *testing.T) {
			f := newHTTPFixture(t, domain.CoordinationModeBlackboard, kind)
			// This test exercises discovery, not the separate response-loss suite.
			f.transport.claimLost = true
			f.transport.outcomeLost = true
			ctx := context.Background()
			before, err := f.client.Discover(ctx, nil, 50, time.Second)
			if err != nil || len(before) != 1 {
				t.Fatalf("discover: %v %v", before, err)
			}
			op, _ := randomID()
			secret, _ := randomID()
			claim, err := f.client.Claim(ctx, f.candidate, op, NewSecret("krs_claim_"+secret), 15)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.client.Heartbeat(ctx, f.candidate, claim.ID, 15); err != nil {
				t.Fatal(err)
			}
			if err = f.client.Release(ctx, f.candidate, claim.ID, "test"); err != nil {
				t.Fatal(err)
			}
			after, err := f.client.Discover(ctx, nil, 50, time.Second)
			if err != nil || len(after) != 1 {
				t.Fatalf("discover after release: %v %v", after, err)
			}
			if before[0].Generation != after[0].Generation {
				t.Fatal("SQL Claim churn changed candidate generation")
			}
			op, _ = randomID()
			secret, _ = randomID()
			if _, err = f.client.Claim(ctx, f.candidate, op, NewSecret("krs_claim_"+secret), 15); err != nil {
				t.Fatal(err)
			}
			reaper, err := application.NewService(f.repo, &manualClock{now: time.Now().Add(time.Minute)}, f.ids)
			if err != nil {
				t.Fatal(err)
			}
			if err = reaper.ReapExpiredClaims(ctx); err != nil {
				t.Fatal(err)
			}
			after, err = f.client.Discover(ctx, nil, 50, time.Second)
			if err != nil || len(after) != 1 || after[0].Generation != before[0].Generation {
				t.Fatalf("reaper changed generation: %v %v", after, err)
			}
			if kind == TaskCandidate {
				op, _ = randomID()
				secret, _ = randomID()
				claim, err = f.client.Claim(ctx, f.candidate, op, NewSecret("krs_claim_"+secret), 15)
				if err != nil {
					t.Fatal(err)
				}
				op, _ = randomID()
				if err = f.client.Apply(ctx, f.candidate, claim.ID, op, HarnessOutcome{Task: &TaskOutcome{Kind: RetryableFailure, Reason: "business retry", RetryPrompt: "changed execution context"}}); err != nil {
					t.Fatal(err)
				}
				after, err = f.client.Discover(ctx, nil, 50, time.Second)
				if err != nil || len(after) != 1 || after[0].Generation == before[0].Generation {
					t.Fatalf("business retry did not change generation: %v %v", after, err)
				}
			}
		})
	}
}

func TestSchedulersShareCoreWithoutDuplicateRuns(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	// Use independent clients without the deliberately stateful loss transport.
	client, err := NewHTTPClient(f.client.base, f.client.token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.CreateBlackboardTask(context.Background(), application.CreateBlackboardTaskCommand{
		WorkItemID: f.candidate.WorkItemID, Title: "Second task", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"},
		Identity: identity.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan StartRequest, 8)
	var mu sync.Mutex
	stopped := make(map[string]bool)
	a := &fakeAdapter{
		start: func(_ context.Context, r StartRequest) (RunRef, error) {
			started <- r
			return RunRef{ID: r.ClaimID}, nil
		},
		observe: func(_ context.Context, r RunRef) (RunObservation, error) {
			mu.Lock()
			defer mu.Unlock()
			if stopped[r.ID] {
				return RunObservation{State: RunStopped}, nil
			}
			return RunObservation{State: RunRunning}, nil
		},
		stop: func(_ context.Context, r RunRef, _ StopReason) error {
			mu.Lock()
			defer mu.Unlock()
			stopped[r.ID] = true
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 2)
	schedulers := make([]*Scheduler, 0, 2)
	for range 2 {
		o := DefaultSchedulerOptions()
		o.WorkspaceRoot = t.TempDir()
		o.Slots = 2
		o.DiscoveryInterval = 10 * time.Millisecond
		o.Dispatch = testOptions()
		o.Dispatch.PollInterval = 10 * time.Millisecond
		s, err := NewScheduler(client, a, o)
		if err != nil {
			t.Fatal(err)
		}
		schedulers = append(schedulers, s)
		go func() { done <- s.Run(ctx) }()
	}
	runs := make([]StartRequest, 0, 2)
	for len(runs) < 2 {
		select {
		case r := <-started:
			runs = append(runs, r)
		case <-ctx.Done():
			t.Fatal("scheduler did not start both Tasks")
		}
	}
	if runs[0].Candidate.TaskID == runs[1].Candidate.TaskID || runs[0].Workspace == runs[1].Workspace {
		t.Fatal("duplicate Task execution or reused workspace")
	}
	// Let both schedulers observe the occupied Core candidates repeatedly.
	select {
	case r := <-started:
		t.Fatalf("unexpected additional run: %s", r.ClaimID)
	case <-time.After(100 * time.Millisecond):
	}
	if schedulers[0].Stats().Claims+schedulers[1].Stats().Claims != 2 {
		t.Fatal("unexpected successful Claim count")
	}
	cancel()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	view, err := client.context(context.Background(), f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Claims) != 2 {
		t.Fatalf("Claim history: %d", len(view.Claims))
	}
	for _, claim := range view.Claims {
		if claim.EndedAt == nil {
			t.Fatal("shutdown did not release Claim")
		}
	}
}
