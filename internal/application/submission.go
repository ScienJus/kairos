package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// WorkflowTransitionCommand contains one progression choice and its Skip Intent.
type WorkflowTransitionCommand struct {
	// ChoiceGroupID selects the current Task's Continue or Exit Group.
	ChoiceGroupID domain.WorkflowChoiceGroupID

	// SkipOptionalTaskIDs may include optional Tasks reachable without crossing
	// a Task that will execute.
	SkipOptionalTaskIDs []domain.WorkflowTaskID

	// ReviewSkippedTaskIDs is a subset of SkipOptionalTaskIDs whose
	// executor_decides policy should request human Review.
	ReviewSkippedTaskIDs []domain.WorkflowTaskID

	Reason string
}

// SubmitTaskCommand submits one immutable result from an active Claim.
type SubmitTaskCommand struct {
	TaskID   domain.TaskID
	ClaimID  domain.ClaimID
	Identity Identity

	Result        string
	ArtifactIDs   []domain.ArtifactID
	RequestReview bool
	Transition    *WorkflowTransitionCommand
}

// SubmitTask ends the active Claim and either completes the Task or requests Review.
func (s *Service) SubmitTask(ctx context.Context, command SubmitTaskCommand) (domain.TaskSubmission, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ClaimID)) == "" {
		return domain.TaskSubmission{}, invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.TaskSubmission{}, err
	}
	if strings.TrimSpace(command.Result) == "" {
		return domain.TaskSubmission{}, invalidCommand("result is required")
	}

	var created domain.TaskSubmission
	err := s.repository.Update(ctx, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		claimIndex := findClaim(claims, command.ClaimID)
		if claimIndex < 0 {
			return fmt.Errorf("%w: claim %q", ErrNotFound, command.ClaimID)
		}
		claim := claims[claimIndex]
		if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || *task.ActiveClaimID != claim.ID || !claim.Active() {
			return conflict("claim %q is not active for task %q", claim.ID, task.ID)
		}
		if !sameActor(claim.Executor, command.Identity.Actor) {
			return forbidden("actor does not own claim %q", claim.ID)
		}
		artifacts, err := submissionArtifacts(store, workItem, task, claim, command.ArtifactIDs)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		submissionID, err := s.newID("submission id")
		if err != nil {
			return err
		}
		submission := domain.TaskSubmission{
			ID:          domain.SubmissionID(submissionID),
			TaskID:      task.ID,
			ClaimID:     claim.ID,
			Result:      strings.TrimSpace(command.Result),
			SubmittedAt: now,
		}
		if err := submission.Validate(); err != nil {
			return err
		}

		requestReview, err := submissionRequiresReview(workItem.CoordinationMode(), task, command.RequestReview)
		if err != nil {
			return err
		}
		decision, err := s.buildTransitionDecision(store, workItem, task, submission.ID, command.Identity.Actor, command.Transition, !requestReview, now)
		if err != nil {
			return err
		}

		claim.EndedAt = &now
		claim.EndReason = domain.ClaimEndTaskCompleted
		if requestReview {
			claim.EndReason = domain.ClaimEndSubmittedForReview
		}
		claims[claimIndex] = claim
		task.ActiveClaimID = nil
		task.Submissions = append(task.Submissions, submission)
		if decision != nil {
			task.TransitionDecisions = append(task.TransitionDecisions, *decision)
		}
		task.UpdatedAt = now
		task.Version++

		var review *domain.Review
		if requestReview {
			reviewID, err := s.newID("review id")
			if err != nil {
				return err
			}
			pending := domain.Review{
				ID:           domain.ReviewID(reviewID),
				TaskID:       task.ID,
				SubmissionID: &submission.ID,
				Status:       domain.ReviewStatusPending,
				RequestedBy:  command.Identity.Actor.ID,
				RequestedAt:  now,
			}
			task.Reviews = append(task.Reviews, pending)
			task.Status = domain.TaskStatusInReview
			review = &pending
		} else {
			task.Status = domain.TaskStatusCompleted
			task.CompletedAt = &now
		}

		if err := domain.ValidateTaskContext(workItem.CoordinationMode(), task, claims); err != nil {
			return err
		}
		if err := store.SaveClaim(claim); err != nil {
			return fmt.Errorf("save submitted claim: %w", err)
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save submitted task: %w", err)
		}
		for _, artifact := range artifacts {
			artifact.SubmissionID = &submission.ID
			if err := store.SaveArtifact(artifact, now); err != nil {
				return fmt.Errorf("bind artifact %q to submission: %w", artifact.ID, err)
			}
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskSubmitted, string(submission.ID), &actor, submission.Result); err != nil {
			return err
		}
		if decision != nil {
			if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTransitionDecided, string(decision.ID), &actor, decision.Reason); err != nil {
				return err
			}
		}
		if review != nil {
			if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventReviewRequested, string(review.ID), &actor, ""); err != nil {
				return err
			}
		} else {
			if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskCompleted, string(submission.ID), &actor, ""); err != nil {
				return err
			}
			if err := s.completeBlackboardAncestors(store, workItem, task, &actor, now); err != nil {
				return err
			}
			if decision != nil {
				if err := s.applyWorkflowDecision(store, &workItem, task, *decision); err != nil {
					return err
				}
			}
			if err := s.completeWorkItemIfDone(store, &workItem, &actor); err != nil {
				return err
			}
		}

		created = submission
		return nil
	})
	if err != nil {
		return domain.TaskSubmission{}, err
	}
	return created, nil
}

func submissionArtifacts(
	store ReadStore,
	workItem domain.WorkItem,
	task domain.Task,
	claim domain.Claim,
	artifactIDs []domain.ArtifactID,
) ([]domain.Artifact, error) {
	artifacts := make([]domain.Artifact, 0, len(artifactIDs))
	seenIDs := make(map[domain.ArtifactID]struct{}, len(artifactIDs))
	seenNames := make(map[string]struct{}, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		if strings.TrimSpace(string(artifactID)) == "" {
			return nil, invalidCommand("artifact id must not be empty")
		}
		if _, exists := seenIDs[artifactID]; exists {
			return nil, invalidCommand("artifact %q is included more than once", artifactID)
		}
		seenIDs[artifactID] = struct{}{}
		artifact, err := store.GetArtifact(artifactID)
		if err != nil {
			return nil, fmt.Errorf("get artifact %q: %w", artifactID, err)
		}
		if artifact.WorkItemID != workItem.ID || artifact.TaskID != task.ID || artifact.ClaimID != claim.ID {
			return nil, invalidCommand("artifact %q does not belong to the active claim", artifact.ID)
		}
		if artifact.SubmissionID != nil {
			return nil, conflict("artifact %q has already been submitted", artifact.ID)
		}
		name := strings.TrimSpace(artifact.Name)
		if _, exists := seenNames[name]; exists {
			return nil, invalidCommand("submission contains duplicate artifact name %q", name)
		}
		seenNames[name] = struct{}{}
		artifacts = append(artifacts, artifact)
	}

	if workItem.CoordinationMode() != domain.CoordinationModeWorkflow {
		return artifacts, nil
	}
	definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
	if err != nil {
		return nil, fmt.Errorf("get workflow definition: %w", err)
	}
	workflowTask, exists := workflowTaskDefinition(definition, *task.WorkflowTaskID)
	if !exists {
		return nil, invalidCommand("workflow task definition %q does not exist", *task.WorkflowTaskID)
	}
	missing := make([]string, 0)
	for _, expected := range workflowTask.Artifacts {
		if _, exists := seenNames[strings.TrimSpace(expected.Name)]; !exists {
			missing = append(missing, expected.Name)
		}
	}
	if len(missing) > 0 {
		return nil, invalidCommand("workflow task %q requires artifacts: %s", task.ID, strings.Join(missing, ", "))
	}
	return artifacts, nil
}

func submissionRequiresReview(mode domain.CoordinationMode, task domain.Task, requested bool) (bool, error) {
	if mode == domain.CoordinationModeBlackboard {
		return requested, nil
	}
	if task.ReviewPolicy == nil {
		return false, invalidCommand("workflow task %q has no review policy", task.ID)
	}
	switch *task.ReviewPolicy {
	case domain.ReviewNone:
		if requested {
			return false, invalidCommand("task %q does not allow review", task.ID)
		}
		return false, nil
	case domain.ReviewExecutorDecides:
		return requested, nil
	case domain.ReviewRequired:
		return true, nil
	default:
		return false, invalidCommand("task %q has invalid review policy", task.ID)
	}
}

func (s *Service) buildTransitionDecision(
	store ReadStore,
	workItem domain.WorkItem,
	task domain.Task,
	submissionID domain.SubmissionID,
	actor domain.ActorRef,
	command *WorkflowTransitionCommand,
	apply bool,
	now time.Time,
) (*domain.TransitionDecision, error) {
	if workItem.CoordinationMode() == domain.CoordinationModeBlackboard {
		if command != nil {
			return nil, invalidCommand("blackboard task must not contain a workflow transition")
		}
		return nil, nil
	}
	definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
	if err != nil {
		return nil, fmt.Errorf("get workflow definition: %w", err)
	}
	groups, err := workflowChoiceGroups(definition, task)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		if command != nil {
			return nil, invalidCommand("terminal workflow task %q must not contain a transition", task.ID)
		}
		return nil, nil
	}
	if command == nil {
		return nil, invalidCommand("workflow transition is required for task %q", task.ID)
	}
	intent := domain.WorkflowSkipIntent{
		SkipTaskIDs:            append([]domain.WorkflowTaskID(nil), command.SkipOptionalTaskIDs...),
		ReviewRequestedTaskIDs: append([]domain.WorkflowTaskID(nil), command.ReviewSkippedTaskIDs...),
	}
	slices.Sort(intent.SkipTaskIDs)
	slices.Sort(intent.ReviewRequestedTaskIDs)
	transition, err := validateWorkflowSkipIntent(
		definition,
		*task.WorkflowTaskID,
		command.ChoiceGroupID,
		intent,
		strings.TrimSpace(command.Reason),
	)
	if err != nil {
		return nil, err
	}
	id, err := s.newID("transition decision id")
	if err != nil {
		return nil, err
	}
	decision := domain.TransitionDecision{
		ID:                 domain.TransitionDecisionID(id),
		WorkItemID:         workItem.ID,
		SourceTaskID:       task.ID,
		SourceSubmissionID: &submissionID,
		WorkflowTransition: transition,
		WorkflowSkipIntent: intent,
		DecidedBy:          actor,
		DecidedAt:          now,
	}
	if apply {
		decision.AppliedAt = &now
	}
	if err := definition.Graph.ValidateDecision(*task.WorkflowTaskID, decision); err != nil {
		return nil, err
	}
	return &decision, nil
}

func validateWorkflowSkipIntent(
	definition domain.WorkflowDefinition,
	sourceTaskID domain.WorkflowTaskID,
	groupID domain.WorkflowChoiceGroupID,
	intent domain.WorkflowSkipIntent,
	reason string,
) (domain.WorkflowTransition, error) {
	if err := intent.Validate(); err != nil {
		return domain.WorkflowTransition{}, err
	}
	for _, taskID := range intent.SkipTaskIDs {
		task, exists := workflowTaskDefinition(definition, taskID)
		if !exists {
			return domain.WorkflowTransition{}, invalidCommand("workflow task definition %q does not exist", taskID)
		}
		if task.Execution != domain.ExecutionOptional {
			return domain.WorkflowTransition{}, invalidCommand("skip intent task %q must be optional", taskID)
		}
	}
	for _, taskID := range intent.ReviewRequestedTaskIDs {
		task, _ := workflowTaskDefinition(definition, taskID)
		if task.ReviewPolicy != domain.ReviewExecutorDecides {
			return domain.WorkflowTransition{}, invalidCommand("skip review may be requested only for executor_decides task %q", taskID)
		}
	}
	root, projected, err := projectWorkflowSkipIntent(definition, sourceTaskID, groupID, intent, reason)
	if err != nil {
		return domain.WorkflowTransition{}, err
	}
	if !slices.Equal(projected.SkipTaskIDs, intent.SkipTaskIDs) {
		projectedTasks := make(map[domain.WorkflowTaskID]struct{}, len(projected.SkipTaskIDs))
		for _, taskID := range projected.SkipTaskIDs {
			projectedTasks[taskID] = struct{}{}
		}
		for _, taskID := range intent.SkipTaskIDs {
			if _, exists := projectedTasks[taskID]; !exists {
				return domain.WorkflowTransition{}, invalidCommand(
					"optional task %q is not reachable without crossing an executed task",
					taskID,
				)
			}
		}
	}
	return root, nil
}

func projectWorkflowSkipIntent(
	definition domain.WorkflowDefinition,
	sourceTaskID domain.WorkflowTaskID,
	groupID domain.WorkflowChoiceGroupID,
	intent domain.WorkflowSkipIntent,
	reason string,
) (domain.WorkflowTransition, domain.WorkflowSkipIntent, error) {
	if err := intent.Validate(); err != nil {
		return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, err
	}
	compiled, err := definition.CompileGraph()
	if err != nil {
		return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, err
	}
	group, err := selectedChoiceGroup(definition, sourceTaskID, groupID)
	if err != nil {
		return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, err
	}
	root, err := workflowTransitionFromSkipIntent(definition, group, intent, reason)
	if err != nil {
		return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, err
	}
	consumed := make(map[domain.WorkflowTaskID]struct{}, len(intent.SkipTaskIDs))
	queuedRelations := append([]domain.WorkflowRelationID(nil), root.SkippedRelationIDs...)
	visited := make(map[domain.WorkflowTaskID]struct{}, len(intent.SkipTaskIDs))
	for len(queuedRelations) > 0 {
		relationID := queuedRelations[0]
		queuedRelations = queuedRelations[1:]
		relation, exists := workflowRelationDefinition(definition, relationID)
		if !exists {
			return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, invalidCommand("workflow relation %q does not exist", relationID)
		}
		consumed[relation.ToTaskID] = struct{}{}
		if _, exists := visited[relation.ToTaskID]; exists {
			continue
		}
		visited[relation.ToTaskID] = struct{}{}
		exitGroup, exists := workflowExitChoiceGroup(compiled, relation.ToTaskID)
		if !exists {
			continue
		}
		transition, err := workflowTransitionFromSkipIntent(definition, exitGroup, intent, reason)
		if err != nil {
			return domain.WorkflowTransition{}, domain.WorkflowSkipIntent{}, err
		}
		queuedRelations = append(queuedRelations, transition.SkippedRelationIDs...)
	}
	projected := domain.WorkflowSkipIntent{}
	for _, taskID := range intent.SkipTaskIDs {
		if _, exists := consumed[taskID]; exists {
			projected.SkipTaskIDs = append(projected.SkipTaskIDs, taskID)
		}
	}
	for _, taskID := range intent.ReviewRequestedTaskIDs {
		if _, exists := consumed[taskID]; exists {
			projected.ReviewRequestedTaskIDs = append(projected.ReviewRequestedTaskIDs, taskID)
		}
	}
	return root, projected, nil
}

func workflowTransitionFromSkipIntent(
	definition domain.WorkflowDefinition,
	group domain.WorkflowChoiceGroup,
	intent domain.WorkflowSkipIntent,
	reason string,
) (domain.WorkflowTransition, error) {
	transition := domain.WorkflowTransition{ChoiceGroupID: group.ID, Reason: reason}
	for _, relationID := range group.RelationIDs {
		relation, exists := workflowRelationDefinition(definition, relationID)
		if !exists {
			return domain.WorkflowTransition{}, invalidCommand("workflow relation %q does not exist", relationID)
		}
		target, exists := workflowTaskDefinition(definition, relation.ToTaskID)
		if !exists {
			return domain.WorkflowTransition{}, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
		}
		shouldSkip := group.Kind == domain.WorkflowChoiceGroupExit &&
			target.Execution == domain.ExecutionOptional &&
			slices.Contains(intent.SkipTaskIDs, target.ID)
		if !shouldSkip {
			transition.TriggeredRelationIDs = append(transition.TriggeredRelationIDs, relationID)
			continue
		}
		transition.SkippedRelationIDs = append(transition.SkippedRelationIDs, relationID)
		if slices.Contains(intent.ReviewRequestedTaskIDs, target.ID) {
			transition.ReviewRequestedRelationIDs = append(transition.ReviewRequestedRelationIDs, relationID)
		}
	}
	return transition, nil
}

func workflowExitChoiceGroup(
	compiled domain.CompiledWorkflowGraph,
	taskID domain.WorkflowTaskID,
) (domain.WorkflowChoiceGroup, bool) {
	for _, group := range compiled.GroupsFor(taskID) {
		if group.Kind == domain.WorkflowChoiceGroupExit {
			return group, true
		}
	}
	return domain.WorkflowChoiceGroup{}, false
}

func workflowRelationDefinition(
	definition domain.WorkflowDefinition,
	relationID domain.WorkflowRelationID,
) (domain.WorkflowRelationDefinition, bool) {
	for _, relation := range definition.Graph.Relations {
		if relation.ID == relationID {
			return relation, true
		}
	}
	return domain.WorkflowRelationDefinition{}, false
}

func workflowChoiceGroups(definition domain.WorkflowDefinition, task domain.Task) ([]domain.WorkflowChoiceGroup, error) {
	if task.WorkflowTaskID == nil {
		return nil, invalidCommand("task %q has no workflow task id", task.ID)
	}
	compiled, err := definition.CompileGraph()
	if err != nil {
		return nil, err
	}
	return compiled.GroupsFor(*task.WorkflowTaskID), nil
}
