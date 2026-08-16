package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

func TestGetWorkflowTaskExecutionContext(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	definition.AgentInstructions = "Prefer the smallest safe delivery path."
	definition.SuggestedTags = []string{"module:*", "risk:*"}
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Execution context", Goal: "Inspect workflow choices",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	task := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: task.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get execution context: %v", err)
	}
	if contextView.Workflow == nil || contextView.Blackboard != nil {
		t.Fatalf("mode context: %#v", contextView)
	}
	if contextView.Definition.AgentInstructions != definition.AgentInstructions || len(contextView.Claims) != 1 {
		t.Fatalf("definition or claims: %#v", contextView)
	}
	if len(contextView.Workflow.ChoiceGroups) != 1 {
		t.Fatalf("choice groups: %#v", contextView.Workflow.ChoiceGroups)
	}
	choice := contextView.Workflow.ChoiceGroups[0]
	if choice.ID != "exit:implement" || len(choice.Targets) != 1 || choice.Targets[0].ID != "docs" {
		t.Fatalf("exit choice: %#v", choice)
	}
	if got := workflowDefinitionTaskIDs(choice.SkippableOptionalTasks); !slices.Equal(got, []domain.WorkflowTaskID{"docs", "examples"}) {
		t.Fatalf("skip candidates: got %v", got)
	}

	other := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "other"}, Role: "backend"}
	_, err = service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{TaskID: task.ID, Identity: other})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("context for non-owner: got %v", err)
	}

	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
		},
	}); err != nil {
		t.Fatalf("submit task: %v", err)
	}
	integration := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["integration"]
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: integration.ID, Identity: agent}); err != nil {
		t.Fatalf("claim integration: %v", err)
	}
	integrationContext, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: integration.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get integration context: %v", err)
	}
	if got := runtimeWorkflowTaskIDs(integrationContext.Workflow.UpstreamTasks); !slices.Equal(got, []domain.WorkflowTaskID{"examples", "docs", "implement"}) {
		t.Fatalf("integration upstream tasks: got %v", got)
	}
}

func TestGetBlackboardTaskExecutionContext(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	definition.AgentInstructions = "Keep the board small and update shared findings."
	definition.SuggestedTags = []string{"module:*"}
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Blackboard context", Goal: "Inspect shared work",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   agent, Title: "Investigate", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   agent, Title: "Summarize", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: first.ID, ToTaskID: second.ID, Identity: agent,
	}); err != nil {
		t.Fatalf("add relation: %v", err)
	}
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: first.ID, Identity: agent}); err != nil {
		t.Fatalf("claim first task: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: first.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get execution context: %v", err)
	}
	if contextView.Blackboard == nil || contextView.Workflow != nil {
		t.Fatalf("mode context: %#v", contextView)
	}
	if len(contextView.Blackboard.Tasks) != 2 || len(contextView.Blackboard.Relations) != 1 {
		t.Fatalf("blackboard space: %#v", contextView.Blackboard)
	}
	if contextView.Definition.AgentInstructions != definition.AgentInstructions {
		t.Fatalf("agent instructions: got %q", contextView.Definition.AgentInstructions)
	}
}

var applicationTestTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

func TestCreateWorkflowWorkItemAndClaimByRole(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.workflows[definitionKey("workflow", 1)] = workflowDefinition()
	service := newTestService(t, repository)
	backend := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-backend"}, Role: "backend"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "workflow", Version: 1, Mode: domain.CoordinationModeWorkflow},
		Identity:   backend,
		Title:      "Implement login",
		Goal:       "Users can log in",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 1 || tasks[0].WorkflowTaskID == nil || *tasks[0].WorkflowTaskID != "implement" {
		t.Fatalf("workflow start tasks: %#v", tasks)
	}

	frontend := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-frontend"}, Role: "frontend"}
	frontendWork, err := service.FindWork(context.Background(), FindWorkQuery{Identity: frontend})
	if err != nil {
		t.Fatalf("find frontend work: %v", err)
	}
	if len(frontendWork) != 0 {
		t.Fatalf("frontend candidates: got %d, want 0", len(frontendWork))
	}

	candidates, err := service.FindWork(context.Background(), FindWorkQuery{Identity: backend, Tags: []string{"backend"}})
	if err != nil {
		t.Fatalf("find backend work: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("backend candidates: got %d, want 1", len(candidates))
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: candidates[0].Task.ID, Identity: backend})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	claimed := repository.tasks[candidates[0].Task.ID]
	if claimed.Status != domain.TaskStatusWorking || claimed.ActiveClaimID == nil || *claimed.ActiveClaimID != claim.ID {
		t.Fatalf("claimed task state: %#v", claimed)
	}

	if err := service.ReleaseClaim(context.Background(), ReleaseClaimCommand{
		TaskID: claimed.ID, ClaimID: claim.ID, Identity: backend, Reason: "Switch executor",
	}); err != nil {
		t.Fatalf("release claim: %v", err)
	}
	released := repository.tasks[claimed.ID]
	if released.Status != domain.TaskStatusPending || released.ActiveClaimID != nil {
		t.Fatalf("released task state: %#v", released)
	}
}

func TestFindWorkDiscoversEmptyBlackboard(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	definition.AgentInstructions = "Create the smallest useful plan."
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent,
		Title: "Empty board", Goal: "Create a plan", Tags: []string{"module:auth"},
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	candidates, err := service.FindWork(context.Background(), FindWorkQuery{
		Identity: agent, Tags: []string{"module:auth"},
	})
	if err != nil {
		t.Fatalf("find empty blackboard: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Kind != WorkCandidateEmptyBlackboard || candidates[0].WorkItem.ID != workItem.ID {
		t.Fatalf("empty blackboard candidate: %#v", candidates)
	}
	if candidates[0].Definition.AgentInstructions != definition.AgentInstructions {
		t.Fatalf("definition context: %#v", candidates[0].Definition)
	}
	other, err := service.FindWork(context.Background(), FindWorkQuery{
		Identity: agent, Tags: []string{"module:billing"},
	})
	if err != nil || len(other) != 0 {
		t.Fatalf("unmatched empty blackboard: candidates=%#v err=%v", other, err)
	}
	completed, err := service.CompleteBlackboardWorkItem(context.Background(), CompleteBlackboardWorkItemCommand{
		WorkItemID: workItem.ID,
		Identity:   agent,
		Result:     "The goal requires no execution tasks.",
	})
	if err != nil {
		t.Fatalf("complete empty blackboard: %v", err)
	}
	if completed.Status != domain.WorkItemStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed empty blackboard: %#v", completed)
	}
}

func TestBlackboardPlanningAndAutomaticCompletion(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Investigate flaky tests",
		Goal:       "Make the suite reliable",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity,
		Title:      "Collect failures",
		Executor:   domain.ExecutorEither,
		Tags:       []string{"investigation"},
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity,
		Title:      "Remove obsolete hypothesis",
		Executor:   domain.ExecutorAgent,
		Tags:       []string{"cleanup"},
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: first.ID,
		ToTaskID:   second.ID,
		Identity:   identity,
	}); err != nil {
		t.Fatalf("add relation: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: second.ID,
		ToTaskID:   first.ID,
		Identity:   identity,
	}); err == nil {
		t.Fatal("add cyclic relation: got nil error")
	}

	if _, err := service.SkipBlackboardTask(context.Background(), SkipBlackboardTaskCommand{
		TaskID: first.ID, Identity: identity, Reason: "Covered by existing logs",
	}); err != nil {
		t.Fatalf("skip first task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard completed with pending task: got %s", got)
	}
	if _, err := service.SkipBlackboardTask(context.Background(), SkipBlackboardTaskCommand{
		TaskID: second.ID, Identity: identity, Reason: "No longer useful",
	}); err != nil {
		t.Fatalf("skip second task: %v", err)
	}
	completed := repository.workItems[workItem.ID]
	if completed.Status != domain.WorkItemStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed blackboard: %#v", completed)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identity, Title: "Late task", Executor: domain.ExecutorAgent,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("append after automatic completion: got %v", err)
	}
}

func TestBlackboardFollowUpCreatedBeforeSubmissionKeepsWorkItemOpen(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Investigate login", Goal: "Resolve the login issue",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Investigate failure", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create initial task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: first.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim initial task: %v", err)
	}
	followUp, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Apply discovered fix", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create follow-up task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: first.ID, ClaimID: claim.ID, Identity: agent, Result: "Found the root cause",
	}); err != nil {
		t.Fatalf("submit initial task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard with follow-up task: got %s", got)
	}
	followUpClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: followUp.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim follow-up task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: followUp.ID, ClaimID: followUpClaim.ID, Identity: agent, Result: "Applied the fix",
	}); err != nil {
		t.Fatalf("submit follow-up task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusCompleted {
		t.Fatalf("blackboard after final task: got %s", got)
	}
}

func TestBlackboardPlanningAppendsAgainstLatestRevision(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Plan login",
		Goal:       "Produce one coherent plan",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Implement login", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Test login", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("append second task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Version; got != 2 {
		t.Fatalf("work item version: got %d, want 2", got)
	}
	if got := len(repository.tasksFor(workItem.ID)); got != 2 {
		t.Fatalf("task count: got %d, want 2", got)
	}
}

func TestBlackboardTaskHierarchySupportsOpenAppendAndRecursiveCompletion(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	planner := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	contributor := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "contributor"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   planner, Title: "Implement login", Goal: "Deliver login",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: planner, Title: "Implement login", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	rootClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: root.ID, Identity: planner})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	decomposeCommand := DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: rootClaim.ID, Identity: planner,
		OperationID: "decompose-login",
		Children: []BlackboardTaskSpec{
			{Title: "Implement authentication", Executor: domain.ExecutorAgent},
			{Title: "Run login tests", Executor: domain.ExecutorAgent},
		},
	}
	decomposition, err := service.DecomposeBlackboardTask(context.Background(), decomposeCommand)
	if err != nil {
		t.Fatalf("decompose root task: %v", err)
	}
	repeated, err := service.DecomposeBlackboardTask(context.Background(), decomposeCommand)
	if err != nil {
		t.Fatalf("repeat decomposition: %v", err)
	}
	if repeated.Parent.ID != decomposition.Parent.ID || repeated.Children[0].ID != decomposition.Children[0].ID {
		t.Fatalf("idempotent decomposition: first=%#v repeated=%#v", decomposition, repeated)
	}
	if decomposition.Parent.Status != domain.TaskStatusWaitingChildren || decomposition.Parent.ActiveClaimID != nil || decomposition.Parent.DecomposedAt == nil {
		t.Fatalf("aggregate root: %#v", decomposition.Parent)
	}
	if claim := repository.claims[rootClaim.ID]; claim.Active() || claim.EndReason != domain.ClaimEndTaskDecomposed {
		t.Fatalf("decomposition claim: %#v", claim)
	}
	for _, child := range decomposition.Children {
		if child.ParentTaskID == nil || *child.ParentTaskID != root.ID {
			t.Fatalf("child parent: %#v", child)
		}
	}

	authentication := decomposition.Children[0]
	authenticationClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: authentication.ID, Identity: planner})
	if err != nil {
		t.Fatalf("claim authentication task: %v", err)
	}
	nested, err := service.DecomposeBlackboardTask(context.Background(), DecomposeBlackboardTaskCommand{
		TaskID: authentication.ID, ClaimID: authenticationClaim.ID, Identity: planner,
		Children: []BlackboardTaskSpec{
			{Title: "Implement password verification", Executor: domain.ExecutorAgent},
			{Title: "Implement session management", Executor: domain.ExecutorAgent},
		},
	})
	if err != nil {
		t.Fatalf("decompose authentication task: %v", err)
	}
	extra, err := service.AddBlackboardChildTask(context.Background(), AddBlackboardChildTaskCommand{
		ParentTaskID: authentication.ID,
		Identity:     contributor,
		Task:         BlackboardTaskSpec{Title: "Add brute force protection", Executor: domain.ExecutorAgent},
	})
	if err != nil {
		t.Fatalf("append child task: %v", err)
	}
	if extra.ParentTaskID == nil || *extra.ParentTaskID != authentication.ID {
		t.Fatalf("appended child: %#v", extra)
	}

	complete := func(task domain.Task) {
		t.Helper()
		claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: contributor})
		if err != nil {
			t.Fatalf("claim %q: %v", task.Title, err)
		}
		if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
			TaskID: task.ID, ClaimID: claim.ID, Identity: contributor, Result: "Completed " + task.Title,
		}); err != nil {
			t.Fatalf("complete %q: %v", task.Title, err)
		}
	}
	complete(decomposition.Children[1])
	for _, child := range nested.Children {
		complete(child)
	}
	if parent := repository.tasks[root.ID]; parent.Status != domain.TaskStatusWaitingChildren {
		t.Fatalf("root completed too early: %#v", parent)
	}
	complete(extra)

	completedNested := repository.tasks[authentication.ID]
	completedRoot := repository.tasks[root.ID]
	if completedNested.Status != domain.TaskStatusCompleted || completedRoot.Status != domain.TaskStatusCompleted {
		t.Fatalf("recursive completion: nested=%s root=%s", completedNested.Status, completedRoot.Status)
	}
	if len(completedNested.Submissions) != 0 || len(completedRoot.Submissions) != 0 {
		t.Fatalf("aggregate submissions: nested=%d root=%d", len(completedNested.Submissions), len(completedRoot.Submissions))
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusCompleted {
		t.Fatalf("blackboard after aggregate closure: got %s", got)
	}
	if _, err := service.AddBlackboardChildTask(context.Background(), AddBlackboardChildTaskCommand{
		ParentTaskID: root.ID, Identity: contributor,
		Task: BlackboardTaskSpec{Title: "Late child", Executor: domain.ExecutorAgent},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("append to completed aggregate: got %v", err)
	}
	if err := domain.ValidateBlackboardTaskHierarchy(workItem.ID, repository.tasksFor(workItem.ID)); err != nil {
		t.Fatalf("final task hierarchy: %v", err)
	}
}

func TestOperationIDReturnsOriginalResultAndRejectsReuse(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity, Title: "Idempotency", Goal: "Avoid duplicate tasks",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	command := CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, OperationID: "create-login-task",
		Title: "Implement login", Executor: domain.ExecutorAgent,
	}
	first, err := service.CreateBlackboardTask(context.Background(), command)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), command)
	if err != nil {
		t.Fatalf("repeated request: %v", err)
	}
	if first.ID != second.ID || len(repository.tasksFor(workItem.ID)) != 1 {
		t.Fatalf("repeated result: first=%q second=%q tasks=%d", first.ID, second.ID, len(repository.tasksFor(workItem.ID)))
	}
	command.Title = "Implement another login"
	if _, err := service.CreateBlackboardTask(context.Background(), command); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused operation id: got %v", err)
	}
}

func TestFailTaskReopensAndPreservesFailure(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Diagnose deployment",
		Goal:       "Restore deployment",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Inspect logs", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	failure, err := service.FailTask(context.Background(), FailTaskCommand{
		TaskID:      task.ID,
		ClaimID:     claim.ID,
		Identity:    identity,
		Action:      domain.TaskFailureReopen,
		Reason:      "Logs are unavailable",
		RetryPrompt: "Check the archive after replication catches up.",
	})
	if err != nil {
		t.Fatalf("fail task: %v", err)
	}
	reopened := repository.tasks[task.ID]
	if reopened.Status != domain.TaskStatusPending || len(reopened.Failures) != 1 || reopened.Failures[0].ID != failure.ID {
		t.Fatalf("reopened task: %#v", reopened)
	}
}

func TestWorkflowReviewGatesTransitionApplication(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := reviewedWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Implement login", Goal: "Users can log in",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID:   start.ID,
		ClaimID:  claim.ID,
		Identity: agent,
		Result:   "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement",
			Reason:        "Ready for testing",
		},
	}); err != nil {
		t.Fatalf("submit reviewed task: %v", err)
	}
	start = repository.tasks[start.ID]
	if start.Status != domain.TaskStatusInReview || len(start.TransitionDecisions) != 1 || start.TransitionDecisions[0].AppliedAt != nil {
		t.Fatalf("task awaiting review: %#v", start)
	}
	if got := len(repository.tasksFor(workItem.ID)); got != 1 {
		t.Fatalf("tasks before approval: got %d, want 1", got)
	}

	review := start.Reviews[len(start.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: start.ID, ReviewID: review.ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve review: %v", err)
	}
	start = repository.tasks[start.ID]
	if start.Status != domain.TaskStatusCompleted || start.TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("approved task: %#v", start)
	}
	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 2 || tasks[1].WorkflowTaskID == nil || *tasks[1].WorkflowTaskID != "test" {
		t.Fatalf("tasks after approval: %#v", tasks)
	}
}

func TestBlackboardReviewReopensTaskWithCompleteHistory(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	firstAgent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-1"}, Role: "generalist"}
	secondAgent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-2"}, Role: "generalist"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   firstAgent, Title: "Investigate incident", Goal: "Identify the cause",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   firstAgent, Title: "Analyze traces", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: firstAgent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: firstAgent, Result: "Initial diagnosis", RequestReview: true,
	}); err != nil {
		t.Fatalf("submit for review: %v", err)
	}
	task = repository.tasks[task.ID]
	review := task.Reviews[len(task.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: task.ID, ReviewID: review.ID, Identity: reviewer,
		Decision: domain.ReviewStatusRejected, Feedback: "Include database traces.",
	}); err != nil {
		t.Fatalf("reject review: %v", err)
	}
	task = repository.tasks[task.ID]
	if task.Status != domain.TaskStatusPending || len(task.Submissions) != 1 || len(task.Reviews) != 1 {
		t.Fatalf("reopened task history: %#v", task)
	}

	claim, err = service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: secondAgent})
	if err != nil {
		t.Fatalf("reclaim task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: secondAgent,
		Result: "Diagnosis with database traces", RequestReview: true,
	}); err != nil {
		t.Fatalf("submit revised task: %v", err)
	}
	task = repository.tasks[task.ID]
	review = task.Reviews[len(task.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: task.ID, ReviewID: review.ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve revised task: %v", err)
	}
	task = repository.tasks[task.ID]
	if task.Status != domain.TaskStatusCompleted || len(task.Submissions) != 2 || len(task.Reviews) != 2 || task.Reviews[0].Feedback != "Include database traces." {
		t.Fatalf("completed revised task: %#v", task)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusCompleted {
		t.Fatalf("blackboard after approved final task: got %s", got)
	}
}

func TestWorkflowActivationJoinsParallelTasks(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Parallel delivery", Goal: "Deliver joined result",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{
		ChoiceGroupID: "exit:start",
	})

	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 3 {
		t.Fatalf("parallel tasks: got %d, want 3", len(tasks))
	}
	byDefinition := workflowTasksByDefinition(tasks)
	completeWorkflowTask(t, service, repository, agent, byDefinition["b"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:b",
	})
	if _, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]; exists {
		t.Fatal("join task was created before all inputs resolved")
	}

	completeWorkflowTask(t, service, repository, agent, byDefinition["c"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:c",
	})
	joined, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]
	if !exists {
		t.Fatal("join task was not created after all inputs resolved")
	}
	relations, err := repository.ListTaskRelations(workItem.ID)
	if err != nil {
		t.Fatalf("list relations: %v", err)
	}
	predecessors := 0
	for _, relation := range relations {
		if relation.ToTaskID == joined.ID {
			predecessors++
		}
	}
	if predecessors != 2 {
		t.Fatalf("join predecessors: got %d, want 2", predecessors)
	}
}

func TestWorkflowOptionalSkipRequiresConfiguredReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := optionalReviewWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Optional documentation", Goal: "Finish delivery",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
	})
	docs := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.Reviews) != 1 || docs.Reviews[0].SubmissionID != nil {
		t.Fatalf("optional skip review: %#v", docs)
	}
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: docs.ID, ReviewID: docs.Reviews[0].ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve optional skip: %v", err)
	}
	docs = repository.tasks[docs.ID]
	if docs.Status != domain.TaskStatusSkipped || docs.CompletedAt == nil {
		t.Fatalf("approved optional skip: %#v", docs)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusCompleted {
		t.Fatalf("workflow status: got %q, want completed", got)
	}
}

func TestWorkflowSkipIntentCanRequestOptionalReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := optionalReviewWorkflowDefinition()
	definition.ID = "optional-requested-review"
	definition.Graph.Tasks[1].ReviewPolicy = domain.ReviewExecutorDecides
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Optional requested review", Goal: "Review a skip decision",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:        "exit:implement",
			SkipOptionalTaskIDs:  []domain.WorkflowTaskID{"docs"},
			ReviewSkippedTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit reviewed skip intent: %v", err)
	}
	docs := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.Reviews) != 1 {
		t.Fatalf("requested skip review: %#v", docs)
	}
}

func TestWorkflowSkipIntentAdvancesConsecutiveOptionalTasks(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Deliver feature", Goal: "Reach integration testing",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
		Reason:              "Documentation and examples are unnecessary for this internal change.",
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit skip intent: %v", err)
	}

	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["docs"].Status != domain.TaskStatusSkipped || byDefinition["examples"].Status != domain.TaskStatusSkipped {
		t.Fatalf("skipped optional tasks: docs=%s examples=%s", byDefinition["docs"].Status, byDefinition["examples"].Status)
	}
	if byDefinition["integration"].Status != domain.TaskStatusPending {
		t.Fatalf("integration task: got %q, want pending", byDefinition["integration"].Status)
	}
	if len(byDefinition["docs"].TransitionDecisions) != 1 || byDefinition["docs"].TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("docs transition decision: %#v", byDefinition["docs"].TransitionDecisions)
	}
	if len(byDefinition["examples"].TransitionDecisions) != 1 || byDefinition["examples"].TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("examples transition decision: %#v", byDefinition["examples"].TransitionDecisions)
	}
}

func TestWorkflowSkipIntentWaitsForOptionalSkipReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	definition.ID = "reviewed-skip-intent"
	definition.Name = "Reviewed skip intent"
	definition.Graph.Tasks[1].ReviewPolicy = domain.ReviewRequired
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Reviewed optional skip", Goal: "Reach examples",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:       "exit:implement",
			SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit skip intent: %v", err)
	}
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	docs := byDefinition["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.TransitionDecisions) != 1 || docs.TransitionDecisions[0].AppliedAt != nil {
		t.Fatalf("reviewed planned skip: %#v", docs)
	}
	if _, exists := byDefinition["examples"]; exists {
		t.Fatal("planned successor was created before skip review approval")
	}
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: docs.ID, ReviewID: docs.Reviews[0].ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve planned skip: %v", err)
	}
	byDefinition = workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["examples"].Status != domain.TaskStatusPending {
		t.Fatalf("planned successor after review: got %q", byDefinition["examples"].Status)
	}
}

func TestWorkflowSkipIntentRejectsTaskBehindKeptOptionalTask(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Incomplete plan", Goal: "Reject incomplete skip planning",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	_, err = service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:       "exit:implement",
			SkipOptionalTaskIDs: []domain.WorkflowTaskID{"examples"},
		},
	})
	if err == nil {
		t.Fatal("submit unreachable skip intent: got nil error")
	}
}

func TestWorkflowSkipIntentAdvancesParallelOptionalBranches(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := branchingOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Parallel optional branches", Goal: "Reach both publishers",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit branching plan: %v", err)
	}
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["docs"].Status != domain.TaskStatusSkipped || byDefinition["examples"].Status != domain.TaskStatusSkipped {
		t.Fatalf("optional branch status: docs=%s examples=%s", byDefinition["docs"].Status, byDefinition["examples"].Status)
	}
	if byDefinition["publish-docs"].Status != domain.TaskStatusPending || byDefinition["publish-examples"].Status != domain.TaskStatusPending {
		t.Fatalf(
			"published branch status: docs=%s examples=%s",
			byDefinition["publish-docs"].Status,
			byDefinition["publish-examples"].Status,
		)
	}
}

func TestWorkflowSkipIntentJoinsOptionalBranchesIntoOneTask(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Joined optional branches", Goal: "Reach final verification",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:start",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"b", "c", "d"},
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Planning complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit joining plan: %v", err)
	}

	tasks := repository.tasksFor(workItem.ID)
	counts := make(map[domain.WorkflowTaskID]int)
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			counts[*task.WorkflowTaskID]++
		}
	}
	for _, taskID := range []domain.WorkflowTaskID{"start", "b", "c", "d", "final"} {
		if counts[taskID] != 1 {
			t.Fatalf("runtime task %q count: got %d, want 1", taskID, counts[taskID])
		}
	}
	byDefinition := workflowTasksByDefinition(tasks)
	if byDefinition["b"].Status != domain.TaskStatusSkipped || byDefinition["c"].Status != domain.TaskStatusSkipped || byDefinition["d"].Status != domain.TaskStatusSkipped {
		t.Fatalf("joined skip states: b=%s c=%s d=%s", byDefinition["b"].Status, byDefinition["c"].Status, byDefinition["d"].Status)
	}
	if byDefinition["final"].Status != domain.TaskStatusPending {
		t.Fatalf("final task status: got %s, want pending", byDefinition["final"].Status)
	}
}

func TestWorkflowSkipIntentFanInKeepsTaskWhenOnePredecessorKeeps(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Keep joined task", Goal: "Preserve one requested task",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{ChoiceGroupID: "exit:start"})
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	completeWorkflowTask(t, service, repository, agent, byDefinition["b"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:b", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"d"},
	})
	if _, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]; exists {
		t.Fatal("joined task was created before all predecessors decided")
	}
	completeWorkflowTask(t, service, repository, agent, byDefinition["c"], WorkflowTransitionCommand{ChoiceGroupID: "exit:c"})
	d := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]
	if d.Status != domain.TaskStatusPending {
		t.Fatalf("joined task status: got %s, want pending", d.Status)
	}
}

func completeWorkflowTask(
	t *testing.T,
	service *Service,
	repository *testRepository,
	identity Identity,
	task domain.Task,
	transition WorkflowTransitionCommand,
) {
	t.Helper()
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("claim task %s: %v", task.ID, err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: identity, Result: "Completed " + task.Title,
		Transition: &transition,
	}); err != nil {
		t.Fatalf("submit task %s: %v", task.ID, err)
	}
}

func workflowTasksByDefinition(tasks []domain.Task) map[domain.WorkflowTaskID]domain.Task {
	result := make(map[domain.WorkflowTaskID]domain.Task, len(tasks))
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			result[*task.WorkflowTaskID] = task
		}
	}
	return result
}

func workflowDefinitionTaskIDs(tasks []domain.WorkflowTaskDefinition) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, len(tasks))
	for index, task := range tasks {
		result[index] = task.ID
	}
	return result
}

func runtimeWorkflowTaskIDs(tasks []domain.Task) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, 0, len(tasks))
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			result = append(result, *task.WorkflowTaskID)
		}
	}
	return result
}

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type testIDs struct{ next int }

func (g *testIDs) NewID() string {
	g.next++
	return fmt.Sprintf("generated-%d", g.next)
}

func newTestService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := NewService(repository, testClock{now: applicationTestTime}, &testIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func workflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "workflow", Version: 1, Name: "Development", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{
					ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent,
					AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired,
					ReviewPolicy: domain.ReviewNone, DefaultTags: []string{"backend"},
				},
			},
		},
	}
}

func reviewedWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "reviewed-workflow", Version: 1, Name: "Reviewed development", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired},
				{ID: "test", Title: "Test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-test", FromTaskID: "implement", ToTaskID: "test"},
			},
		},
	}
}

func joiningWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "joining-workflow", Version: 1, Name: "Joining workflow", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"start"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "start", Title: "Start", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "b", Title: "B", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "c", Title: "C", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "d", Title: "D", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "start-b", FromTaskID: "start", ToTaskID: "b"},
				{ID: "start-c", FromTaskID: "start", ToTaskID: "c"},
				{ID: "b-d", FromTaskID: "b", ToTaskID: "d"},
				{ID: "c-d", FromTaskID: "c", ToTaskID: "d"},
			},
		},
	}
}

func optionalReviewWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "optional-review-workflow", Version: 1, Name: "Optional review", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewRequired},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
			},
		},
	}
}

func consecutiveOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "consecutive-optional-workflow", Version: 1, Name: "Consecutive optional tasks", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "examples", Title: "Examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "integration", Title: "Integration test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "docs-examples", FromTaskID: "docs", ToTaskID: "examples"},
				{ID: "examples-integration", FromTaskID: "examples", ToTaskID: "integration"},
			},
		},
	}
}

func branchingOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "branching-optional-workflow", Version: 1, Name: "Branching optional tasks", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "examples", Title: "Examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "publish-docs", Title: "Publish documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "publish-examples", Title: "Publish examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "implement-examples", FromTaskID: "implement", ToTaskID: "examples"},
				{ID: "docs-publish", FromTaskID: "docs", ToTaskID: "publish-docs"},
				{ID: "examples-publish", FromTaskID: "examples", ToTaskID: "publish-examples"},
			},
		},
	}
}

func joiningOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "joining-optional-workflow", Version: 1, Name: "Joining optional workflow", Status: domain.DefinitionStatusPublished,
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"start"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "start", Title: "Start", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "b", Title: "Optional B", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "c", Title: "Optional C", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "d", Title: "Joined optional D", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "final", Title: "Final verification", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "start-b", FromTaskID: "start", ToTaskID: "b"},
				{ID: "start-c", FromTaskID: "start", ToTaskID: "c"},
				{ID: "b-d", FromTaskID: "b", ToTaskID: "d"},
				{ID: "c-d", FromTaskID: "c", ToTaskID: "d"},
				{ID: "d-final", FromTaskID: "d", ToTaskID: "final"},
			},
		},
	}
}

func blackboardDefinition() domain.BlackboardDefinition {
	return domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{
		ID: "blackboard", Version: 1, Name: "Open collaboration", Status: domain.DefinitionStatusPublished,
		CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
	}}
}

func definitionKey(id domain.DefinitionID, version int64) string {
	return fmt.Sprintf("%s:%d", id, version)
}

type testRepository struct {
	workItems   map[domain.WorkItemID]domain.WorkItem
	tasks       map[domain.TaskID]domain.Task
	relations   []domain.TaskRelation
	claims      map[domain.ClaimID]domain.Claim
	activations map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation
	events      []domain.WorkItemEvent
	workflows   map[string]domain.WorkflowDefinition
	blackboards map[string]domain.BlackboardDefinition
	idempotency map[string]IdempotencyRecord
}

func newTestRepository() *testRepository {
	return &testRepository{
		workItems:   make(map[domain.WorkItemID]domain.WorkItem),
		tasks:       make(map[domain.TaskID]domain.Task),
		claims:      make(map[domain.ClaimID]domain.Claim),
		activations: make(map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation),
		workflows:   make(map[string]domain.WorkflowDefinition),
		blackboards: make(map[string]domain.BlackboardDefinition),
		idempotency: make(map[string]IdempotencyRecord),
	}
}

func (r *testRepository) View(_ context.Context, operation func(ReadStore) error) error {
	return operation(r)
}

func (r *testRepository) Update(_ context.Context, operation func(WriteStore) error) error {
	return operation(r)
}

func (r *testRepository) GetWorkItem(id domain.WorkItemID) (domain.WorkItem, error) {
	value, ok := r.workItems[id]
	if !ok {
		return domain.WorkItem{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) GetTask(id domain.TaskID) (domain.Task, error) {
	value, ok := r.tasks[id]
	if !ok {
		return domain.Task{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListTasks(workItemID domain.WorkItemID) ([]domain.Task, error) {
	return r.tasksFor(workItemID), nil
}

func (r *testRepository) tasksFor(workItemID domain.WorkItemID) []domain.Task {
	var result []domain.Task
	for _, task := range r.tasks {
		if task.WorkItemID == workItemID {
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Position < result[j].Position })
	return result
}

func (r *testRepository) ListTaskRelations(workItemID domain.WorkItemID) ([]domain.TaskRelation, error) {
	var result []domain.TaskRelation
	for _, relation := range r.relations {
		if relation.WorkItemID == workItemID {
			result = append(result, relation)
		}
	}
	return result, nil
}

func (r *testRepository) ListClaims(taskID domain.TaskID) ([]domain.Claim, error) {
	var result []domain.Claim
	for _, claim := range r.claims {
		if claim.TaskID == taskID {
			result = append(result, claim)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ClaimedAt.Before(result[j].ClaimedAt) })
	return result, nil
}

func (r *testRepository) GetWorkflowTaskActivation(id domain.WorkflowTaskActivationID) (domain.WorkflowTaskActivation, error) {
	value, ok := r.activations[id]
	if !ok {
		return domain.WorkflowTaskActivation{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListWorkflowTaskActivations(workItemID domain.WorkItemID) ([]domain.WorkflowTaskActivation, error) {
	var result []domain.WorkflowTaskActivation
	for _, activation := range r.activations {
		if activation.WorkItemID == workItemID {
			result = append(result, activation)
		}
	}
	return result, nil
}

func (r *testRepository) ListOpenTasks() ([]WorkCandidate, error) {
	var result []WorkCandidate
	for _, task := range r.tasks {
		workItem := r.workItems[task.WorkItemID]
		if workItem.Status == domain.WorkItemStatusOpen {
			result = append(result, WorkCandidate{Kind: WorkCandidateTask, WorkItem: workItem, Task: task})
		}
	}
	return result, nil
}

func (r *testRepository) ListEmptyBlackboards() ([]domain.WorkItem, error) {
	var result []domain.WorkItem
	for _, workItem := range r.workItems {
		if workItem.Status == domain.WorkItemStatusOpen &&
			workItem.CoordinationMode() == domain.CoordinationModeBlackboard &&
			len(r.tasksFor(workItem.ID)) == 0 {
			result = append(result, workItem)
		}
	}
	return result, nil
}

func (r *testRepository) GetWorkflowDefinition(id domain.DefinitionID, version int64) (domain.WorkflowDefinition, error) {
	value, ok := r.workflows[definitionKey(id, version)]
	if !ok {
		return domain.WorkflowDefinition{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) GetBlackboardDefinition(id domain.DefinitionID, version int64) (domain.BlackboardDefinition, error) {
	value, ok := r.blackboards[definitionKey(id, version)]
	if !ok {
		return domain.BlackboardDefinition{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListWorkflowDefinitions() ([]domain.WorkflowDefinition, error) {
	result := make([]domain.WorkflowDefinition, 0, len(r.workflows))
	for _, definition := range r.workflows {
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == result[j].ID {
			return result[i].Version < result[j].Version
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *testRepository) ListBlackboardDefinitions() ([]domain.BlackboardDefinition, error) {
	result := make([]domain.BlackboardDefinition, 0, len(r.blackboards))
	for _, definition := range r.blackboards {
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == result[j].ID {
			return result[i].Version < result[j].Version
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *testRepository) LastWorkItemEventSequence(workItemID domain.WorkItemID) (int64, error) {
	var sequence int64
	for _, event := range r.events {
		if event.WorkItemID == workItemID && event.Sequence > sequence {
			sequence = event.Sequence
		}
	}
	return sequence, nil
}

func (r *testRepository) GetIdempotencyRecord(actor domain.ActorRef, operationID string) (IdempotencyRecord, error) {
	value, ok := r.idempotency[idempotencyTestKey(actor, operationID)]
	if !ok {
		return IdempotencyRecord{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) CreateWorkflowDefinition(value domain.WorkflowDefinition) error {
	key := definitionKey(value.ID, value.Version)
	if _, exists := r.workflows[key]; exists {
		return ErrConflict
	}
	r.workflows[key] = value
	return nil
}

func (r *testRepository) CreateBlackboardDefinition(value domain.BlackboardDefinition) error {
	key := definitionKey(value.ID, value.Version)
	if _, exists := r.blackboards[key]; exists {
		return ErrConflict
	}
	r.blackboards[key] = value
	return nil
}

func (r *testRepository) CreateWorkItem(value domain.WorkItem) error {
	if _, exists := r.workItems[value.ID]; exists {
		return ErrConflict
	}
	r.workItems[value.ID] = value
	return nil
}

func (r *testRepository) SaveWorkItem(value domain.WorkItem) error {
	if _, exists := r.workItems[value.ID]; !exists {
		return ErrNotFound
	}
	r.workItems[value.ID] = value
	return nil
}

func (r *testRepository) CreateTask(value domain.Task) error {
	if _, exists := r.tasks[value.ID]; exists {
		return ErrConflict
	}
	r.tasks[value.ID] = value
	return nil
}

func (r *testRepository) SaveTask(value domain.Task) error {
	if _, exists := r.tasks[value.ID]; !exists {
		return ErrNotFound
	}
	r.tasks[value.ID] = value
	return nil
}

func (r *testRepository) CreateTaskRelation(value domain.TaskRelation) error {
	r.relations = append(r.relations, value)
	return nil
}

func (r *testRepository) CreateWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if _, exists := r.activations[value.ID]; exists {
		return ErrConflict
	}
	r.activations[value.ID] = value
	return nil
}

func (r *testRepository) SaveWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if _, exists := r.activations[value.ID]; !exists {
		return ErrNotFound
	}
	r.activations[value.ID] = value
	return nil
}

func (r *testRepository) CreateClaim(value domain.Claim) error {
	if _, exists := r.claims[value.ID]; exists {
		return ErrConflict
	}
	r.claims[value.ID] = value
	return nil
}

func (r *testRepository) SaveClaim(value domain.Claim) error {
	if _, exists := r.claims[value.ID]; !exists {
		return ErrNotFound
	}
	r.claims[value.ID] = value
	return nil
}

func (r *testRepository) AppendWorkItemEvent(value domain.WorkItemEvent) error {
	r.events = append(r.events, value)
	return nil
}

func (r *testRepository) LockIdempotencyKey(domain.ActorRef, string) error { return nil }

func (r *testRepository) CreateIdempotencyRecord(value IdempotencyRecord) error {
	key := idempotencyTestKey(value.Actor, value.OperationID)
	if _, exists := r.idempotency[key]; exists {
		return ErrConflict
	}
	r.idempotency[key] = value
	return nil
}

func idempotencyTestKey(actor domain.ActorRef, operationID string) string {
	return string(actor.Kind) + ":" + string(actor.ID) + ":" + operationID
}
