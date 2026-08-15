package repository

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

const collaborationSimulationSeed int64 = 20260814

func TestRandomAgentCollaboration(t *testing.T) {
	t.Run("workflow", testRandomWorkflowCollaboration)
	t.Run("blackboard", testRandomBlackboardCollaboration)
}

func testRandomWorkflowCollaboration(t *testing.T) {
	simulation := newCollaborationSimulation(t, collaborationSimulationSeed)
	definition := simulationWorkflowDefinition()
	if err := simulation.repository.CreateWorkflowDefinition(simulation.ctx, definition); err != nil {
		t.Fatalf("create workflow definition: %v", err)
	}
	creator := simulationAgent("creator", "architect")
	workItem, err := simulation.service.CreateWorkItem(simulation.ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(),
		Identity:   creator.identity,
		Title:      "Implement login",
		Goal:       "Deliver a reviewed login implementation",
	})
	if err != nil {
		t.Fatalf("create workflow work item: %v", err)
	}
	simulation.record(
		"workflow %q (version=%d), work item %q, goal=%q",
		definition.Name,
		definition.Version,
		workItem.Title,
		workItem.Goal,
	)
	simulation.record(
		`workflow graph: start %q; %q -> %q -> %q (optional) -> %q`,
		"Design login",
		"Design login",
		"Implement login",
		"Document login",
		"Test login",
	)
	simulation.record(
		"workflow suggested tags=%v, instructions=%q",
		definition.SuggestedTags,
		definition.AgentInstructions,
	)

	agents := []simulatedAgent{
		simulationAgent("architect-agent", "architect"),
		simulationAgent("backend-agent", "backend"),
		simulationAgent("writer-agent", "writer"),
		simulationAgent("qa-agent", "qa"),
	}
	simulation.run(workItem.ID, agents)

	workItem, tasks, _ := simulation.snapshot(workItem.ID)
	if workItem.Status != domain.WorkItemStatusCompleted {
		t.Fatalf("workflow status: got %s", workItem.Status)
	}
	byDefinition := make(map[domain.WorkflowTaskID]domain.Task, len(tasks))
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			byDefinition[*task.WorkflowTaskID] = task
		}
	}
	if len(byDefinition["implement"].Reviews) == 0 {
		t.Fatal("workflow implementation did not pass through human review")
	}
	if byDefinition["docs"].Status != domain.TaskStatusSkipped {
		t.Fatalf("fixed seed should skip optional docs, got %s", byDefinition["docs"].Status)
	}
	simulation.logTrace()
}

func testRandomBlackboardCollaboration(t *testing.T) {
	simulation := newCollaborationSimulation(t, collaborationSimulationSeed+1)
	definition := simulationBlackboardDefinition()
	if err := simulation.repository.CreateBlackboardDefinition(simulation.ctx, definition); err != nil {
		t.Fatalf("create blackboard definition: %v", err)
	}
	planner := simulationAgent("planner-agent", "planner", "planning")
	workItem, err := simulation.service.CreateWorkItem(simulation.ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(),
		Identity:   planner.identity,
		Title:      "Implement login",
		Goal:       "Plan and deliver login collaboratively",
		Tags:       []string{"planning"},
	})
	if err != nil {
		t.Fatalf("create blackboard work item: %v", err)
	}
	simulation.record(
		"kanban for work item %q, blackboard %q (version=%d), goal=%q",
		workItem.Title,
		definition.Name,
		definition.Version,
		workItem.Goal,
	)
	simulation.record(
		"blackboard suggested tags=%v, instructions=%q",
		definition.SuggestedTags,
		definition.AgentInstructions,
	)
	simulation.record("kanban initial state: no tasks, work item tags=%v", workItem.Tags)

	backend := simulationAgent("backend-agent", "backend", "module:auth")
	backend.requestReview = true
	writer := simulationAgent("writer-agent", "writer", "docs")
	writer.maySkip = true
	agents := []simulatedAgent{
		planner,
		backend,
		writer,
		simulationAgent("qa-agent", "qa", "test"),
	}
	simulation.run(workItem.ID, agents)

	workItem, tasks, relations := simulation.snapshot(workItem.ID)
	if workItem.Status != domain.WorkItemStatusCompleted {
		t.Fatalf("blackboard status: got %s", workItem.Status)
	}
	if len(tasks) != 4 || len(relations) != 1 {
		t.Fatalf("dynamic plan: tasks=%d relations=%d", len(tasks), len(relations))
	}
	if err := domain.ValidateBlackboardTaskHierarchy(workItem.ID, tasks); err != nil {
		t.Fatalf("dynamic hierarchy: %v", err)
	}
	planning := taskWithTag(t, tasks, "planning")
	if planning.Status != domain.TaskStatusCompleted || planning.DecomposedAt == nil || len(planning.Submissions) != 0 {
		t.Fatalf("planning aggregate: %#v", planning)
	}
	implementation := taskWithTag(t, tasks, "module:auth")
	if len(implementation.Reviews) == 0 {
		t.Fatal("blackboard implementation did not request dynamic review")
	}
	if taskWithTag(t, tasks, "test").Status != domain.TaskStatusCompleted {
		t.Fatal("test task did not complete after its suggested predecessor")
	}
	simulation.logTrace()
}

type simulatedAgent struct {
	identity      application.Identity
	tags          []string
	requestReview bool
	maySkip       bool
}

func simulationAgent(id, role string, tags ...string) simulatedAgent {
	return simulatedAgent{
		identity: application.Identity{
			Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: domain.ActorID(id)},
			Role:  role,
		},
		tags: append([]string(nil), tags...),
	}
}

type collaborationSimulation struct {
	t          *testing.T
	ctx        context.Context
	repository *SQLRepository
	service    *application.Service
	random     *rand.Rand
	seed       int64
	reviewer   application.Identity
	trace      []string

	rejectedReview bool
	planExpanded   bool
}

func newCollaborationSimulation(t *testing.T, seed int64) *collaborationSimulation {
	t.Helper()
	ctx := context.Background()
	repository, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "simulation.db"))
	if err != nil {
		t.Fatalf("open simulation database: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	service := repositoryTestService(t, repository)
	return &collaborationSimulation{
		t:          t,
		ctx:        ctx,
		repository: repository,
		service:    service,
		random:     rand.New(rand.NewSource(seed)),
		seed:       seed,
		reviewer: application.Identity{Actor: domain.ActorRef{
			Kind: domain.ActorHuman,
			ID:   "human-reviewer",
		}},
	}
}

func (s *collaborationSimulation) run(workItemID domain.WorkItemID, agents []simulatedAgent) {
	s.t.Helper()
	const maxSteps = 200
	for step := 0; step < maxSteps; step++ {
		workItem, tasks, _ := s.snapshot(workItemID)
		if workItem.Status == domain.WorkItemStatusCompleted {
			return
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			s.fail("work item ended as %s", workItem.Status)
		}
		if s.decideRandomReview(tasks) {
			s.assertInvariants(workItemID)
			continue
		}
		if workItem.CoordinationMode() == domain.CoordinationModeBlackboard && allTasksTerminal(tasks) {
			completed, err := s.service.CompleteBlackboardWorkItem(s.ctx, application.CompleteBlackboardWorkItemCommand{
				WorkItemID: workItemID,
				Identity:   agents[s.random.Intn(len(agents))].identity,
				Result:     "All planned work completed",
			})
			if err != nil {
				s.fail("complete blackboard: %v", err)
			}
			s.record("completed blackboard %s", completed.ID)
			s.assertInvariants(workItemID)
			continue
		}

		agent := agents[s.random.Intn(len(agents))]
		if s.executeRandomTask(workItemID, agent) {
			s.assertInvariants(workItemID)
		}
	}
	s.fail("exceeded %d steps", maxSteps)
}

func (s *collaborationSimulation) executeRandomTask(
	workItemID domain.WorkItemID,
	agent simulatedAgent,
) bool {
	candidates, err := s.service.FindWork(s.ctx, application.FindWorkQuery{
		Identity: agent.identity,
		Tags:     agent.tags,
	})
	if err != nil {
		s.fail("find work for %s: %v", agent.identity.Actor.ID, err)
	}
	ready := candidates[:0]
	for _, candidate := range candidates {
		if candidate.WorkItem.ID != workItemID {
			continue
		}
		if candidate.Kind == application.WorkCandidateEmptyBlackboard {
			if _, err := s.service.CreateBlackboardTask(s.ctx, application.CreateBlackboardTaskCommand{
				WorkItemID: candidate.WorkItem.ID,
				Identity:   agent.identity, Title: "Plan the implementation",
				Executor: domain.ExecutorAgent, Tags: []string{"planning"},
			}); err != nil {
				s.fail("create first blackboard task: %v", err)
			}
			s.record("%s discovered empty blackboard and added task %q", agent.identity.Actor.ID, "Plan the implementation")
			return true
		}
		if !s.agentAccepts(candidate.Task, agent) {
			continue
		}
		ready = append(ready, candidate)
	}
	if len(ready) == 0 {
		return false
	}
	candidate := ready[s.random.Intn(len(ready))]
	s.record(
		"%s found %d candidate(s), selected task %q",
		agent.identity.Actor.ID,
		len(ready),
		candidate.Task.Title,
	)
	if agent.maySkip && s.random.Intn(2) == 0 {
		if _, err := s.service.SkipBlackboardTask(s.ctx, application.SkipBlackboardTaskCommand{
			TaskID: candidate.Task.ID, Identity: agent.identity, Reason: "Agent judged this task unnecessary",
		}); err != nil {
			s.fail("skip task %s: %v", candidate.Task.ID, err)
		}
		s.record("%s skipped %q", agent.identity.Actor.ID, candidate.Task.Title)
		return true
	}

	claim, err := s.service.ClaimTask(s.ctx, application.ClaimTaskCommand{
		TaskID: candidate.Task.ID, Identity: agent.identity,
	})
	if err != nil {
		s.fail("claim task %s: %v", candidate.Task.ID, err)
	}
	s.record("%s claimed task %q", agent.identity.Actor.ID, candidate.Task.Title)
	executionContext, err := s.service.GetTaskExecutionContext(s.ctx, application.GetTaskExecutionContextQuery{
		TaskID: candidate.Task.ID, Identity: agent.identity,
	})
	if err != nil {
		s.fail("get context for %s: %v", candidate.Task.ID, err)
	}
	if slices.Contains(candidate.Task.Tags, "planning") && !s.planExpanded {
		s.expandBlackboardPlan(candidate.WorkItem.ID, agent.identity, candidate.Task, claim.ID)
		return true
	}
	transition := s.randomWorkflowTransition(executionContext)
	if transition != nil {
		var skippedTitles []string
		for _, group := range executionContext.Workflow.ChoiceGroups {
			if group.ID != transition.ChoiceGroupID {
				continue
			}
			for _, optional := range group.SkippableOptionalTasks {
				if slices.Contains(transition.SkipOptionalTaskIDs, optional.ID) {
					skippedTitles = append(skippedTitles, optional.Title)
				}
			}
			break
		}
		s.record(
			"%s chose workflow group %q, optional skips=%q",
			agent.identity.Actor.ID,
			transition.ChoiceGroupID,
			skippedTitles,
		)
	}
	if _, err := s.service.SubmitTask(s.ctx, application.SubmitTaskCommand{
		TaskID:        candidate.Task.ID,
		ClaimID:       claim.ID,
		Identity:      agent.identity,
		Result:        fmt.Sprintf("Completed by %s", agent.identity.Actor.ID),
		RequestReview: agent.requestReview,
		Transition:    transition,
	}); err != nil {
		s.fail("submit task %s: %v", candidate.Task.ID, err)
	}
	verb := "completed"
	if agent.requestReview ||
		(executionContext.Task.ReviewPolicy != nil && *executionContext.Task.ReviewPolicy == domain.ReviewRequired) {
		verb = "submitted for review"
	}
	s.record("%s %s %q", agent.identity.Actor.ID, verb, candidate.Task.Title)
	return true
}

func (s *collaborationSimulation) agentAccepts(task domain.Task, agent simulatedAgent) bool {
	if task.WorkflowTaskID != nil {
		return true
	}
	view, err := s.service.GetTaskExecutionContext(s.ctx, application.GetTaskExecutionContextQuery{
		TaskID: task.ID, Identity: agent.identity,
	})
	if err != nil {
		s.fail("inspect blackboard task %s: %v", task.ID, err)
	}
	byID := make(map[domain.TaskID]domain.Task, len(view.Blackboard.Tasks))
	for _, existing := range view.Blackboard.Tasks {
		byID[existing.ID] = existing
	}
	for _, relation := range view.Blackboard.Relations {
		if relation.ToTaskID != task.ID {
			continue
		}
		predecessor := byID[relation.FromTaskID]
		if predecessor.Status != domain.TaskStatusCompleted && predecessor.Status != domain.TaskStatusSkipped {
			return false
		}
	}
	return true
}

func (s *collaborationSimulation) randomWorkflowTransition(
	view application.TaskExecutionContext,
) *application.WorkflowTransitionCommand {
	if view.Workflow == nil || len(view.Workflow.ChoiceGroups) == 0 {
		return nil
	}
	group := view.Workflow.ChoiceGroups[s.random.Intn(len(view.Workflow.ChoiceGroups))]
	command := &application.WorkflowTransitionCommand{
		ChoiceGroupID: group.ID,
		Reason:        "Selected by simulated agent",
	}
	for _, optional := range group.SkippableOptionalTasks {
		if s.random.Intn(2) == 0 {
			command.SkipOptionalTaskIDs = append(command.SkipOptionalTaskIDs, optional.ID)
		}
	}
	return command
}

func (s *collaborationSimulation) decideRandomReview(tasks []domain.Task) bool {
	type pendingReview struct {
		review domain.Review
		title  string
	}
	var pending []pendingReview
	for _, task := range tasks {
		if len(task.Reviews) == 0 {
			continue
		}
		review := task.Reviews[len(task.Reviews)-1]
		if review.Status == domain.ReviewStatusPending {
			pending = append(pending, pendingReview{review: review, title: task.Title})
		}
	}
	if len(pending) == 0 {
		return false
	}
	pendingDecision := pending[s.random.Intn(len(pending))]
	review := pendingDecision.review
	decision := domain.ReviewStatusApproved
	feedback := "Approved by simulated reviewer"
	if !s.rejectedReview && s.random.Intn(4) == 0 {
		decision = domain.ReviewStatusRejected
		feedback = "Please address the review feedback"
		s.rejectedReview = true
	}
	if _, err := s.service.DecideReview(s.ctx, application.DecideReviewCommand{
		TaskID: review.TaskID, ReviewID: review.ID, Identity: s.reviewer,
		Decision: decision, Feedback: feedback,
	}); err != nil {
		s.fail("decide review %s: %v", review.ID, err)
	}
	s.record("reviewer decided %s for %q", decision, pendingDecision.title)
	return true
}

func (s *collaborationSimulation) expandBlackboardPlan(
	workItemID domain.WorkItemID,
	planner application.Identity,
	planningTask domain.Task,
	claimID domain.ClaimID,
) {
	type plannedTask struct {
		title string
		tag   string
	}
	plans := []plannedTask{
		{title: "Implement authentication", tag: "module:auth"},
		{title: "Update documentation", tag: "docs"},
		{title: "Run integration tests", tag: "test"},
	}
	permutation := s.random.Perm(len(plans))
	specs := make([]application.BlackboardTaskSpec, 0, len(plans))
	for _, index := range permutation {
		plan := plans[index]
		specs = append(specs, application.BlackboardTaskSpec{
			Title: plan.title, Executor: domain.ExecutorAgent, Tags: []string{plan.tag},
		})
	}
	decomposition, err := s.service.DecomposeBlackboardTask(s.ctx, application.DecomposeBlackboardTaskCommand{
		TaskID: planningTask.ID, ClaimID: claimID, Identity: planner, Children: specs,
	})
	if err != nil {
		s.fail("decompose planning task: %v", err)
	}
	created := make(map[string]domain.Task, len(decomposition.Children))
	for _, task := range decomposition.Children {
		created[task.Tags[0]] = task
	}
	relations := [][2]domain.TaskID{
		{created["module:auth"].ID, created["test"].ID},
	}
	for _, relation := range relations {
		if _, err := s.service.AddBlackboardRelation(s.ctx, application.AddBlackboardRelationCommand{
			WorkItemID: workItemID,
			FromTaskID: relation[0],
			ToTaskID:   relation[1],
			Identity:   planner,
		}); err != nil {
			s.fail("add blackboard relation: %v", err)
		}
	}
	s.planExpanded = true
	s.record(`planner decomposed %q into "Implement authentication", "Update documentation", and "Run integration tests"`, planningTask.Title)
}

func (s *collaborationSimulation) snapshot(
	workItemID domain.WorkItemID,
) (domain.WorkItem, []domain.Task, []domain.TaskRelation) {
	s.t.Helper()
	var workItem domain.WorkItem
	var tasks []domain.Task
	var relations []domain.TaskRelation
	err := s.repository.View(s.ctx, func(store application.ReadStore) error {
		var err error
		workItem, err = store.GetWorkItem(workItemID)
		if err != nil {
			return err
		}
		tasks, err = store.ListTasks(workItemID)
		if err != nil {
			return err
		}
		relations, err = store.ListTaskRelations(workItemID)
		return err
	})
	if err != nil {
		s.fail("read simulation snapshot: %v", err)
	}
	return workItem, tasks, relations
}

func (s *collaborationSimulation) assertInvariants(workItemID domain.WorkItemID) {
	s.t.Helper()
	workItem, tasks, relations := s.snapshot(workItemID)
	if err := workItem.Validate(); err != nil {
		s.fail("invalid work item: %v", err)
	}
	if err := domain.ValidateRuntimeTaskGraph(workItem.ID, tasks, relations); err != nil {
		s.fail("invalid runtime task graph: %v", err)
	}
	err := s.repository.View(s.ctx, func(store application.ReadStore) error {
		for _, task := range tasks {
			claims, err := store.ListClaims(task.ID)
			if err != nil {
				return err
			}
			if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.fail("invalid task context: %v", err)
	}
}

func (s *collaborationSimulation) record(format string, args ...any) {
	s.trace = append(s.trace, fmt.Sprintf(format, args...))
}

func (s *collaborationSimulation) logTrace() {
	s.t.Helper()
	s.t.Logf("seed=%d\n%s", s.seed, strings.Join(s.trace, "\n"))
}

func (s *collaborationSimulation) fail(format string, args ...any) {
	s.t.Helper()
	s.t.Fatalf("seed=%d: %s\ntrace:\n%s", s.seed, fmt.Sprintf(format, args...), strings.Join(s.trace, "\n"))
}

func allTasksTerminal(tasks []domain.Task) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		if task.Status != domain.TaskStatusCompleted && task.Status != domain.TaskStatusSkipped {
			return false
		}
	}
	return true
}

func taskWithTag(t *testing.T, tasks []domain.Task, tag string) domain.Task {
	t.Helper()
	for _, task := range tasks {
		if slices.Contains(task.Tags, tag) {
			return task
		}
	}
	t.Fatalf("task with tag %q not found", tag)
	return domain.Task{}
}

func simulationWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID:                "simulation-workflow",
			Version:           1,
			Name:              "Login delivery workflow",
			AgentInstructions: "Choose legal work and use prior task results.",
			SuggestedTags:     []string{"module:*", "test"},
			Status:            domain.DefinitionStatusPublished,
			CreatedAt:         repositoryTestTime,
			UpdatedAt:         repositoryTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"design"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "design", Title: "Design login", Executor: domain.ExecutorAgent, AllowedRoles: []string{"architect"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "implement", Title: "Implement login", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired},
				{ID: "docs", Title: "Document login", Executor: domain.ExecutorAgent, AllowedRoles: []string{"writer"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "test", Title: "Test login", Executor: domain.ExecutorAgent, AllowedRoles: []string{"qa"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "design-implement", FromTaskID: "design", ToTaskID: "implement"},
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "docs-test", FromTaskID: "docs", ToTaskID: "test"},
			},
		},
	}
}

func simulationBlackboardDefinition() domain.BlackboardDefinition {
	return domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{
		ID:                "simulation-blackboard",
		Version:           1,
		Name:              "Login collaboration blackboard",
		AgentInstructions: "Plan useful tasks, respect suggested dependencies, and close the shared work explicitly.",
		SuggestedTags:     []string{"planning", "module:*", "docs", "test"},
		Status:            domain.DefinitionStatusPublished,
		CreatedAt:         repositoryTestTime,
		UpdatedAt:         repositoryTestTime,
	}}
}
