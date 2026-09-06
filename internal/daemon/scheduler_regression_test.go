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

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestSchedulerRegressionHealthyDiscoveryDoesNotStarve(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	view, err := f.client.context(context.Background(), f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	creator := f.agent
	creator.Actor = domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}
	creator.Role = ""
	for range 2 {
		work, err := f.service.CreateWorkItem(context.Background(), application.CreateWorkItemCommand{
			Identity: creator, Definition: view.WorkItem.Definition, Title: "More work", Goal: "Healthy backlog", AcceptanceMode: domain.WorkItemAcceptanceAgent,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.service.CreateBlackboardTask(context.Background(), application.CreateBlackboardTaskCommand{
			Identity: creator, WorkItemID: work.ID, Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Real HTTP/SQL progress uses normal deadlines; precise timeout semantics
	// are tested separately with virtual time, without filesystem/scheduling noise.
	core, err := NewHTTPClient(f.client.base, f.client.token, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := DefaultSchedulerOptions()
	o.Dispatch = testOptions()
	o.WorkspaceRoot = t.TempDir()
	s, err := NewScheduler(core, &fakeAdapter{}, o)
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	for range 3 {
		s.admit(context.Background(), func(*scheduledRun) { launches++ })
	}
	if launches == 0 || s.Stats().Claims == 0 {
		t.Fatalf("healthy HTTP/SQL discovery made no progress: stats=%+v", s.Stats())
	}
}

func TestHTTPDiscoveryPreservesResolvedCandidates(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	view, err := f.client.context(context.Background(), f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	creator := f.agent
	creator.Actor = domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}
	creator.Role = ""
	work, err := f.service.CreateWorkItem(context.Background(), application.CreateWorkItemCommand{
		Identity: creator, Definition: view.WorkItem.Definition, Title: "Second work", Goal: "Partial discovery", AcceptanceMode: domain.WorkItemAcceptanceAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateBlackboardTask(context.Background(), application.CreateBlackboardTaskCommand{
		Identity: creator, WorkItemID: work.ID, Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"},
	}); err != nil {
		t.Fatal(err)
	}
	core, err := NewHTTPClient(f.client.base, f.client.token, &http.Client{Transport: &claimPreflightFailureTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := core.Discover(context.Background(), nil, 50, time.Second)
	if err == nil || len(rows) != 1 || rows[0].Generation == "" {
		t.Fatalf("partial discovery: rows=%v err=%v", rows, err)
	}
}

func TestSchedulerRegressionNotSentClaimRetryHonorsUnhealthyProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, a, o := schedulerFixture(t)
		o.Slots = 1 // A retained acquisition must still probe when all slots are full.
		c.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: errors.New("context request unavailable before POST")}
		s, err := NewScheduler(c, a, o)
		if err != nil {
			t.Fatal(err)
		}
		stop := runSchedulerTest(t, s)
		synctest.Wait()
		a.mu.Lock()
		a.unhealthy = true
		a.mu.Unlock()
		time.Sleep(8 * time.Second)
		if stats := s.Stats(); !stats.Paused || stats.Claims != 0 {
			t.Fatalf("bad precondition: %+v", stats)
		}
		c.mu.Lock()
		c.claimError = nil
		c.mu.Unlock()
		time.Sleep(2 * time.Second)
		stats := s.Stats()
		stop()
		if stats.Claims != 0 {
			t.Fatalf("created a new Claim and started Harness after failing Probe: %+v", stats)
		}
	})
}

func TestSchedulerRegressionStopSystemFailurePausesAdmissions(t *testing.T) {
	c, a, o := schedulerFixture(t)
	a.stop = func(context.Context, RunRef, StopReason) error {
		return &SystemError{Err: errors.New("control service unavailable")}
	}
	s, err := NewScheduler(c, a, o)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &schedulerAdapter{Adapter: a, scheduler: s}
	err = wrapped.Stop(context.Background(), RunRef{ID: "run"}, StopRequested)
	var system *SystemError
	if !errors.As(err, &system) {
		t.Fatal("missing system failure precondition")
	}
	if !s.Stats().Paused {
		t.Fatal("Stop returned SystemError without pausing admissions")
	}
}

type claimPreflightFailureTransport struct{ contexts atomic.Int64 }

func (r *claimPreflightFailureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/context") && r.contexts.Add(1) == 2 {
		return nil, errors.New("preflight network failure before claim POST")
	}
	return http.DefaultTransport.RoundTrip(req)
}
func TestSchedulerRegressionNotSentClaimPauseWithRealSQL(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	core, err := NewHTTPClient(f.client.base, f.client.token, &http.Client{Transport: &claimPreflightFailureTransport{}})
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
		t.Fatal("missing unclaimed prepared Dispatch")
	}
	before, err := core.context(context.Background(), f.candidate)
	if err != nil || len(before.Claims) != 0 {
		t.Fatalf("preflight created Claim: %v %+v", err, before.Claims)
	}
	a.unhealthy = true
	if s.probe(context.Background(), true) {
		t.Fatal("Probe unexpectedly healthy")
	}
	if err = run.dispatch.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := core.context(context.Background(), f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Claims) != 0 {
		t.Fatalf("created %d persisted SQL Claim while paused=%v", len(after.Claims), s.Stats().Paused)
	}
}
