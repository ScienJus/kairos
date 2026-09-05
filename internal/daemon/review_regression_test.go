package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestInvalidOutcomePayloadUsesHarnessRetry(t *testing.T) {
	badSpecs := []struct {
		name  string
		spec  TaskSpec
		field string
	}{
		{"empty_role", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, AllowedRoles: []string{" "}}, "allowed_roles"},
		{"untrimmed_role", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, AllowedRoles: []string{" backend "}}, "allowed_roles"},
		{"untrimmed_tag", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, Tags: []string{" tag "}}, "tags"},
		{"duplicate_role", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend", "backend"}}, "allowed_roles"},
		{"trimmed_duplicate_role", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend", " backend "}}, "allowed_roles"},
		{"empty_tag", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, Tags: []string{""}}, "tags"},
		{"duplicate_tag", TaskSpec{Title: "child", Executor: domain.ExecutorAgent, Tags: []string{"tag", " tag "}}, "tags"},
		{"human_role", TaskSpec{Title: "child", Executor: domain.ExecutorHuman, AllowedRoles: []string{"backend"}}, "human"},
	}
	type testCase struct {
		name      string
		candidate Candidate
		outcome   HarnessOutcome
		field     string
	}
	cases := []testCase{}
	for _, tc := range badSpecs {
		for _, kind := range []OutcomeKind{CreateTask, Decomposed} {
			c := taskCandidate()
			o := HarnessOutcome{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{tc.spec}}}
			if kind == CreateTask {
				c.Kind = EmptyBlackboard
				c.TaskID = ""
				o = HarnessOutcome{Coordination: &CoordinationDecision{Kind: CreateTask, Task: &tc.spec}}
			}
			cases = append(cases, testCase{tc.name + "/" + string(kind), c, o, tc.field})
		}
	}
	for i, transition := range []Transition{
		{ChoiceGroupID: "continue", SkipOptionalTaskIDs: []domain.WorkflowTaskID{" "}},
		{ChoiceGroupID: "continue", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"a", "a"}},
		{ChoiceGroupID: "continue", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"a"}, ReviewSkippedTaskIDs: []domain.WorkflowTaskID{" "}},
		{ChoiceGroupID: "continue", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"a"}, ReviewSkippedTaskIDs: []domain.WorkflowTaskID{"a", "a"}},
		{ChoiceGroupID: "continue", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"a"}, ReviewSkippedTaskIDs: []domain.WorkflowTaskID{"b"}},
	} {
		c := taskCandidate()
		c.Mode = domain.CoordinationModeWorkflow
		o := completedOutcome()
		o.Task.Transition = &transition
		cases = append(cases, testCase{fmt.Sprintf("transition_%d", i), c, o, "skip/review"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.outcome.Validate(tc.candidate); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("payload validation=%v, want %s", err, tc.field)
			}
			for _, recover := range []bool{false, true} {
				t.Run(fmt.Sprintf("recover_%t", recover), func(t *testing.T) {
					core := newFakeCore()
					good := completedOutcome()
					if tc.candidate.Mode == domain.CoordinationModeWorkflow {
						policy := domain.ReviewExecutorDecides
						core.status.Task.ReviewPolicy = &policy
					}
					if tc.candidate.Kind != TaskCandidate {
						good = HarnessOutcome{Coordination: &CoordinationDecision{Kind: SubmitCompletion, Result: "done"}}
					}
					observations := 0
					a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
						observations++
						o := tc.outcome
						if recover && observations > 1 {
							o = good
						}
						return RunObservation{State: OutcomeReady, Outcome: &o}, nil
					}}
					d := dispatchForTest(t, core, a, tc.candidate, testOptions())
					steps(t, d, 2)
					if err := d.Step(context.Background()); err == nil {
						t.Fatal("invalid output was not rejected")
					}
					if s := d.Snapshot(); s.State != Starting || s.RunState != RuntimeFailed || core.applies != 0 || core.releases != 0 {
						t.Fatalf("invalid output escaped to Core: %+v", s)
					}
					s := drain(t, d)
					if s.Attempts != 2 || !s.ClaimEnded || core.claimCalls != 1 {
						t.Fatalf("retry lifecycle=%+v", s)
					}
					if recover {
						if !s.OutcomeApplied || core.applies != 1 || core.releases != 0 {
							t.Fatalf("corrected output=%+v", s)
						}
					} else {
						if s.StopReason != StopRuntimeFailure || core.applies != 0 || core.releases != 1 || len(core.status.Task.Failures) != 0 {
							t.Fatalf("invalid output exhaustion=%+v", s)
						}
					}
				})
			}
		})
	}
}

func TestCanonicalTaskSpecValidationMatchesCore(t *testing.T) {
	for _, executor := range []domain.ExecutorRequirement{domain.ExecutorAgent, domain.ExecutorHuman, domain.ExecutorEither} {
		for _, values := range [][]string{nil, {}, {"backend"}, {""}, {"backend", "backend"}} {
			for _, field := range []string{"roles", "tags"} {
				spec := TaskSpec{Title: "child", Executor: executor}
				if field == "roles" {
					spec.AllowedRoles = values
				} else {
					spec.Tags = values
				}
				now := time.Now()
				task := domain.Task{ID: "child", WorkItemID: "work", Title: spec.Title, Status: domain.TaskStatusPending, Executor: executor, AllowedRoles: spec.AllowedRoles, Tags: spec.Tags, CreatedAt: now, UpdatedAt: now}
				want := task.Validate(domain.CoordinationModeBlackboard)
				if got := spec.validate(); (got == nil) != (want == nil) {
					t.Fatalf("spec/Core mismatch: %s %s %v: %v / %v", executor, field, values, got, want)
				}
			}
		}
	}
}

func TestOutcomeHistoryTextByteLimits(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", domain.MaxHistoryTextBytes), strings.Repeat("x", domain.MaxHistoryTextBytes+1), strings.Repeat("界", domain.MaxHistoryTextBytes/3+1)} {
		for _, field := range []string{"result", "failure_reason", "retry_prompt", "release_reason", "completion", "transition"} {
			t.Run(fmt.Sprintf("%s/%d", field, len(text)), func(t *testing.T) {
				c := taskCandidate()
				o := completedOutcome()
				switch field {
				case "result":
					o.Task.Result = text
				case "failure_reason":
					o.Task = &TaskOutcome{Kind: TerminalFailure, Reason: text}
				case "retry_prompt":
					o.Task = &TaskOutcome{Kind: RetryableFailure, Reason: "failed", RetryPrompt: text}
				case "release_reason":
					o.Task = &TaskOutcome{Kind: Abandoned, Reason: text}
				case "completion":
					c.Kind = EmptyBlackboard
					c.TaskID = ""
					o = HarnessOutcome{Coordination: &CoordinationDecision{Kind: SubmitCompletion, Result: text}}
				case "transition":
					c.Mode = domain.CoordinationModeWorkflow
					o.Task.Transition = &Transition{ChoiceGroupID: "continue", Reason: text}
				}
				err := o.Validate(c)
				valid := len(text) <= domain.MaxHistoryTextBytes
				if (err == nil) != valid || (!valid && !strings.Contains(err.Error(), "byte limit")) {
					t.Fatalf("text length %d: %v", len(text), err)
				}
			})
		}
	}
}

func TestCoreRequestCollectionEncoding(t *testing.T) {
	for _, populated := range []bool{false, true} {
		for _, kind := range []string{"completed", "transition", "create_task", "decomposed"} {
			t.Run(fmt.Sprintf("%s/populated_%t", kind, populated), func(t *testing.T) {
				c := taskCandidate()
				o := completedOutcome()
				spec := TaskSpec{Title: "child", Executor: domain.ExecutorAgent}
				transition := &Transition{ChoiceGroupID: "continue"}
				if populated {
					o.Task.ArtifactIDs = []domain.ArtifactID{"artifact"}
					spec.AllowedRoles = []string{"backend"}
					spec.Tags = []string{"tag"}
					transition.SkipOptionalTaskIDs = []domain.WorkflowTaskID{"next"}
					transition.ReviewSkippedTaskIDs = []domain.WorkflowTaskID{"next"}
				}
				switch kind {
				case "transition":
					c.Mode = domain.CoordinationModeWorkflow
					o.Task.Transition = transition
				case "create_task":
					c.Kind = EmptyBlackboard
					c.TaskID = ""
					o = HarnessOutcome{Coordination: &CoordinationDecision{Kind: CreateTask, Task: &spec}}
				case "decomposed":
					o = HarnessOutcome{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{spec}}}
				}
				before, _ := json.Marshal(o)
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					assertArray := func(object map[string]json.RawMessage, field, value string) {
						t.Helper()
						want := "[]"
						if populated {
							encoded, _ := json.Marshal([]string{value})
							want = string(encoded)
						}
						if got := string(object[field]); got != want {
							t.Errorf("%s=%s want %s", field, got, want)
						}
					}
					if kind == "completed" || kind == "transition" {
						assertArray(body, "artifact_ids", "artifact")
					}
					if kind == "completed" && string(body["transition"]) != "null" {
						t.Error("absent transition should remain null")
					}
					if kind == "transition" {
						var nested map[string]json.RawMessage
						_ = json.Unmarshal(body["transition"], &nested)
						assertArray(nested, "skip_optional_task_ids", "next")
						assertArray(nested, "review_skipped_task_ids", "next")
					}
					if kind == "create_task" || kind == "decomposed" {
						nested := body
						if kind == "decomposed" {
							var children []map[string]json.RawMessage
							if err := json.Unmarshal(body["children"], &children); err != nil || len(children) != 1 {
								t.Errorf("children=%s", body["children"])
								return
							}
							nested = children[0]
						}
						assertArray(nested, "allowed_roles", "backend")
						assertArray(nested, "tags", "tag")
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":{}}`))
				}))
				defer server.Close()
				client, err := NewHTTPClient(server.URL, NewSecret("identity-token"), server.Client())
				if err != nil {
					t.Fatal(err)
				}
				if err := client.Apply(context.Background(), c, "claim", "operation", o); err != nil {
					t.Fatal(err)
				}
				after, _ := json.Marshal(o)
				if calls != 1 || string(before) != string(after) {
					t.Fatal("request normalization mutated outcome or did not send request")
				}
			})
		}
	}
}

func TestCompletedReconcileReviewMatrix(t *testing.T) {
	for _, mode := range []domain.CoordinationMode{domain.CoordinationModeBlackboard, domain.CoordinationModeWorkflow} {
		for _, policy := range []domain.ReviewPolicy{"", domain.ReviewNone, domain.ReviewExecutorDecides, domain.ReviewRequired, "invalid"} {
			for _, requested := range []bool{false, true} {
				for _, reason := range []string{"task_completed", "submitted_for_review"} {
					for _, ack := range []bool{false, true} {
						c := taskCandidate()
						c.Mode = mode
						o := completedOutcome()
						o.Task.RequestReview = requested
						task := domain.Task{Submissions: []domain.TaskSubmission{{ClaimID: "claim", Result: "done"}}}
						if policy != "" {
							task.ReviewPolicy = &policy
						}
						status := ClaimStatus{Claim: Claim{ID: "claim", EndReason: reason}, Task: &task}
						valid, review := true, requested
						if mode == domain.CoordinationModeWorkflow {
							switch policy {
							case domain.ReviewRequired:
								review = true
							case domain.ReviewExecutorDecides:
							case domain.ReviewNone:
								valid = !requested
							default:
								valid = false
							}
						}
						want := valid && ((review && reason == "submitted_for_review") || (!review && reason == "task_completed"))
						if got := outcomeMatches(c, status, o, ack); got != want {
							t.Errorf("mode=%s policy=%s requested=%t end=%s ack=%t got=%t want=%t", mode, policy, requested, reason, ack, got, want)
						}
					}
				}
			}
		}
	}
}

func TestHTTPReconcileEffectiveReview(t *testing.T) {
	for _, mode := range []domain.CoordinationMode{domain.CoordinationModeBlackboard, domain.CoordinationModeWorkflow} {
		t.Run(string(mode)+"/other_caller", func(t *testing.T) {
			f := newHTTPFixture(t, mode, TaskCandidate)
			f.transport.claimLost = true
			d := dispatchForTest(t, f.client, outcomeAdapter(completedOutcome()), f.candidate, testOptions())
			steps(t, d, 3)
			_, err := f.service.SubmitTask(context.Background(), application.SubmitTaskCommand{TaskID: f.candidate.TaskID, ClaimID: domain.ClaimID(d.Snapshot().ClaimID), Identity: f.agent, Result: "done", RequestReview: true})
			if err != nil {
				t.Fatal(err)
			}
			s := drain(t, d)
			if s.OutcomeApplied || s.EndReason != "submitted_for_review" || f.transport.outcomePosts != 0 {
				t.Fatalf("misattributed other caller: %+v", s)
			}
			status, err := f.client.Inspect(context.Background(), f.candidate, s.ClaimID)
			if err != nil {
				t.Fatal(err)
			}
			if status.Task.Status != domain.TaskStatusInReview || len(status.Task.Submissions) != 1 || len(status.Task.Reviews) != 1 {
				t.Fatal("unexpected persisted Review or Submission")
			}
		})
	}
	for _, requested := range []bool{false, true} {
		t.Run(fmt.Sprintf("required/requested_%t", requested), func(t *testing.T) {
			graph := domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"execute"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "execute", Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired}}}
			f := newHTTPFixture(t, domain.CoordinationModeWorkflow, TaskCandidate, graph)
			o := completedOutcome()
			o.Task.RequestReview = requested
			d := dispatchForTest(t, f.client, outcomeAdapter(o), f.candidate, testOptions())
			s := drain(t, d)
			if !s.OutcomeApplied || s.EndReason != "submitted_for_review" || f.transport.outcomePosts != 1 {
				t.Fatalf("required Review lost response=%+v", s)
			}
			status, err := f.client.Inspect(context.Background(), f.candidate, s.ClaimID)
			if err != nil {
				t.Fatal(err)
			}
			if status.Task.Status != domain.TaskStatusInReview || len(status.Task.Submissions) != 1 || len(status.Task.Reviews) != 1 {
				t.Fatal("required Review not persisted exactly once")
			}
		})
	}
}

func TestHTTPInvalidPlanningOutcomeRetriesBeforeMutation(t *testing.T) {
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, EmptyBlackboard)
	observations := 0
	a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
		observations++
		spec := TaskSpec{Title: "planned", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}}
		if observations == 1 {
			spec.AllowedRoles = append(spec.AllowedRoles, "backend")
		}
		o := HarnessOutcome{Coordination: &CoordinationDecision{Kind: CreateTask, Task: &spec}}
		return RunObservation{State: OutcomeReady, Outcome: &o}, nil
	}}
	d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
	s := drain(t, d)
	if !s.OutcomeApplied || s.Attempts != 2 || f.transport.outcomePosts != 1 || f.transport.claimPosts != 2 {
		t.Fatalf("invalid plan reached Core or lost Claim: %+v posts=%d", s, f.transport.outcomePosts)
	}
	view, err := f.service.GetWorkItemExecutionContext(context.Background(), application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Tasks) != 1 || len(view.CoordinationClaims) != 1 || view.CoordinationClaims[0].EndReason != domain.CoordinationClaimEndTaskCreated {
		t.Fatal("retry changed durable plan or Claim history")
	}
}
