package application

import (
	"fmt"
	"slices"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

const defaultMaxWorkflowTaskExecutions = 1000

func (s *Service) applyWorkflowDecision(
	store WriteStore,
	workItem *domain.WorkItem,
	sourceTask domain.Task,
	decision domain.TransitionDecision,
) error {
	if decision.AppliedAt == nil {
		return conflict("transition decision %q is not applied", decision.ID)
	}
	definition, err := store.GetWorkflowDefinition(workItem.Definition.ID, workItem.Definition.Version)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}
	if sourceTask.WorkflowTaskID == nil || sourceTask.WorkflowActivationID == nil {
		return invalidCommand("workflow task %q has incomplete runtime identity", sourceTask.ID)
	}
	if err := definition.Graph.ValidateDecision(*sourceTask.WorkflowTaskID, decision); err != nil {
		return err
	}
	group, err := selectedChoiceGroup(definition, *sourceTask.WorkflowTaskID, decision.ChoiceGroupID)
	if err != nil {
		return err
	}
	sourceActivation, err := store.GetWorkflowTaskActivation(*sourceTask.WorkflowActivationID)
	if err != nil {
		return fmt.Errorf("get source workflow activation: %w", err)
	}
	relations := make(map[domain.WorkflowRelationID]domain.WorkflowRelationDefinition, len(definition.Graph.Relations))
	for _, relation := range definition.Graph.Relations {
		relations[relation.ID] = relation
	}
	for _, relationID := range group.RelationIDs {
		relation := relations[relationID]
		outcome := domain.WorkflowActivationInputTriggered
		if slices.Contains(decision.SkippedRelationIDs, relationID) {
			outcome = domain.WorkflowActivationInputSkipped
		}
		correlationID, parentCorrelationIDs, err := s.nextWorkflowCorrelation(
			definition,
			group.Kind,
			sourceActivation,
			relation,
		)
		if err != nil {
			return err
		}
		stopped, err := s.resolveWorkflowActivationInput(
			store,
			workItem,
			definition,
			group.Kind,
			correlationID,
			parentCorrelationIDs,
			relation,
			sourceTask,
			decision,
			outcome,
		)
		if err != nil {
			return err
		}
		if stopped {
			break
		}
	}
	actor := decision.DecidedBy
	return s.appendEvent(
		store,
		workItem.ID,
		&sourceTask.ID,
		domain.WorkItemEventTransitionApplied,
		string(decision.ID),
		&actor,
		decision.Reason,
	)
}

func selectedChoiceGroup(
	definition domain.WorkflowDefinition,
	sourceTaskID domain.WorkflowTaskID,
	groupID domain.WorkflowChoiceGroupID,
) (domain.WorkflowChoiceGroup, error) {
	compiled, err := definition.CompileGraph()
	if err != nil {
		return domain.WorkflowChoiceGroup{}, err
	}
	for _, group := range compiled.GroupsFor(sourceTaskID) {
		if group.ID == groupID {
			return group, nil
		}
	}
	return domain.WorkflowChoiceGroup{}, invalidCommand("choice group %q does not belong to workflow task %q", groupID, sourceTaskID)
}

func (s *Service) nextWorkflowCorrelation(
	definition domain.WorkflowDefinition,
	groupKind domain.WorkflowChoiceGroupKind,
	source domain.WorkflowTaskActivation,
	relation domain.WorkflowRelationDefinition,
) (domain.WorkflowCorrelationID, []domain.WorkflowCorrelationID, error) {
	correlationID := source.CorrelationID
	parents := append([]domain.WorkflowCorrelationID(nil), source.ParentCorrelationIDs...)
	if groupKind == domain.WorkflowChoiceGroupContinue {
		if len(parents) == 0 {
			parents = append(parents, correlationID)
		}
		id, err := s.newID("workflow correlation id")
		if err != nil {
			return "", nil, err
		}
		return domain.WorkflowCorrelationID(id), parents, nil
	}

	sourceInCycle := workflowTaskInCycle(definition, relation.FromTaskID)
	targetInCycle := workflowTaskInCycle(definition, relation.ToTaskID)
	if sourceInCycle && len(parents) > 0 {
		last := len(parents) - 1
		correlationID = parents[last]
		parents = parents[:last]
	}
	if targetInCycle {
		parents = append(parents, correlationID)
		id, err := s.newID("workflow correlation id")
		if err != nil {
			return "", nil, err
		}
		correlationID = domain.WorkflowCorrelationID(id)
	}
	return correlationID, parents, nil
}

func workflowTaskInCycle(definition domain.WorkflowDefinition, taskID domain.WorkflowTaskID) bool {
	compiled, err := definition.CompileGraph()
	if err != nil {
		return false
	}
	for _, group := range compiled.GroupsFor(taskID) {
		if group.Kind == domain.WorkflowChoiceGroupContinue {
			return true
		}
	}
	return false
}

func (s *Service) resolveWorkflowActivationInput(
	store WriteStore,
	workItem *domain.WorkItem,
	definition domain.WorkflowDefinition,
	groupKind domain.WorkflowChoiceGroupKind,
	correlationID domain.WorkflowCorrelationID,
	parentCorrelationIDs []domain.WorkflowCorrelationID,
	relation domain.WorkflowRelationDefinition,
	sourceTask domain.Task,
	decision domain.TransitionDecision,
	outcome domain.WorkflowActivationInputOutcome,
) (bool, error) {
	activations, err := store.ListWorkflowTaskActivations(workItem.ID)
	if err != nil {
		return false, fmt.Errorf("list workflow activations: %w", err)
	}
	activation, exists, err := findWaitingActivation(activations, relation.ToTaskID, correlationID)
	if err != nil {
		return false, err
	}
	created := false
	if !exists {
		id, err := s.newID("workflow activation id")
		if err != nil {
			return false, err
		}
		now := s.clock.Now()
		activation = domain.WorkflowTaskActivation{
			ID:                   domain.WorkflowTaskActivationID(id),
			WorkItemID:           workItem.ID,
			WorkflowTaskID:       relation.ToTaskID,
			CorrelationID:        correlationID,
			ParentCorrelationIDs: append([]domain.WorkflowCorrelationID(nil), parentCorrelationIDs...),
			Inputs:               expectedActivationInputs(definition, groupKind, relation),
			Status:               domain.WorkflowActivationWaiting,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		created = true
	}

	inputIndex := -1
	for index := range activation.Inputs {
		if activation.Inputs[index].RelationID == relation.ID {
			inputIndex = index
			break
		}
	}
	if inputIndex < 0 {
		return false, invalidCommand("relation %q is not expected by activation %q", relation.ID, activation.ID)
	}
	if activation.Inputs[inputIndex].Resolved() {
		return false, conflict("relation %q already resolved activation %q", relation.ID, activation.ID)
	}
	now := s.clock.Now()
	activation.Inputs[inputIndex].SourceTaskID = &sourceTask.ID
	activation.Inputs[inputIndex].DecisionID = &decision.ID
	activation.Inputs[inputIndex].Outcome = outcome
	activation.Inputs[inputIndex].ReviewRequested = slices.Contains(decision.ReviewRequestedRelationIDs, relation.ID)
	activation.Inputs[inputIndex].ResolvedAt = &now
	activation.UpdatedAt = now

	if !allActivationInputsResolved(activation.Inputs) {
		if err := activation.Validate(); err != nil {
			return false, err
		}
		if created {
			if err := store.CreateWorkflowTaskActivation(activation); err != nil {
				return false, fmt.Errorf("create waiting workflow activation: %w", err)
			}
		} else if err := store.SaveWorkflowTaskActivation(activation); err != nil {
			return false, fmt.Errorf("save waiting workflow activation: %w", err)
		}
		return false, nil
	}

	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return false, fmt.Errorf("list workflow tasks: %w", err)
	}
	limit := definition.Graph.MaxTaskExecutions
	if limit == 0 {
		limit = defaultMaxWorkflowTaskExecutions
	}
	if len(tasks) >= limit {
		if err := s.failWorkflowExecutionLimit(store, workItem, sourceTask, limit, now); err != nil {
			return false, err
		}
		return true, nil
	}

	taskDefinition, ok := workflowTaskDefinition(definition, relation.ToTaskID)
	if !ok {
		return false, invalidCommand("workflow task definition %q does not exist", relation.ToTaskID)
	}
	task, err := s.newWorkflowTask(workItem.ID, taskDefinition, activation.ID, nextTaskPosition(tasks), now)
	if err != nil {
		return false, err
	}
	activation.Status = domain.WorkflowActivationResolved
	activation.Outcome = domain.WorkflowActivationSkipped
	task.Status = domain.TaskStatusSkipped
	task.CompletedAt = &now
	triggered := false
	for _, input := range activation.Inputs {
		if input.Outcome == domain.WorkflowActivationInputTriggered {
			triggered = true
			activation.Outcome = domain.WorkflowActivationCreated
			task.Status = domain.TaskStatusPending
			task.CompletedAt = nil
			break
		}
	}
	var skipReview *domain.Review
	var skipDecision *domain.TransitionDecision
	if !triggered {
		planned, err := skipDecisionForActivation(store, definition, activation)
		if err != nil {
			return false, err
		}
		if planned != nil {
			decisionID, err := s.newID("transition decision id")
			if err != nil {
				return false, err
			}
			materialized := domain.TransitionDecision{
				ID:                 domain.TransitionDecisionID(decisionID),
				WorkItemID:         workItem.ID,
				SourceTaskID:       task.ID,
				WorkflowTransition: planned.transition,
				WorkflowSkipIntent: planned.intent,
				DecidedBy:          decision.DecidedBy,
				DecidedAt:          now,
			}
			if err := definition.Graph.ValidateDecision(activation.WorkflowTaskID, materialized); err != nil {
				return false, err
			}
			task.TransitionDecisions = append(task.TransitionDecisions, materialized)
			skipDecision = &task.TransitionDecisions[len(task.TransitionDecisions)-1]
		}
	}
	if !triggered && skipRequiresReview(taskDefinition.ReviewPolicy, activation.Inputs) {
		reviewID, err := s.newID("review id")
		if err != nil {
			return false, err
		}
		review := domain.Review{
			ID:          domain.ReviewID(reviewID),
			TaskID:      task.ID,
			Status:      domain.ReviewStatusPending,
			RequestedBy: decision.DecidedBy.ID,
			RequestedAt: now,
		}
		task.Status = domain.TaskStatusInReview
		task.CompletedAt = nil
		task.Reviews = append(task.Reviews, review)
		activation.Outcome = domain.WorkflowActivationReview
		skipReview = &review
	} else if skipDecision != nil {
		skipDecision.AppliedAt = &now
	}
	if task.Status == domain.TaskStatusSkipped {
		actor := decision.DecidedBy
		task.SkippedBy = &actor
		if skipDecision != nil {
			task.SkipReason = skipDecision.Reason
		}
	}
	activation.ResolvedAt = &now
	if err := activation.Validate(); err != nil {
		return false, err
	}
	if err := task.Validate(domain.CoordinationModeWorkflow); err != nil {
		return false, err
	}
	if err := store.CreateTask(task); err != nil {
		return false, fmt.Errorf("create activated workflow task: %w", err)
	}
	if created {
		if err := store.CreateWorkflowTaskActivation(activation); err != nil {
			return false, fmt.Errorf("create resolved workflow activation: %w", err)
		}
	} else if err := store.SaveWorkflowTaskActivation(activation); err != nil {
		return false, fmt.Errorf("save resolved workflow activation: %w", err)
	}
	for _, input := range activation.Inputs {
		if input.SourceTaskID == nil {
			continue
		}
		concreteRelation := domain.TaskRelation{
			WorkItemID: workItem.ID,
			FromTaskID: *input.SourceTaskID,
			ToTaskID:   task.ID,
			CreatedAt:  now,
		}
		if err := store.CreateTaskRelation(concreteRelation); err != nil {
			return false, fmt.Errorf("create activated task relation: %w", err)
		}
	}
	actor := decision.DecidedBy
	eventType := domain.WorkItemEventTaskCreated
	if task.Status == domain.TaskStatusSkipped {
		eventType = domain.WorkItemEventTaskSkipped
	}
	if err := s.appendEvent(store, workItem.ID, &task.ID, eventType, string(task.ID), &actor, ""); err != nil {
		return false, err
	}
	if skipDecision != nil {
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTransitionDecided, string(skipDecision.ID), &actor, skipDecision.Reason); err != nil {
			return false, err
		}
	}
	if skipReview != nil {
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventReviewRequested, string(skipReview.ID), &actor, "optional task skip"); err != nil {
			return false, err
		}
	}
	if skipDecision != nil && skipDecision.AppliedAt != nil {
		if err := s.applyWorkflowDecision(store, workItem, task, *skipDecision); err != nil {
			return false, err
		}
	}
	return false, nil
}

type materializedSkipDecision struct {
	transition domain.WorkflowTransition
	intent     domain.WorkflowSkipIntent
}

func skipDecisionForActivation(
	store ReadStore,
	definition domain.WorkflowDefinition,
	activation domain.WorkflowTaskActivation,
) (*materializedSkipDecision, error) {
	compiled, err := definition.CompileGraph()
	if err != nil {
		return nil, err
	}
	exitGroup, hasExit := workflowExitChoiceGroup(compiled, activation.WorkflowTaskID)
	if !hasExit {
		return nil, nil
	}
	var commonSkips map[domain.WorkflowTaskID]struct{}
	reviewRequests := make(map[domain.WorkflowTaskID]struct{})
	for _, input := range activation.Inputs {
		if input.DecisionID == nil || input.SourceTaskID == nil {
			return nil, invalidCommand("activation %q contains an unresolved input", activation.ID)
		}
		sourceTask, err := store.GetTask(*input.SourceTaskID)
		if err != nil {
			return nil, fmt.Errorf("get skip intent source task: %w", err)
		}
		sourceDecision, exists := transitionDecisionByID(sourceTask, *input.DecisionID)
		if !exists {
			return nil, invalidCommand("decision %q does not exist on task %q", *input.DecisionID, sourceTask.ID)
		}
		if !slices.Contains(sourceDecision.SkipTaskIDs, activation.WorkflowTaskID) {
			return nil, invalidCommand("decision %q does not permit skipping workflow task %q", sourceDecision.ID, activation.WorkflowTaskID)
		}
		candidateSkips := make(map[domain.WorkflowTaskID]struct{}, len(sourceDecision.SkipTaskIDs))
		for _, taskID := range sourceDecision.SkipTaskIDs {
			candidateSkips[taskID] = struct{}{}
		}
		if commonSkips == nil {
			commonSkips = candidateSkips
		} else {
			for taskID := range commonSkips {
				if _, exists := candidateSkips[taskID]; !exists {
					delete(commonSkips, taskID)
				}
			}
		}
		for _, taskID := range sourceDecision.ReviewRequestedTaskIDs {
			reviewRequests[taskID] = struct{}{}
		}
	}
	delete(commonSkips, activation.WorkflowTaskID)
	delete(reviewRequests, activation.WorkflowTaskID)
	merged := domain.WorkflowSkipIntent{}
	for taskID := range commonSkips {
		merged.SkipTaskIDs = append(merged.SkipTaskIDs, taskID)
	}
	slices.Sort(merged.SkipTaskIDs)
	for taskID := range reviewRequests {
		if _, remainsSkipped := commonSkips[taskID]; remainsSkipped {
			merged.ReviewRequestedTaskIDs = append(merged.ReviewRequestedTaskIDs, taskID)
		}
	}
	slices.Sort(merged.ReviewRequestedTaskIDs)
	transition, projected, err := projectWorkflowSkipIntent(
		definition,
		activation.WorkflowTaskID,
		exitGroup.ID,
		merged,
		"Skipped by upstream intent",
	)
	if err != nil {
		return nil, err
	}
	return &materializedSkipDecision{transition: transition, intent: projected}, nil
}

func transitionDecisionByID(task domain.Task, id domain.TransitionDecisionID) (domain.TransitionDecision, bool) {
	for _, decision := range task.TransitionDecisions {
		if decision.ID == id {
			return decision, true
		}
	}
	return domain.TransitionDecision{}, false
}

func skipRequiresReview(policy domain.ReviewPolicy, inputs []domain.WorkflowActivationInput) bool {
	if policy == domain.ReviewRequired {
		return true
	}
	if policy != domain.ReviewExecutorDecides {
		return false
	}
	for _, input := range inputs {
		if input.ReviewRequested {
			return true
		}
	}
	return false
}

func findWaitingActivation(
	activations []domain.WorkflowTaskActivation,
	taskID domain.WorkflowTaskID,
	correlationID domain.WorkflowCorrelationID,
) (domain.WorkflowTaskActivation, bool, error) {
	var found *domain.WorkflowTaskActivation
	for _, activation := range activations {
		if activation.WorkflowTaskID != taskID || activation.CorrelationID != correlationID || activation.Status != domain.WorkflowActivationWaiting {
			continue
		}
		if found != nil {
			return domain.WorkflowTaskActivation{}, false, conflict(
				"multiple waiting activations exist for workflow task %q and correlation %q",
				taskID,
				correlationID,
			)
		}
		candidate := activation
		found = &candidate
	}
	if found == nil {
		return domain.WorkflowTaskActivation{}, false, nil
	}
	return *found, true, nil
}

func expectedActivationInputs(
	definition domain.WorkflowDefinition,
	groupKind domain.WorkflowChoiceGroupKind,
	selected domain.WorkflowRelationDefinition,
) []domain.WorkflowActivationInput {
	if groupKind == domain.WorkflowChoiceGroupContinue {
		return []domain.WorkflowActivationInput{{RelationID: selected.ID}}
	}
	var inputs []domain.WorkflowActivationInput
	for _, incoming := range definition.Graph.Relations {
		if incoming.ToTaskID != selected.ToTaskID || relationIsContinue(definition, incoming) {
			continue
		}
		inputs = append(inputs, domain.WorkflowActivationInput{RelationID: incoming.ID})
	}
	return inputs
}

func relationIsContinue(definition domain.WorkflowDefinition, relation domain.WorkflowRelationDefinition) bool {
	compiled, err := definition.CompileGraph()
	if err != nil {
		return false
	}
	for _, group := range compiled.GroupsFor(relation.FromTaskID) {
		if group.Kind == domain.WorkflowChoiceGroupContinue && slices.Contains(group.RelationIDs, relation.ID) {
			return true
		}
	}
	return false
}

func allActivationInputsResolved(inputs []domain.WorkflowActivationInput) bool {
	for _, input := range inputs {
		if !input.Resolved() {
			return false
		}
	}
	return true
}

func workflowTaskDefinition(
	definition domain.WorkflowDefinition,
	taskID domain.WorkflowTaskID,
) (domain.WorkflowTaskDefinition, bool) {
	for _, task := range definition.Graph.Tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.WorkflowTaskDefinition{}, false
}

func (s *Service) failWorkflowExecutionLimit(
	store WriteStore,
	workItem *domain.WorkItem,
	sourceTask domain.Task,
	limit int,
	now time.Time,
) error {
	workItem.Status = domain.WorkItemStatusFailed
	workItem.UpdatedAt = now
	workItem.Version++
	if err := workItem.Validate(); err != nil {
		return err
	}
	if err := store.SaveWorkItem(*workItem); err != nil {
		return fmt.Errorf("save workflow execution limit failure: %w", err)
	}
	actor := domain.ActorRef{Kind: domain.ActorAgent, ID: "kairos"}
	message := fmt.Sprintf("workflow exceeded MaxTaskExecutions (%d) after task %s", limit, sourceTask.ID)
	return s.appendEvent(store, workItem.ID, nil, domain.WorkItemEventWorkItemFailed, string(workItem.ID), &actor, message)
}

func hasAppliedDecision(task domain.Task) bool {
	for _, decision := range task.TransitionDecisions {
		if decision.AppliedAt != nil {
			return true
		}
	}
	return false
}
