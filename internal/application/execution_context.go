package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// DefinitionExecutionContext contains collaboration guidance shared by both modes.
type DefinitionExecutionContext struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	AgentInstructions string   `json:"agent_instructions"`
	SuggestedTags     []string `json:"suggested_tags"`
}

// WorkflowChoiceOption describes one legal progression choice for the current Task.
type WorkflowChoiceOption struct {
	ID   domain.WorkflowChoiceGroupID   `json:"id"`
	Kind domain.WorkflowChoiceGroupKind `json:"kind"`

	// Targets are the selected group's direct Task Definitions.
	Targets []domain.WorkflowTaskDefinition `json:"targets"`

	// Relations retain the authored guidance for each direct target. Guidance
	// describes choices already permitted by the compiled Workflow; it does not
	// make an otherwise automatic relation conditional.
	Relations []WorkflowChoiceRelation `json:"relations"`

	// SkippableOptionalTasks are reachable without crossing a Task that must execute.
	// Executors submit only the IDs they intend to skip.
	SkippableOptionalTasks []domain.WorkflowTaskDefinition `json:"skippable_optional_tasks"`
}

// WorkflowChoiceRelation describes one direct relation in a legal choice group.
type WorkflowChoiceRelation struct {
	RelationID    domain.WorkflowRelationID     `json:"relation_id"`
	Target        domain.WorkflowTaskDefinition `json:"target"`
	Label         string                        `json:"label"`
	AgentGuidance string                        `json:"agent_guidance"`
}

// WorkflowExecutionContext contains the choices derived from the bound Definition.
type WorkflowExecutionContext struct {
	// UpstreamTasks are ordered nearest-first and include skipped ancestors, so
	// the executor can reach the durable results that feed this Task.
	UpstreamTasks []domain.Task `json:"upstream_tasks"`

	ChoiceGroups []WorkflowChoiceOption `json:"choice_groups"`
}

// BlackboardExecutionContext contains the other Tasks in the current shared
// space. The Task being executed is available separately on TaskExecutionContext.
type BlackboardExecutionContext struct {
	CurrentTaskID domain.TaskID         `json:"current_task_id"`
	Tasks         []domain.Task         `json:"tasks"`
	Relations     []domain.TaskRelation `json:"relations"`
	CanDecompose  bool                  `json:"can_decompose"`
}

// TaskExecutionContext contains the durable context needed to execute one Task.
// Task owns its complete Submission, Review, Failure, and Transition histories.
type TaskExecutionContext struct {
	WorkItem          domain.WorkItem             `json:"work_item"`
	Task              domain.Task                 `json:"task"`
	Claims            []domain.Claim              `json:"claims"`
	Artifacts         []domain.Artifact           `json:"artifacts"`
	ExpectedArtifacts []domain.ArtifactDefinition `json:"expected_artifacts"`
	Definition        DefinitionExecutionContext  `json:"definition"`
	Responsibility    TaskResponsibility          `json:"responsibility"`
	Outcome           TaskOutcome                 `json:"outcome"`

	Workflow   *WorkflowExecutionContext   `json:"workflow"`
	Blackboard *BlackboardExecutionContext `json:"blackboard"`
}

type TaskResponsibility struct {
	Kind  string           `json:"kind"`
	Actor *domain.ActorRef `json:"actor"`
}

type TaskOutcome struct {
	Kind       string           `json:"kind"`
	Actor      *domain.ActorRef `json:"actor"`
	Reason     string           `json:"reason"`
	OccurredAt *time.Time       `json:"occurred_at"`
}

// GetTaskExecutionContextQuery identifies the Task and requesting executor.
type GetTaskExecutionContextQuery struct {
	TaskID   domain.TaskID
	Identity Identity
}

// GetTaskExecutionContext returns one executor-facing view without exposing
// Workflow relation planning as a client-authored graph.
func (s *Service) GetTaskExecutionContext(
	ctx context.Context,
	query GetTaskExecutionContextQuery,
) (TaskExecutionContext, error) {
	if strings.TrimSpace(string(query.TaskID)) == "" {
		return TaskExecutionContext{}, invalidCommand("task id is required")
	}
	if err := query.Identity.Validate(); err != nil {
		return TaskExecutionContext{}, err
	}

	var result TaskExecutionContext
	err := s.repository.View(ctx, func(store ReadStore) error {
		task, err := store.GetTask(query.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", query.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if err := identityCanExecute(query.Identity, task); err != nil {
			return err
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		if err := executionContextVisibleTo(query.Identity, task, claims); err != nil {
			return err
		}

		artifacts, err := store.ListArtifacts(workItem.ID)
		if err != nil {
			return fmt.Errorf("list artifacts for work item %q: %w", workItem.ID, err)
		}
		visibleArtifacts := make([]domain.Artifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			if artifact.SubmissionID != nil || claimOwnedByIdentity(claims, artifact.ClaimID, query.Identity) {
				visibleArtifacts = append(visibleArtifacts, artifact)
			}
		}
		result = TaskExecutionContext{WorkItem: workItem, Task: task, Claims: claims, Artifacts: visibleArtifacts}
		switch workItem.CoordinationMode() {
		case domain.CoordinationModeWorkflow:
			definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
			if err != nil {
				return fmt.Errorf("get workflow definition: %w", err)
			}
			result.Definition = definitionExecutionContext(definition.DefinitionMetadata)
			workflowTask, exists := workflowTaskDefinition(definition, *task.WorkflowTaskID)
			if !exists {
				return invalidCommand("workflow task definition %q does not exist", *task.WorkflowTaskID)
			}
			result.ExpectedArtifacts = append([]domain.ArtifactDefinition{}, workflowTask.Artifacts...)
			workflow, err := workflowExecutionContext(definition, task)
			if err != nil {
				return err
			}
			workflow.UpstreamTasks, err = workflowUpstreamTasks(store, workItem.ID, task.ID)
			if err != nil {
				return err
			}
			result.Workflow = &workflow
		case domain.CoordinationModeBlackboard:
			definition, err := store.GetBlackboardDefinition(workItem.Definition.ID, workItem.Definition.Version)
			if err != nil {
				return fmt.Errorf("get blackboard definition: %w", err)
			}
			result.Definition = definitionExecutionContext(definition.DefinitionMetadata)
			tasks, err := store.ListTasks(workItem.ID)
			if err != nil {
				return fmt.Errorf("list blackboard tasks: %w", err)
			}
			otherTasks := make([]domain.Task, 0, len(tasks))
			for _, sharedTask := range tasks {
				if sharedTask.ID != task.ID {
					otherTasks = append(otherTasks, sharedTask)
				}
			}
			relations, err := store.ListTaskRelations(workItem.ID)
			if err != nil {
				return fmt.Errorf("list blackboard relations: %w", err)
			}
			if relations == nil {
				relations = []domain.TaskRelation{}
			}
			canDecompose := false
			if task.ActiveClaimID != nil {
				canDecompose = validateBlackboardTaskDecomposition(workItem, task, claims, query.Identity, *task.ActiveClaimID) == nil
			}
			result.Blackboard = &BlackboardExecutionContext{CurrentTaskID: task.ID, Tasks: otherTasks, Relations: relations, CanDecompose: canDecompose}
		default:
			return invalidCommand("work item %q has invalid coordination mode", workItem.ID)
		}
		return nil
	})
	if err != nil {
		return TaskExecutionContext{}, err
	}
	result.WorkItem = normalizeWorkItemCollections(result.WorkItem)
	result.Responsibility, result.Outcome = projectTaskLifecycle(result.Task, result.Claims)
	result.Task = normalizeTaskCollections(result.Task)
	if result.Artifacts == nil {
		result.Artifacts = []domain.Artifact{}
	}
	if result.ExpectedArtifacts == nil {
		result.ExpectedArtifacts = []domain.ArtifactDefinition{}
	}
	if result.Claims == nil {
		result.Claims = []domain.Claim{}
	}
	result.Definition = normalizeDefinitionContext(result.Definition)
	if result.Workflow != nil {
		result.Workflow.UpstreamTasks = normalizeTasks(result.Workflow.UpstreamTasks)
		result.Workflow.ChoiceGroups = normalizeWorkflowChoiceOptions(result.Workflow.ChoiceGroups)
	}
	if result.Blackboard != nil {
		result.Blackboard.Tasks = normalizeTasks(result.Blackboard.Tasks)
		if result.Blackboard.Relations == nil {
			result.Blackboard.Relations = []domain.TaskRelation{}
		}
	}
	return result, nil
}

func claimOwnedByIdentity(claims []domain.Claim, claimID domain.ClaimID, identity Identity) bool {
	for _, claim := range claims {
		if claim.ID == claimID && claim.Active() && sameActor(claim.Executor, identity.Actor) {
			return true
		}
	}
	return false
}

func projectTaskLifecycle(task domain.Task, claims []domain.Claim) (TaskResponsibility, TaskOutcome) {
	findClaimActor := func(id domain.ClaimID) *domain.ActorRef {
		for _, claim := range claims {
			if claim.ID == id {
				actor := claim.Executor
				return &actor
			}
		}
		return nil
	}
	if task.Status == domain.TaskStatusSkipped {
		return TaskResponsibility{Kind: "skipped_by", Actor: task.SkippedBy}, TaskOutcome{Kind: "skipped", Actor: task.SkippedBy, Reason: task.SkipReason, OccurredAt: task.CompletedAt}
	}
	if task.Status == domain.TaskStatusWorking && task.ActiveClaimID != nil {
		return TaskResponsibility{Kind: "claimed_by", Actor: findClaimActor(*task.ActiveClaimID)}, TaskOutcome{Kind: "active"}
	}
	if task.Status == domain.TaskStatusInReview {
		if len(task.Submissions) > 0 {
			submission := task.Submissions[len(task.Submissions)-1]
			return TaskResponsibility{Kind: "submitted_by", Actor: findClaimActor(submission.ClaimID)}, TaskOutcome{Kind: "in_review"}
		}
		if len(task.TransitionDecisions) > 0 {
			actor := task.TransitionDecisions[len(task.TransitionDecisions)-1].DecidedBy
			return TaskResponsibility{Kind: "skip_requested_by", Actor: &actor}, TaskOutcome{Kind: "in_review"}
		}
		return TaskResponsibility{Kind: "review_requested_by"}, TaskOutcome{Kind: "in_review"}
	}
	if task.Status == domain.TaskStatusFailed && len(task.Failures) > 0 {
		failure := task.Failures[len(task.Failures)-1]
		return TaskResponsibility{Kind: "failed_by", Actor: findClaimActor(failure.ClaimID)}, TaskOutcome{Kind: "failed", Reason: failure.Reason, OccurredAt: &failure.FailedAt}
	}
	if task.Status == domain.TaskStatusWaitingChildren {
		for _, claim := range claims {
			if claim.EndReason == domain.ClaimEndTaskDecomposed {
				actor := claim.Executor
				return TaskResponsibility{Kind: "decomposed_by", Actor: &actor}, TaskOutcome{Kind: "waiting_children", OccurredAt: task.DecomposedAt}
			}
		}
	}
	if task.Status == domain.TaskStatusCompleted {
		if len(task.Submissions) > 0 {
			submission := task.Submissions[len(task.Submissions)-1]
			return TaskResponsibility{Kind: "executed_by", Actor: findClaimActor(submission.ClaimID)}, TaskOutcome{Kind: "completed", OccurredAt: task.CompletedAt}
		}
		for _, claim := range claims {
			if claim.EndReason == domain.ClaimEndTaskDecomposed {
				actor := claim.Executor
				return TaskResponsibility{Kind: "decomposed_by", Actor: &actor}, TaskOutcome{Kind: "completed", OccurredAt: task.CompletedAt}
			}
		}
	}
	return TaskResponsibility{Kind: "unclaimed"}, TaskOutcome{Kind: "pending"}
}

func workflowUpstreamTasks(
	store ReadStore,
	workItemID domain.WorkItemID,
	taskID domain.TaskID,
) ([]domain.Task, error) {
	tasks, err := store.ListTasks(workItemID)
	if err != nil {
		return nil, fmt.Errorf("list workflow tasks: %w", err)
	}
	relations, err := store.ListTaskRelations(workItemID)
	if err != nil {
		return nil, fmt.Errorf("list workflow relations: %w", err)
	}
	byID := make(map[domain.TaskID]domain.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	visited := map[domain.TaskID]struct{}{taskID: {}}
	queue := []domain.TaskID{taskID}
	var upstream []domain.Task
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, relation := range relations {
			if relation.ToTaskID != current {
				continue
			}
			if _, exists := visited[relation.FromTaskID]; exists {
				continue
			}
			predecessor, exists := byID[relation.FromTaskID]
			if !exists {
				return nil, invalidCommand("relation references missing task %q", relation.FromTaskID)
			}
			visited[predecessor.ID] = struct{}{}
			upstream = append(upstream, predecessor)
			queue = append(queue, predecessor.ID)
		}
	}
	return upstream, nil
}

func definitionExecutionContext(metadata domain.DefinitionMetadata) DefinitionExecutionContext {
	return DefinitionExecutionContext{
		Name:              metadata.Name,
		Description:       metadata.Description,
		AgentInstructions: metadata.AgentInstructions,
		SuggestedTags:     append([]string(nil), metadata.SuggestedTags...),
	}
}

func executionContextVisibleTo(identity Identity, task domain.Task, claims []domain.Claim) error {
	if task.Status != domain.TaskStatusWorking {
		return nil
	}
	if task.ActiveClaimID == nil {
		return conflict("working task %q has no active claim", task.ID)
	}
	claimIndex := findClaim(claims, *task.ActiveClaimID)
	if claimIndex < 0 {
		return conflict("active claim %q does not exist for task %q", *task.ActiveClaimID, task.ID)
	}
	if !sameActor(claims[claimIndex].Executor, identity.Actor) {
		return forbidden("actor does not own active claim %q", *task.ActiveClaimID)
	}
	return nil
}

func workflowExecutionContext(
	definition domain.WorkflowDefinition,
	task domain.Task,
) (WorkflowExecutionContext, error) {
	if task.WorkflowTaskID == nil {
		return WorkflowExecutionContext{}, invalidCommand("task %q has no workflow task id", task.ID)
	}
	compiled, err := definition.CompileGraph()
	if err != nil {
		return WorkflowExecutionContext{}, err
	}
	groups := compiled.GroupsFor(*task.WorkflowTaskID)
	context := WorkflowExecutionContext{ChoiceGroups: make([]WorkflowChoiceOption, 0, len(groups))}
	for _, group := range groups {
		option := WorkflowChoiceOption{ID: group.ID, Kind: group.Kind}
		for _, relationID := range group.RelationIDs {
			relation, exists := workflowRelationDefinition(definition, relationID)
			if !exists {
				return WorkflowExecutionContext{}, invalidCommand("workflow relation %q does not exist", relationID)
			}
			target, exists := workflowTaskDefinition(definition, relation.ToTaskID)
			if !exists {
				return WorkflowExecutionContext{}, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
			}
			option.Targets = append(option.Targets, target)
			option.Relations = append(option.Relations, WorkflowChoiceRelation{
				RelationID: relation.ID, Target: target, Label: relation.Label, AgentGuidance: relation.AgentGuidance,
			})
		}
		if group.Kind == domain.WorkflowChoiceGroupExit {
			candidates, err := reachableOptionalTasks(definition, compiled, group)
			if err != nil {
				return WorkflowExecutionContext{}, err
			}
			option.SkippableOptionalTasks = candidates
		}
		context.ChoiceGroups = append(context.ChoiceGroups, option)
	}
	return context, nil
}

func reachableOptionalTasks(
	definition domain.WorkflowDefinition,
	compiled domain.CompiledWorkflowGraph,
	root domain.WorkflowChoiceGroup,
) ([]domain.WorkflowTaskDefinition, error) {
	queuedRelations := append([]domain.WorkflowRelationID(nil), root.RelationIDs...)
	visited := make(map[domain.WorkflowTaskID]struct{})
	var candidates []domain.WorkflowTaskDefinition
	for len(queuedRelations) > 0 {
		relationID := queuedRelations[0]
		queuedRelations = queuedRelations[1:]
		relation, exists := workflowRelationDefinition(definition, relationID)
		if !exists {
			return nil, invalidCommand("workflow relation %q does not exist", relationID)
		}
		target, exists := workflowTaskDefinition(definition, relation.ToTaskID)
		if !exists {
			return nil, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
		}
		if target.Execution != domain.ExecutionOptional {
			continue
		}
		if _, exists := visited[target.ID]; exists {
			continue
		}
		visited[target.ID] = struct{}{}
		candidates = append(candidates, target)
		exitGroup, exists := workflowExitChoiceGroup(compiled, target.ID)
		if exists {
			queuedRelations = append(queuedRelations, exitGroup.RelationIDs...)
		}
	}
	return candidates, nil
}
