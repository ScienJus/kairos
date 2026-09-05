package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

type testIDs struct{ next atomic.Uint64 }

func (g *testIDs) NewID() string { return fmt.Sprintf("dispatch-%d", g.next.Add(1)) }

// lostResponseTransport lets real HTTP mutations commit before discarding their
// successful response. It does not mock application transactions or persistence.
type lostResponseTransport struct {
	base                     http.RoundTripper
	claimLost, outcomeLost   bool
	claimPosts, outcomePosts int
	identityToken            string
	t                        *testing.T
}

func (r *lostResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "Bearer "+r.identityToken {
		r.t.Error("Daemon lifecycle request did not use its Identity Token")
	}
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost && req.Method != http.MethodDelete {
		return resp, nil
	}
	if strings.HasSuffix(req.URL.Path, "/heartbeat") {
		return resp, nil
	}
	isClaim := req.Method == http.MethodPost && (strings.HasSuffix(req.URL.Path, "/claims") || strings.HasSuffix(req.URL.Path, "/coordination-claims"))
	if isClaim {
		r.claimPosts++
	} else {
		r.outcomePosts++
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if isClaim && !r.claimLost {
			r.claimLost = true
			resp.Body.Close()
			return nil, errors.New("lost Claim response")
		}
		if !isClaim && !r.outcomeLost {
			r.outcomeLost = true
			resp.Body.Close()
			return nil, errors.New("lost outcome response")
		}
	}
	return resp, nil
}

type httpFixture struct {
	repo       *repository.SQLRepository
	ids        *testIDs
	service    *application.Service
	identities *identity.Service
	agent      identity.Identity
	candidate  Candidate
	client     *HTTPClient
	transport  *lostResponseTransport
}

func newHTTPFixture(t *testing.T, mode domain.CoordinationMode, kind CandidateKind, graphs ...domain.WorkflowGraph) *httpFixture {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "daemon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	ids := &testIDs{}
	service, err := application.NewService(repo, realClock{}, ids)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := identity.NewService(repo, realClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		t.Fatal(err)
	}
	actor := domain.ActorRef{Kind: domain.ActorAgent, ID: "daemon-agent"}
	issued, err := identities.CreateIdentity(ctx, actor, "backend")
	if err != nil {
		t.Fatal(err)
	}
	agent := identity.Identity{Actor: actor, Role: "backend"}
	human := identity.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}}
	var binding domain.DefinitionBinding
	if mode == domain.CoordinationModeBlackboard {
		def, err := service.CreateBlackboardDefinition(ctx, application.CreateBlackboardDefinitionCommand{Identity: human, Metadata: application.DefinitionMetadataCommand{ID: "dispatch", Name: "Dispatch"}})
		if err != nil {
			t.Fatal(err)
		}
		binding = def.Binding()
	} else {
		graph := domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"execute"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "execute", Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewExecutorDecides}}}
		if len(graphs) > 0 {
			graph = graphs[0]
		}
		def, err := service.CreateWorkflowDefinition(ctx, application.CreateWorkflowDefinitionCommand{Identity: human, Metadata: application.DefinitionMetadataCommand{ID: "dispatch", Name: "Dispatch"}, Graph: graph})
		if err != nil {
			t.Fatal(err)
		}
		binding = def.Binding()
	}
	acceptance := domain.WorkItemAcceptanceNone
	if mode == domain.CoordinationModeBlackboard {
		acceptance = domain.WorkItemAcceptanceAgent
	}
	work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: binding, Identity: human, Title: "Dispatch", Goal: "Exercise one Dispatch", AcceptanceMode: acceptance})
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{Kind: kind, Mode: mode, WorkItemID: work.ID}
	if mode == domain.CoordinationModeWorkflow {
		view, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: agent})
		if err != nil {
			t.Fatal(err)
		}
		candidate.TaskID = view.Tasks[0].ID
	} else if kind != EmptyBlackboard {
		task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: work.ID, Identity: human, Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}})
		if err != nil {
			t.Fatal(err)
		}
		if kind == TaskCandidate {
			candidate.TaskID = task.ID
		} else {
			claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "precondition done"})
			if err != nil {
				t.Fatal(err)
			}
			if kind == WorkItemAcceptance {
				claim, err := service.ClaimWorkCandidate(ctx, application.ClaimWorkCandidateCommand{WorkItemID: work.ID, Kind: application.WorkCandidateBlackboardCompletion, Identity: agent})
				if err != nil {
					t.Fatal(err)
				}
				_, err = service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{WorkItemID: work.ID, CoordinationClaimID: claim.ID, Identity: agent, Result: "accept this"})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	handler, err := httpapi.New(service, identity.AuthenticatedResolver{Authenticator: identities}, httpapi.Options{AuthenticationMode: httpapi.AuthenticationModeAuthenticated, MaxArtifactUploadBytes: httpapi.DefaultMaxArtifactUploadBytes})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport := &lostResponseTransport{base: server.Client().Transport, identityToken: issued.Token, t: t}
	client, err := NewHTTPClient(server.URL, NewSecret(issued.Token), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return &httpFixture{repo: repo, ids: ids, service: service, identities: identities, agent: agent, candidate: candidate, client: client, transport: transport}
}

func TestHTTPDispatchOutcomesAndLostResponses(t *testing.T) {
	spec := TaskSpec{Title: "Follow-up", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Tags: []string{}}
	cases := []struct {
		kind    CandidateKind
		mode    domain.CoordinationMode
		outcome HarnessOutcome
	}{
		{TaskCandidate, domain.CoordinationModeBlackboard, completedOutcome()},
		{TaskCandidate, domain.CoordinationModeBlackboard, HarnessOutcome{Task: &TaskOutcome{Kind: Completed, Result: "review", RequestReview: true}}},
		{TaskCandidate, domain.CoordinationModeBlackboard, HarnessOutcome{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{spec}}}},
		{TaskCandidate, domain.CoordinationModeBlackboard, HarnessOutcome{Task: &TaskOutcome{Kind: RetryableFailure, Reason: "business blocker", RetryPrompt: "try this"}}},
		{TaskCandidate, domain.CoordinationModeBlackboard, HarnessOutcome{Task: &TaskOutcome{Kind: TerminalFailure, Reason: "business impossible"}}},
		{TaskCandidate, domain.CoordinationModeBlackboard, HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned, Reason: "not suitable"}}},
		{TaskCandidate, domain.CoordinationModeWorkflow, completedOutcome()},
		{TaskCandidate, domain.CoordinationModeWorkflow, HarnessOutcome{Task: &TaskOutcome{Kind: Completed, Result: "review", RequestReview: true}}},
		{TaskCandidate, domain.CoordinationModeWorkflow, HarnessOutcome{Task: &TaskOutcome{Kind: RetryableFailure, Reason: "business blocker", RetryPrompt: "try this"}}},
		{TaskCandidate, domain.CoordinationModeWorkflow, HarnessOutcome{Task: &TaskOutcome{Kind: TerminalFailure, Reason: "business impossible"}}},
		{TaskCandidate, domain.CoordinationModeWorkflow, HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}},
	}
	for _, kind := range []CandidateKind{EmptyBlackboard, BlackboardCompletion, WorkItemAcceptance} {
		for _, outcome := range []OutcomeKind{CreateTask, Abandoned, SubmitCompletion, AcceptCompletion} {
			o := HarnessOutcome{Coordination: &CoordinationDecision{Kind: outcome}}
			if outcome == CreateTask {
				o.Coordination.Task = &spec
			}
			if outcome == SubmitCompletion {
				o.Coordination.Result = "finished"
			}
			candidate := Candidate{Kind: kind, WorkItemID: "work", Mode: domain.CoordinationModeBlackboard}
			if o.Validate(candidate) == nil {
				cases = append(cases, struct {
					kind    CandidateKind
					mode    domain.CoordinationMode
					outcome HarnessOutcome
				}{kind, domain.CoordinationModeBlackboard, o})
			}
		}
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%02d/%s/%s/%s", i, tc.kind, tc.mode, tc.outcome.Kind()), func(t *testing.T) {
			f := newHTTPFixture(t, tc.mode, tc.kind)
			a := outcomeAdapter(tc.outcome)
			a.start = func(ctx context.Context, r StartRequest) (RunRef, error) {
				principal, err := f.service.Authenticate(ctx, r.ExecutorToken.Reveal())
				if err != nil {
					t.Fatalf("injected credential: %v", err)
				}
				if principal.Executor == nil || principal.Executor.ClaimID != r.ClaimID || principal.Executor.WorkItemID != f.candidate.WorkItemID || principal.Role != "" {
					t.Fatal("wrong executor principal")
				}
				if r.ExecutorToken.Reveal() == f.transport.identityToken {
					t.Fatal("Harness received Identity Token")
				}
				return RunRef{ID: "run"}, nil
			}
			d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
			s := drain(t, d)
			if !s.OutcomeApplied || !s.ClaimEnded || s.Attempts != 1 || f.transport.claimPosts != 2 || f.transport.outcomePosts != 1 {
				t.Fatalf("HTTP Dispatch=%+v claimPosts=%d outcomePosts=%d", s, f.transport.claimPosts, f.transport.outcomePosts)
			}
			if _, err := f.service.Authenticate(context.Background(), d.executorToken.Reveal()); !errors.Is(err, identity.ErrUnauthenticated) {
				t.Fatalf("ended credential error=%v", err)
			}
			view, err := f.service.GetWorkItemExecutionContext(context.Background(), application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
			if err != nil {
				t.Fatal(err)
			}
			claimCount := 0
			for _, c := range view.Claims {
				if c.ExecutorTokenHash != "" {
					claimCount++
				}
			}
			for _, c := range view.CoordinationClaims {
				if c.ExecutorTokenHash != "" {
					claimCount++
				}
			}
			if claimCount != 1 {
				t.Fatalf("created %d Executor Claims", claimCount)
			}
			if tc.outcome.Kind() == CreateTask {
				expected := 2
				if tc.kind == EmptyBlackboard {
					expected = 1
				}
				if len(view.Tasks) != expected {
					t.Fatalf("duplicate planning tasks: %d", len(view.Tasks))
				}
			}
			if tc.outcome.Kind() == Decomposed && len(view.Tasks) != 2 {
				t.Fatal("duplicate child tasks")
			}
		})
	}
}

func TestHTTPDispatchCancellationAndRuntimeRelease(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard} {
		for _, scenario := range []string{"runtime", "cancel", "reaper"} {
			t.Run(string(kind)+"/"+scenario, func(t *testing.T) {
				f := newHTTPFixture(t, domain.CoordinationModeBlackboard, kind)
				f.transport.claimLost = true
				f.transport.outcomeLost = true
				a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) { return RunObservation{State: RunStopped}, nil }}
				d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
				steps(t, d, 2)
				if scenario == "cancel" {
					_, err := f.service.CancelWorkItem(context.Background(), application.CancelWorkItemCommand{WorkItemID: f.candidate.WorkItemID, Identity: identity.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}, Reason: "cancelled"})
					if err != nil {
						t.Fatal(err)
					}
				} else if scenario == "reaper" {
					// Advance only the Core clock; no wall-clock sleep is needed.
					reaper, err := application.NewService(f.repo, &manualClock{now: time.Now().Add(time.Minute)}, f.ids)
					if err != nil {
						t.Fatal(err)
					}
					if err := reaper.ReapExpiredClaims(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				s := drain(t, d)
				if !s.ClaimEnded || s.OutcomeApplied {
					t.Fatalf("stop=%+v", s)
				}
				view, err := f.service.GetWorkItemExecutionContext(context.Background(), application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range view.Tasks {
					if len(task.Failures) != 0 || len(task.Submissions) != 0 {
						t.Fatal("runtime stop created business history")
					}
				}
			})
		}
	}
}

func TestHTTPClientRejectsRedirectsAndInvalidURLs(t *testing.T) {
	for _, base := range []string{"file:///tmp/core", "http://user:secret@example.test", "http://example.test?secret=x", "http://example.test#fragment"} {
		if _, err := NewHTTPClient(base, NewSecret("token"), nil); err == nil {
			t.Errorf("accepted URL %q", base)
		}
	}
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, err := NewHTTPClient(source.URL, NewSecret("token"), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Release(context.Background(), taskCandidate(), "claim", "")
	if !statusIs(err, 307) || leaked.Load() {
		t.Fatalf("redirect followed: err=%v leaked=%v", err, leaked.Load())
	}
}

func TestHTTPWorkflowTransitionAndArtifactReconciliation(t *testing.T) {
	graph := domain.WorkflowGraph{
		StartTaskIDs: []domain.WorkflowTaskID{"execute"},
		Tasks: []domain.WorkflowTaskDefinition{
			{ID: "execute", Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone, Artifacts: []domain.ArtifactDefinition{{Name: "result", Description: "Deliver the result"}}},
			{ID: "next", Title: "Next", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
		},
		Relations: []domain.WorkflowRelationDefinition{{ID: "next", FromTaskID: "execute", ToTaskID: "next"}},
	}
	f := newHTTPFixture(t, domain.CoordinationModeWorkflow, TaskCandidate, graph)
	o := HarnessOutcome{Task: &TaskOutcome{Kind: Completed, Result: "  done\n"}}
	a := outcomeAdapter(o)
	a.start = func(ctx context.Context, r StartRequest) (RunRef, error) {
		principal, err := f.service.Authenticate(ctx, r.ExecutorToken.Reveal())
		if err != nil {
			t.Fatal(err)
		}
		view, err := f.service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: r.Candidate.TaskID, Identity: principal})
		if err != nil {
			t.Fatal(err)
		}
		if view.Workflow == nil || len(view.Workflow.ChoiceGroups) != 1 {
			t.Fatal("unexpected Workflow choices")
		}
		o.Task.Transition = &Transition{ChoiceGroupID: view.Workflow.ChoiceGroups[0].ID, Reason: "  continue\n"}
		artifact, err := f.service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: r.Candidate.TaskID, ClaimID: domain.ClaimID(r.ClaimID), Identity: principal, Name: "result", URI: "https://example.test/result", OperationID: "harness-artifact"})
		if err != nil {
			t.Fatal(err)
		}
		o.Task.ArtifactIDs = []domain.ArtifactID{artifact.ID}
		return RunRef{ID: "workflow-run"}, nil
	}
	d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
	s := drain(t, d)
	if !s.OutcomeApplied || f.transport.outcomePosts != 1 {
		t.Fatalf("transition reconcile=%+v posts=%d", s, f.transport.outcomePosts)
	}
	view, err := f.service.GetWorkItemExecutionContext(context.Background(), application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Tasks) != 2 || len(view.Artifacts) != 1 || view.Artifacts[0].SubmissionID == nil {
		t.Fatal("transition or Artifact binding missing")
	}
}

func TestHTTPStaleClaimReplayNeverStartsHarness(t *testing.T) {
	for _, kind := range []CandidateKind{TaskCandidate, EmptyBlackboard} {
		t.Run(string(kind), func(t *testing.T) {
			f := newHTTPFixture(t, domain.CoordinationModeBlackboard, kind)
			a := &fakeAdapter{start: func(context.Context, StartRequest) (RunRef, error) {
				t.Fatal("started from an ended Claim replay")
				return RunRef{}, nil
			}}
			d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
			if err := d.Step(context.Background()); err == nil {
				t.Fatal("expected lost Claim response")
			}
			reaper, err := application.NewService(f.repo, &manualClock{now: time.Now().Add(time.Minute)}, f.ids)
			if err != nil {
				t.Fatal(err)
			}
			if err := reaper.ReapExpiredClaims(context.Background()); err != nil {
				t.Fatal(err)
			}
			s := drain(t, d)
			if s.Attempts != 0 || !s.ClaimEnded || s.EndReason != "expired" || f.transport.claimPosts != 2 {
				t.Fatalf("stale replay=%+v posts=%d", s, f.transport.claimPosts)
			}
		})
	}
}
