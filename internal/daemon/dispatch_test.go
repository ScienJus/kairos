package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

type manualClock struct{ now time.Time }

func (c *manualClock) Now() time.Time                         { return c.now }
func (c *manualClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type fakeAdapter struct {
	start   func(context.Context, StartRequest) (RunRef, error)
	observe func(context.Context, RunRef) (RunObservation, error)
	stop    func(context.Context, RunRef, StopReason) error
}

func (*fakeAdapter) Probe(context.Context) error { return nil }
func (a *fakeAdapter) Start(ctx context.Context, r StartRequest) (RunRef, error) {
	if a.start != nil {
		return a.start(ctx, r)
	}
	return RunRef{ID: "run"}, nil
}
func (a *fakeAdapter) Observe(ctx context.Context, r RunRef) (RunObservation, error) {
	if a.observe != nil {
		return a.observe(ctx, r)
	}
	return RunObservation{State: RunRunning}, nil
}
func (a *fakeAdapter) Stop(ctx context.Context, r RunRef, reason StopReason) error {
	if a.stop != nil {
		return a.stop(ctx, r, reason)
	}
	return nil
}

type fakeCore struct {
	mu                                                                 sync.Mutex
	claim                                                              Claim
	status                                                             ClaimStatus
	claimCalls, heartbeats, applies, releases                          int
	claimOperations, tokens, outcomeOperations                         []string
	claimError, heartbeatError, inspectError, applyError, releaseError error
	dropClaim, dropApply, dropRelease                                  bool
	lastOutcome                                                        HarnessOutcome
}

func newFakeCore() *fakeCore {
	claim := Claim{ID: "claim", Executor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, LeaseSeconds: 15, Active: true}
	return &fakeCore{claim: claim, status: ClaimStatus{Claim: claim, Task: &domain.Task{ID: "task", WorkItemID: "work"}}}
}
func (f *fakeCore) Claim(_ context.Context, _ Candidate, op string, token Secret, _ int64) (Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls++
	f.claimOperations = append(f.claimOperations, op)
	f.tokens = append(f.tokens, token.Reveal())
	if f.claimError != nil {
		return Claim{}, f.claimError
	}
	if f.dropClaim {
		f.dropClaim = false
		return Claim{}, errors.New("lost response")
	}
	return f.claim, nil
}
func (f *fakeCore) Heartbeat(context.Context, Candidate, string, int64) (Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
	if f.heartbeatError != nil {
		return Claim{}, f.heartbeatError
	}
	if !f.claim.Active {
		return Claim{}, &APIError{Status: 409}
	}
	return f.claim, nil
}
func (f *fakeCore) Inspect(context.Context, Candidate, string) (ClaimStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inspectError != nil {
		return ClaimStatus{}, f.inspectError
	}
	f.status.Claim = f.claim
	data, _ := json.Marshal(f.status)
	var copy ClaimStatus
	_ = json.Unmarshal(data, &copy)
	return copy, nil
}
func (f *fakeCore) Apply(_ context.Context, c Candidate, id, op string, o HarnessOutcome) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applies++
	f.outcomeOperations = append(f.outcomeOperations, op)
	if f.applyError != nil {
		return f.applyError
	}
	f.lastOutcome = o
	f.claim.Active = false
	now := time.Now()
	f.claim.EndedAt = &now
	if c.Kind != TaskCandidate {
		f.claim.EndReason = map[OutcomeKind]string{CreateTask: "task_created", SubmitCompletion: "completion_submitted", AcceptCompletion: "completion_accepted", Abandoned: "released"}[o.Kind()]
		if o.Kind() == CreateTask {
			spec := o.Coordination.Task
			f.status.Tasks = []domain.Task{{ID: "created", Title: spec.Title, Executor: spec.Executor, AllowedRoles: spec.AllowedRoles, Tags: spec.Tags, CreatedAt: now}}
		}
		if o.Kind() == SubmitCompletion {
			f.status.WorkItemResult = o.Coordination.Result
		}
	} else {
		switch o.Kind() {
		case Completed:
			f.claim.EndReason = "task_completed"
			if o.Task.RequestReview {
				f.claim.EndReason = "submitted_for_review"
			}
			f.status.Task.Submissions = append(f.status.Task.Submissions, domain.TaskSubmission{ID: "submission", ClaimID: domain.ClaimID(id), Result: o.Task.Result})
		case RetryableFailure, TerminalFailure:
			f.claim.EndReason = "task_failed"
			f.status.Task.Failures = append(f.status.Task.Failures, domain.TaskFailure{ClaimID: domain.ClaimID(id), Action: failureAction(o.Kind()), Reason: o.Task.Reason, RetryPrompt: o.Task.RetryPrompt})
		case Decomposed:
			f.claim.EndReason = "task_decomposed"
			f.status.Task.DecomposedAt = &now
			for i, spec := range o.Task.Children {
				parentID := c.TaskID
				f.status.Tasks = append(f.status.Tasks, domain.Task{ID: domain.TaskID(fmt.Sprint(i)), ParentTaskID: &parentID, Title: spec.Title, Executor: spec.Executor, AllowedRoles: spec.AllowedRoles, Tags: spec.Tags, CreatedAt: now})
			}
		case Abandoned:
			f.claim.EndReason = "released"
		}
	}
	if f.dropApply {
		f.dropApply = false
		return errors.New("lost response")
	}
	return nil
}
func (f *fakeCore) Release(context.Context, Candidate, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	if f.releaseError != nil {
		return f.releaseError
	}
	f.claim.Active = false
	f.claim.EndReason = "released"
	if f.dropRelease {
		f.dropRelease = false
		return errors.New("lost response")
	}
	return nil
}

func testOptions() Options {
	o := DefaultOptions()
	o.Lease = 15 * time.Second
	o.RequestTimeout = 6 * time.Second
	o.StopTimeout = 5 * time.Second
	o.MCPURL = "http://localhost/mcp"
	return o
}
func taskCandidate() Candidate {
	return Candidate{Kind: TaskCandidate, WorkItemID: "work", TaskID: "task", Mode: domain.CoordinationModeBlackboard}
}
func completedOutcome() HarnessOutcome {
	return HarnessOutcome{Task: &TaskOutcome{Kind: Completed, Result: "done"}}
}
func outcomeAdapter(o HarnessOutcome) *fakeAdapter {
	return &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
		return RunObservation{State: OutcomeReady, Outcome: &o}, nil
	}}
}
func dispatchForTest(t *testing.T, core Core, adapter Adapter, c Candidate, o Options) *Dispatch {
	t.Helper()
	d, err := NewDispatch(core, adapter, c, o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func steps(t *testing.T, d *Dispatch, n int) {
	t.Helper()
	for range n {
		if err := d.Step(context.Background()); err != nil {
			t.Fatalf("Step: %v (%+v)", err, d.Snapshot())
		}
	}
}
func drain(t *testing.T, d *Dispatch) Snapshot {
	t.Helper()
	for range 20 {
		if d.Snapshot().Terminal() {
			return d.Snapshot()
		}
		_ = d.Step(context.Background())
	}
	t.Fatalf("Dispatch did not finish: %+v", d.Snapshot())
	return Snapshot{}
}

func TestOutcomeApplicability(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard, BlackboardCompletion, WorkItemAcceptance} {
		for _, mode := range []domain.CoordinationMode{domain.CoordinationModeBlackboard, domain.CoordinationModeWorkflow} {
			c := Candidate{Kind: kind, Mode: mode, WorkItemID: "work"}
			if kind == TaskCandidate {
				c.TaskID = "task"
			} else if mode == domain.CoordinationModeWorkflow {
				continue
			}
			for _, outcome := range []OutcomeKind{Completed, Decomposed, RetryableFailure, TerminalFailure, Abandoned, CreateTask, SubmitCompletion, AcceptCompletion} {
				t.Run(string(kind)+"/"+string(mode)+"/"+string(outcome), func(t *testing.T) {
					spec := TaskSpec{Title: "child", Executor: domain.ExecutorAgent}
					o := HarnessOutcome{}
					if kind == TaskCandidate {
						o.Task = &TaskOutcome{Kind: outcome}
						switch outcome {
						case Completed:
							o.Task.Result = "done"
						case Decomposed:
							o.Task.Children = []TaskSpec{spec}
						case RetryableFailure, TerminalFailure:
							o.Task.Reason = "business failure"
						}
					} else {
						o.Coordination = &CoordinationDecision{Kind: outcome}
						if outcome == CreateTask {
							o.Coordination.Task = &spec
						}
						if outcome == SubmitCompletion {
							o.Coordination.Result = "done"
						}
					}
					allowed := outcome == Abandoned
					if kind == TaskCandidate {
						allowed = allowed || outcome == Completed || outcome == RetryableFailure || outcome == TerminalFailure || (outcome == Decomposed && mode == domain.CoordinationModeBlackboard)
					} else {
						allowed = allowed || outcome == CreateTask || (outcome == SubmitCompletion && kind != WorkItemAcceptance) || (outcome == AcceptCompletion && kind == WorkItemAcceptance)
					}
					if err := o.Validate(c); (err == nil) != allowed {
						t.Fatalf("Validate=%v allowed=%v", err, allowed)
					}
					if allowed {
						core := newFakeCore()
						if mode == domain.CoordinationModeWorkflow {
							policy := domain.ReviewExecutorDecides
							core.status.Task.ReviewPolicy = &policy
						}
						core.dropApply = true
						d := dispatchForTest(t, core, outcomeAdapter(o), c, testOptions())
						s := drain(t, d)
						if !s.OutcomeApplied || core.applies != 1 || !s.ClaimEnded {
							t.Fatalf("reconcile=%+v applies=%d", s, core.applies)
						}
					}
				})
			}
		}
	}
}

func TestLostClaimResponseAndFrozenOutcome(t *testing.T) {
	core := newFakeCore()
	core.dropClaim = true
	o := completedOutcome()
	d := dispatchForTest(t, core, outcomeAdapter(o), taskCandidate(), testOptions())
	if err := d.Step(context.Background()); err == nil {
		t.Fatal("expected lost response")
	}
	if d.Snapshot().RunRef.ID != "" {
		t.Fatal("started without Claim response")
	}
	steps(t, d, 3)
	o.Task.Result = "changed after observation"
	s := drain(t, d)
	if !s.OutcomeApplied || core.lastOutcome.Task.Result != "done" || core.claimCalls != 2 || core.claimOperations[0] != core.claimOperations[1] || core.tokens[0] != core.tokens[1] {
		t.Fatalf("unstable retry or outcome: %+v", s)
	}
	if _, err := identity.ExecutorTokenHash(core.tokens[0]); err != nil {
		t.Fatal("invalid generated token")
	}
}

func TestRuntimeRetryAndInvalidOutputNeverFailWork(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard} {
		for _, failure := range []string{"start", "output", "runtime"} {
			t.Run(string(kind)+"/"+failure, func(t *testing.T) {
				core := newFakeCore()
				c := taskCandidate()
				c.Kind = kind
				if kind != TaskCandidate {
					c.TaskID = ""
				}
				a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
					if failure == "runtime" {
						return RunObservation{State: RuntimeFailed}, nil
					}
					return RunObservation{State: OutcomeReady}, nil
				}}
				if failure == "start" {
					a.start = func(context.Context, StartRequest) (RunRef, error) {
						return RunRef{}, errors.New("provider unavailable")
					}
				}
				d := dispatchForTest(t, core, a, c, testOptions())
				s := drain(t, d)
				if s.Attempts != 2 || core.applies != 0 || core.releases != 1 || !s.ClaimEnded || s.OutcomeApplied {
					t.Fatalf("runtime failure=%+v applies=%d releases=%d", s, core.applies, core.releases)
				}
			})
		}
	}
}

func TestLostRunWaitsForCoreAndDoesNotRestart(t *testing.T) {
	clock := &manualClock{now: time.Now()}
	o := testOptions()
	o.Clock = clock
	core := newFakeCore()
	a := &fakeAdapter{stop: func(context.Context, RunRef, StopReason) error { return errors.New("cannot stop") }}
	d := dispatchForTest(t, core, a, taskCandidate(), o)
	steps(t, d, 2)
	d.RequestStop(StopRequested)
	core.inspectError = errors.New("Core offline")
	_ = d.Step(context.Background())
	clock.now = clock.now.Add(6 * time.Second)
	_ = d.Step(context.Background())
	s := d.Snapshot()
	if s.State != Stopping || s.RunState != RunLost || s.Terminal() || s.Attempts != 1 || core.releases != 0 {
		t.Fatalf("premature terminal=%+v", s)
	}
	core.inspectError = nil
	core.dropRelease = true
	s = drain(t, d)
	if s.State != Lost || !s.ClaimEnded || s.Attempts != 1 || core.releases != 1 {
		t.Fatalf("lost reconcile=%+v", s)
	}
}

func TestAuthorityLossAndHeartbeatDeadline(t *testing.T) {
	for _, code := range []int{401, 403, 404, 409, 410, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			clock := &manualClock{now: time.Now()}
			o := testOptions()
			o.Clock = clock
			core := newFakeCore()
			a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) { return RunObservation{State: RunStopped}, nil }}
			d := dispatchForTest(t, core, a, taskCandidate(), o)
			steps(t, d, 2)
			core.heartbeatError = &APIError{Status: code}
			clock.now = clock.now.Add(3 * time.Second)
			_ = d.Heartbeat(context.Background())
			if code == 503 {
				if d.Snapshot().State != Running {
					t.Fatal("transient heartbeat stopped too early")
				}
				clock.now = clock.now.Add(11 * time.Second)
				_ = d.Heartbeat(context.Background())
			}
			if d.Snapshot().State != Stopping {
				t.Fatalf("not stopping: %+v", d.Snapshot())
			}
			drain(t, d)
			if core.applies != 0 || core.releases != 1 {
				t.Fatal("authority loss generated business outcome")
			}
		})
	}
}

func TestHeartbeatContinuesDuringStartAndFinalization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core := newFakeCore()
		a := outcomeAdapter(completedOutcome())
		a.start = func(ctx context.Context, r StartRequest) (RunRef, error) {
			time.Sleep(5 * time.Second)
			core.mu.Lock()
			n := core.heartbeats
			core.mu.Unlock()
			if n < 2 {
				t.Errorf("heartbeat blocked by Start: %d", n)
			}
			return RunRef{ID: "run"}, nil
		}
		d := dispatchForTest(t, core, a, taskCandidate(), testOptions())
		core.applyError = errors.New("Core temporarily offline")
		go func() { time.Sleep(12 * time.Second); core.mu.Lock(); core.applyError = nil; core.mu.Unlock() }()
		s, err := d.Run(context.Background())
		if err != nil || !s.OutcomeApplied {
			t.Fatalf("Run=%+v err=%v", s, err)
		}
		if core.heartbeats < 4 || core.applies < 2 {
			t.Fatalf("guard/retries heartbeats=%d applies=%d", core.heartbeats, core.applies)
		}
		for _, op := range core.outcomeOperations {
			if op != core.outcomeOperations[0] {
				t.Fatal("outcome operation changed")
			}
		}
	})
}

func TestCancellationRetainsUnresolvedDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core := newFakeCore()
		a := &fakeAdapter{}
		d := dispatchForTest(t, core, a, taskCandidate(), testOptions())
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(3 * time.Second)
			core.mu.Lock()
			core.inspectError = errors.New("offline")
			core.mu.Unlock()
			cancel()
		}()
		s, err := d.Run(ctx)
		if !errors.Is(err, context.Canceled) || s.Terminal() || s.State != Stopping || s.ClaimID == "" {
			t.Fatalf("cancel=%+v err=%v", s, err)
		}
		core.inspectError = nil
		time.Sleep(6 * time.Second)
		s, err = d.Run(context.Background())
		if err != nil || s.State != Lost || core.claimCalls != 1 {
			t.Fatalf("resume=%+v err=%v", s, err)
		}
	})
}

func TestStartContractViolationNeverRetries(t *testing.T) {
	for _, ref := range []RunRef{{}, {ID: "uncertain-run"}} {
		core := newFakeCore()
		a := &fakeAdapter{start: func(context.Context, StartRequest) (RunRef, error) {
			if ref.ID != "" {
				return ref, errors.New("ambiguous")
			}
			return ref, nil
		}}
		d := dispatchForTest(t, core, a, taskCandidate(), testOptions())
		s := drain(t, d)
		if s.State != Lost || s.Attempts != 1 || core.releases != 1 {
			t.Fatalf("contract violation=%+v", s)
		}
	}
}

func TestExecutorSecretRedaction(t *testing.T) {
	s := NewSecret("secret-value")
	r := StartRequest{ExecutorToken: s}
	data, _ := json.Marshal(r)
	for _, value := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r), string(data)} {
		if strings.Contains(value, s.Reveal()) {
			t.Fatal("secret leaked in formatted request")
		}
	}
}
