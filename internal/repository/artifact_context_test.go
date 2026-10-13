package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

func TestWorkflowArtifactInputsFollowLoopSubmissionReceipts(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := domain.WorkflowDefinition{
			DefinitionMetadata: domain.DefinitionMetadata{ID: "artifact-loop", Version: 1, Name: "Artifact loop", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime},
			Graph: domain.WorkflowGraph{
				StartTaskIDs: []domain.WorkflowTaskID{"edit", "parallel"},
				Tasks: []domain.WorkflowTaskDefinition{
					{ID: "edit", Title: "Edit", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired},
					{ID: "parallel", Title: "Parallel", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired},
					{ID: "done", Title: "Done", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				},
				Relations: []domain.WorkflowRelationDefinition{{ID: "again", FromTaskID: "edit", ToTaskID: "edit"}, {ID: "finish", FromTaskID: "edit", ToTaskID: "done"}},
			},
		}
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Loop", Goal: "Keep exact artifact inputs"})
		if err != nil {
			t.Fatal(err)
		}
		read := func() application.WorkItemExecutionContext {
			t.Helper()
			view, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return view
		}
		pending := func(node domain.WorkflowTaskID) domain.Task {
			t.Helper()
			for _, task := range read().Tasks {
				if *task.WorkflowTaskID == node && task.Status == domain.TaskStatusPending {
					return task
				}
			}
			t.Fatalf("no pending %s", node)
			return domain.Task{}
		}
		claim := func(task domain.Task) domain.Claim {
			t.Helper()
			held, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return held
		}
		artifact := func(task domain.Task, held domain.Claim, uri string) domain.Artifact {
			t.Helper()
			value, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Name: "patch", URI: uri})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		submit := func(task domain.Task, held domain.Claim, value domain.Artifact, choice domain.WorkflowChoiceGroupID) domain.TaskSubmission {
			t.Helper()
			command := application.SubmitTaskCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Result: value.URI, ArtifactIDs: []domain.ArtifactID{value.ID}}
			if choice != "" {
				command.Transition = &application.WorkflowTransitionCommand{ChoiceGroupID: choice}
			}
			submission, err := service.SubmitTask(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			return submission
		}
		review := func(taskID domain.TaskID, status domain.ReviewStatus) {
			t.Helper()
			for _, task := range read().Tasks {
				if task.ID == taskID {
					_, err := service.DecideReview(ctx, application.DecideReviewCommand{TaskID: task.ID, ReviewID: task.Reviews[len(task.Reviews)-1].ID, Identity: actor, Decision: status, Feedback: "Reviewed"})
					if err != nil {
						t.Fatal(err)
					}
					return
				}
			}
			t.Fatal("missing review task")
		}
		contextFor := func(task domain.Task) application.TaskExecutionContext {
			t.Helper()
			view, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return view
		}
		assertInput := func(task domain.Task, source domain.Task, submission domain.TaskSubmission, value domain.Artifact) {
			t.Helper()
			view := contextFor(task)
			if len(view.InputArtifacts) != 1 || len(view.SharedArtifacts) != 0 {
				t.Fatalf("input scope: %+v", view)
			}
			group := view.InputArtifacts[0]
			if group.TaskID != source.ID || group.SubmissionID != submission.ID || group.ReviewStatus != "approved" || len(group.Artifacts) != 1 || group.Artifacts[0].ID != value.ID {
				t.Fatalf("wrong submission receipt: %+v", group)
			}
		}

		first := pending("edit")
		initial := contextFor(first)
		if len(initial.InputArtifacts) != 0 || initial.InputArtifacts == nil || initial.StagedArtifacts == nil || initial.SharedArtifacts == nil {
			t.Fatalf("initial collections: %+v", initial)
		}
		firstClaim := claim(first)
		rejected := artifact(first, firstClaim, "https://example.test/rejected")
		submit(first, firstClaim, rejected, "continue:again")
		review(first.ID, domain.ReviewStatusRejected)
		firstClaim = claim(pending("edit"))
		accepted := artifact(first, firstClaim, "https://example.test/accepted")
		acceptedSubmission := submit(first, firstClaim, accepted, "continue:again")
		review(first.ID, domain.ReviewStatusApproved)

		// A newer same-name artifact awaiting Review on another branch is not input.
		parallel := pending("parallel")
		parallelClaim := claim(parallel)
		submit(parallel, parallelClaim, artifact(parallel, parallelClaim, "https://example.test/unrelated"), "")
		second := pending("edit")
		assertInput(second, first, acceptedSubmission, accepted)
		secondClaim := claim(second)
		abandoned := artifact(second, secondClaim, "https://example.test/abandoned")
		if values := contextFor(second).StagedArtifacts; len(values) != 1 || values[0].ID != abandoned.ID {
			t.Fatalf("current staged artifacts: %+v", values)
		}
		if _, err := service.FailTask(ctx, application.FailTaskCommand{TaskID: second.ID, ClaimID: secondClaim.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: "Try again"}); err != nil {
			t.Fatal(err)
		}
		replacement := pending("edit")
		assertInput(replacement, first, acceptedSubmission, accepted)
		if values := contextFor(replacement).StagedArtifacts; len(values) != 0 {
			t.Fatalf("abandoned artifacts leaked: %+v", values)
		}
		thirdClaim := claim(replacement)
		third := artifact(replacement, thirdClaim, "https://example.test/third")
		thirdSubmission := submit(replacement, thirdClaim, third, "continue:again")
		review(replacement.ID, domain.ReviewStatusApproved)
		assertInput(pending("edit"), replacement, thirdSubmission, third)
		if len(read().Artifacts) != 4 {
			t.Fatalf("history must retain rejected, accepted, unrelated and third artifacts: %+v", read().Artifacts)
		}
	})
}

func TestExecutorArtifactContextOnlyExposesBoundClaimStaging(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
		def := domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "artifact-executor", Version: 1, Name: "Executor staging", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}}
		if err := repo.CreateBlackboardDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: human, Title: "Scoped", Goal: "Isolate staging"})
		if err != nil {
			t.Fatal(err)
		}
		tasks := []domain.Task{}
		for _, title := range []string{"First", "Second"} {
			task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: work.ID, Identity: human, Title: title, Executor: domain.ExecutorEither})
			if err != nil {
				t.Fatal(err)
			}
			tasks = append(tasks, task)
		}
		token := "krs_claim_" + strings.Repeat("A", 43)
		held, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: tasks[0].ID, Identity: agent, ExecutorToken: token})
		if err != nil {
			t.Fatal(err)
		}
		principal, err := service.Authenticate(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		own, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: tasks[0].ID, ClaimID: held.ID, Identity: principal, Name: "report", URI: "https://example.test/own"})
		if err != nil {
			t.Fatal(err)
		}
		secondClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: tasks[1].ID, Identity: agent})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: tasks[1].ID, ClaimID: secondClaim.ID, Identity: agent, Name: "report", URI: "https://example.test/other"}); err != nil {
			t.Fatal(err)
		}
		for index, task := range tasks {
			view, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: task.ID, Identity: principal})
			if err != nil {
				t.Fatal(err)
			}
			if index == 0 && (len(view.StagedArtifacts) != 1 || view.StagedArtifacts[0].ID != own.ID) {
				t.Fatalf("own staging missing: %+v", view.StagedArtifacts)
			}
			if index == 1 && len(view.StagedArtifacts) != 0 {
				t.Fatalf("other staging leaked: %+v", view.StagedArtifacts)
			}
		}
		if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: tasks[0].ID, ClaimID: held.ID, Identity: agent}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: tasks[0].ID, Identity: principal}); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("ended executor context: %v", err)
		}
	})
}

func TestBlackboardArtifactContextSeparatesHistoryAndActiveClaim(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "artifact-shared", Version: 1, Name: "Shared history", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}}
		if err := repo.CreateBlackboardDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Shared", Goal: "Preserve evidence"})
		if err != nil {
			t.Fatal(err)
		}
		create := func(title string) domain.Task {
			t.Helper()
			task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: work.ID, Identity: actor, Title: title, Executor: domain.ExecutorHuman})
			if err != nil {
				t.Fatal(err)
			}
			return task
		}
		claim := func(task domain.Task) domain.Claim {
			t.Helper()
			held, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return held
		}
		artifact := func(task domain.Task, held domain.Claim, uri string) domain.Artifact {
			t.Helper()
			value, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: task.ID, ClaimID: held.ID, Identity: actor, Name: "report", URI: uri})
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		producer := create("Producer")
		consumer := create("Consumer")
		other := create("Other")
		producerClaim := claim(producer)
		old := artifact(producer, producerClaim, "https://example.test/old")
		oldSubmission, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: producer.ID, ClaimID: producerClaim.ID, Identity: actor, Result: "Needs review", ArtifactIDs: []domain.ArtifactID{old.ID}, RequestReview: true})
		if err != nil {
			t.Fatal(err)
		}
		oldConsumerClaim := claim(consumer)
		artifact(consumer, oldConsumerClaim, "https://example.test/released")
		if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: consumer.ID, ClaimID: oldConsumerClaim.ID, Identity: actor}); err != nil {
			t.Fatal(err)
		}
		read := func() application.TaskExecutionContext {
			t.Helper()
			view, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: consumer.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			return view
		}
		if len(read().StagedArtifacts) != 0 {
			t.Fatal("released Claim artifacts leaked")
		}
		consumerClaim := claim(consumer)
		current := artifact(consumer, consumerClaim, "https://example.test/current")
		artifact(other, claim(other), "https://example.test/other-staged")
		view := read()
		if len(view.InputArtifacts) != 0 || len(view.StagedArtifacts) != 1 || view.StagedArtifacts[0].ID != current.ID || len(view.SharedArtifacts) != 1 || view.SharedArtifacts[0].ReviewStatus != "pending" || view.SharedArtifacts[0].SubmissionID != oldSubmission.ID {
			t.Fatalf("mixed scopes: %+v", view)
		}
		workView, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		var reviewID domain.ReviewID
		for _, task := range workView.Tasks {
			if task.ID == producer.ID {
				reviewID = task.Reviews[0].ID
			}
		}
		if _, err := service.DecideReview(ctx, application.DecideReviewCommand{TaskID: producer.ID, ReviewID: reviewID, Identity: actor, Decision: domain.ReviewStatusRejected, Feedback: "Revise"}); err != nil {
			t.Fatal(err)
		}
		if read().SharedArtifacts[0].ReviewStatus != "rejected" {
			t.Fatal("shared rejection status lost")
		}
		producerClaim = claim(producer)
		next := artifact(producer, producerClaim, "https://example.test/new")
		if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: producer.ID, ClaimID: producerClaim.ID, Identity: actor, Result: "Revised", ArtifactIDs: []domain.ArtifactID{next.ID}}); err != nil {
			t.Fatal(err)
		}
		groups := read().SharedArtifacts
		if len(groups) != 2 || groups[0].ReviewStatus != "rejected" || groups[1].ReviewStatus != "none" || groups[0].Artifacts[0].ID != old.ID || groups[1].Artifacts[0].ID != next.ID {
			t.Fatalf("shared revisions: %+v", groups)
		}
	})
}

func TestWorkflowArtifactJoinRetainsBothSameNameSources(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		def := attemptDefinition()
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Join", Goal: "Preserve both inputs"})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		want := make(map[domain.SubmissionID]domain.ArtifactID)
		for _, source := range initial.Tasks {
			held, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: source.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			value, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{TaskID: source.ID, ClaimID: held.ID, Identity: actor, Name: "report", URI: "https://example.test/" + string(source.ID)})
			if err != nil {
				t.Fatal(err)
			}
			submission, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: source.ID, ClaimID: held.ID, Identity: actor, Result: "Completed", ArtifactIDs: []domain.ArtifactID{value.ID}, Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: domain.WorkflowChoiceGroupID("exit:" + string(*source.WorkflowTaskID))}})
			if err != nil {
				t.Fatal(err)
			}
			want[submission.ID] = value.ID
		}
		view, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		var joined domain.Task
		for _, task := range view.Tasks {
			if *task.WorkflowTaskID == "c" {
				joined = task
			}
		}
		inputs, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: joined.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if len(inputs.InputArtifacts) != 2 {
			t.Fatalf("lost parallel inputs: %+v", inputs.InputArtifacts)
		}
		for _, group := range inputs.InputArtifacts {
			if len(group.Artifacts) != 1 || group.Artifacts[0].ID != want[group.SubmissionID] || group.Artifacts[0].Name != "report" {
				t.Fatalf("wrong parallel input: %+v", group)
			}
			delete(want, group.SubmissionID)
		}
		if len(want) != 0 {
			t.Fatal("missing input submission")
		}
	})
}
