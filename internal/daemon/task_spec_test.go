package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestTaskSpecRejectsSurroundingWhitespace(t *testing.T) {
	for _, value := range []string{" backend", "backend ", " backend ", "\tbackend", "backend\n", "\u00a0backend\u00a0", "\u3000backend\u3000"} {
		for _, field := range []string{"allowed_roles", "tags"} {
			for _, kind := range []OutcomeKind{CreateTask, Decomposed} {
				t.Run(field+"/"+string(kind)+"/"+value, func(t *testing.T) {
					spec := TaskSpec{Title: "follow-up", Executor: domain.ExecutorAgent}
					if field == "allowed_roles" {
						spec.AllowedRoles = []string{value}
					} else {
						spec.Tags = []string{value}
					}
					candidate := taskCandidate()
					outcome := HarnessOutcome{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{spec}}}
					if kind == CreateTask {
						candidate.Kind = EmptyBlackboard
						candidate.TaskID = ""
						outcome = HarnessOutcome{Coordination: &CoordinationDecision{Kind: CreateTask, Task: &spec}}
					}
					before, _ := json.Marshal(outcome)
					err := outcome.Validate(candidate)
					if err == nil || !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "surrounding whitespace") {
						t.Fatalf("expected whitespace rejection, got %v", err)
					}
					if _, err := DecodeOutcome(before, candidate); err == nil || !strings.Contains(err.Error(), "surrounding whitespace") {
						t.Fatalf("JSON outcome not rejected: %v", err)
					}
					after, _ := json.Marshal(outcome)
					if string(before) != string(after) {
						t.Fatal("validation mutated the outcome")
					}
				})
			}
		}
	}
}

func TestCorrectedTaskSpecIsDiscoverableAndClaimable(t *testing.T) {
	for _, field := range []string{"allowed_roles", "tags"} {
		for _, kind := range []CandidateKind{EmptyBlackboard, TaskCandidate} {
			t.Run(field+"/"+string(kind), func(t *testing.T) {
				f := newHTTPFixture(t, domain.CoordinationModeBlackboard, kind)
				f.transport.claimLost = true
				observations := 0
				a := &fakeAdapter{observe: func(context.Context, RunRef) (RunObservation, error) {
					observations++
					spec := TaskSpec{Title: "follow-up", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Tags: []string{"implementation"}}
					if observations == 1 {
						if field == "allowed_roles" {
							spec.AllowedRoles = []string{" backend "}
						} else {
							spec.Tags = []string{" implementation "}
						}
					}
					outcome := HarnessOutcome{Task: &TaskOutcome{Kind: Decomposed, Children: []TaskSpec{spec}}}
					if kind == EmptyBlackboard {
						outcome = HarnessOutcome{Coordination: &CoordinationDecision{Kind: CreateTask, Task: &spec}}
					}
					return RunObservation{State: OutcomeReady, Outcome: &outcome}, nil
				}}
				d := dispatchForTest(t, f.client, a, f.candidate, testOptions())
				steps(t, d, 2)
				if err := d.Step(context.Background()); err == nil {
					t.Fatal("untrimmed Task spec accepted")
				}
				if d.Snapshot().State != Starting || f.transport.outcomePosts != 0 {
					t.Fatal("invalid spec reached Core instead of Harness retry")
				}
				ctx := context.Background()
				view, err := f.service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
				if err != nil {
					t.Fatal(err)
				}
				beforeTasks := 0
				if kind == TaskCandidate {
					beforeTasks = 1
				}
				if len(view.Tasks) != beforeTasks {
					t.Fatal("invalid spec created a Task")
				}
				s := drain(t, d)
				if !s.OutcomeApplied || s.Attempts != 2 || f.transport.claimPosts != 1 || f.transport.outcomePosts != 1 {
					t.Fatalf("corrected spec retry=%+v", s)
				}
				view, err = f.service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: f.candidate.WorkItemID, Identity: f.agent})
				if err != nil {
					t.Fatal(err)
				}
				if len(view.Tasks) != beforeTasks+1 {
					t.Fatal("corrected spec created duplicate Tasks")
				}
				var created domain.Task
				for _, task := range view.Tasks {
					if task.Title == "follow-up" {
						created = task
					}
				}
				if created.ID == "" || len(created.AllowedRoles) != 1 || created.AllowedRoles[0] != "backend" || len(created.Tags) != 1 || created.Tags[0] != "implementation" {
					t.Fatalf("unexpected persisted spec: %+v", created)
				}
				if kind == TaskCandidate && (created.ParentTaskID == nil || *created.ParentTaskID != f.candidate.TaskID) {
					t.Fatal("child has wrong parent")
				}
				candidates, err := f.service.FindWork(ctx, application.FindWorkQuery{Identity: f.agent, Tags: []string{"implementation"}, Limit: 50})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, candidate := range candidates {
					if candidate.Task != nil && candidate.Task.ID == created.ID {
						found = true
					}
				}
				if !found {
					t.Fatal("corrected Task not discoverable by role and tag")
				}
				claim, err := f.service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: created.ID, Identity: f.agent})
				if err != nil {
					t.Fatalf("corrected Task not claimable: %v", err)
				}
				if claim.TaskID != created.ID || !claim.Active() {
					t.Fatal("Claim not established for corrected Task")
				}
			})
		}
	}
}
