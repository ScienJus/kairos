package mcpapi

import (
	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"time"
)

type definitionView struct {
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	AgentInstructions string   `json:"agent_instructions,omitempty"`
	SuggestedTags     []string `json:"suggested_tags"`
}

type definitionBindingView struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Mode    string `json:"mode"`
}

type workItemView struct {
	ID                 string                `json:"id"`
	Definition         definitionBindingView `json:"definition"`
	Status             string                `json:"status"`
	AcceptanceMode     string                `json:"acceptance_mode"`
	Title              string                `json:"title"`
	Goal               string                `json:"goal"`
	Context            string                `json:"context,omitempty"`
	Constraints        string                `json:"constraints,omitempty"`
	AcceptanceCriteria string                `json:"acceptance_criteria,omitempty"`
	Tags               []string              `json:"tags"`
	Result             string                `json:"result,omitempty"`
}

type actorView struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type claimView struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	Executor     actorView `json:"executor"`
	EndReason    string    `json:"end_reason,omitempty"`
	LeaseSeconds int64     `json:"lease_seconds,omitempty"`
	LeaseUntil   string    `json:"lease_until,omitempty"`
}

type coordinationClaimView struct {
	ID           string    `json:"id"`
	WorkItemID   string    `json:"work_item_id"`
	Kind         string    `json:"kind"`
	Executor     actorView `json:"executor"`
	EndReason    string    `json:"end_reason,omitempty"`
	LeaseSeconds int64     `json:"lease_seconds"`
	LeaseUntil   string    `json:"lease_until"`
}

type submissionView struct {
	ID      string `json:"id"`
	TaskID  string `json:"task_id"`
	ClaimID string `json:"claim_id"`
	Result  string `json:"result"`
}

type artifactDefinitionView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type artifactView struct {
	ID           string  `json:"id"`
	WorkItemID   string  `json:"work_item_id"`
	TaskID       string  `json:"task_id"`
	ClaimID      string  `json:"claim_id"`
	SubmissionID *string `json:"submission_id,omitempty"`
	Name         string  `json:"name"`
	URI          string  `json:"uri"`
}

type failureView struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	ClaimID     string `json:"claim_id"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	RetryPrompt string `json:"retry_prompt,omitempty"`
}

type reviewView struct {
	ID           string  `json:"id"`
	TaskID       string  `json:"task_id"`
	SubmissionID *string `json:"submission_id,omitempty"`
	Status       string  `json:"status"`
	RequestedBy  string  `json:"requested_by"`
	DecidedBy    *string `json:"decided_by,omitempty"`
	Feedback     string  `json:"feedback,omitempty"`
}

type transitionDecisionView struct {
	ID                     string    `json:"id"`
	ChoiceGroupID          string    `json:"choice_group_id"`
	SkipTaskIDs            []string  `json:"skip_task_ids"`
	ReviewRequestedTaskIDs []string  `json:"review_requested_task_ids"`
	Reason                 string    `json:"reason,omitempty"`
	DecidedBy              actorView `json:"decided_by"`
}

type taskSummaryView struct {
	ID                 string   `json:"id"`
	WorkItemID         string   `json:"work_item_id"`
	ParentTaskID       *string  `json:"parent_task_id,omitempty"`
	WorkflowTaskID     *string  `json:"workflow_task_id,omitempty"`
	Status             string   `json:"status"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
	Executor           string   `json:"executor"`
	AllowedRoles       []string `json:"allowed_roles"`
	Tags               []string `json:"tags"`
	LatestResult       string   `json:"latest_result,omitempty"`
	RetryPrompt        string   `json:"retry_prompt,omitempty"`
}

type taskView struct {
	ID                  string                   `json:"id"`
	WorkItemID          string                   `json:"work_item_id"`
	ParentTaskID        *string                  `json:"parent_task_id,omitempty"`
	WorkflowTaskID      *string                  `json:"workflow_task_id,omitempty"`
	Status              string                   `json:"status"`
	ActiveClaimID       *string                  `json:"active_claim_id,omitempty"`
	Title               string                   `json:"title"`
	Description         string                   `json:"description,omitempty"`
	AcceptanceCriteria  string                   `json:"acceptance_criteria,omitempty"`
	Executor            string                   `json:"executor"`
	AllowedRoles        []string                 `json:"allowed_roles"`
	Tags                []string                 `json:"tags"`
	Execution           *string                  `json:"execution,omitempty"`
	ReviewPolicy        *string                  `json:"review_policy,omitempty"`
	Reviews             []reviewView             `json:"reviews"`
	Submissions         []submissionView         `json:"submissions"`
	Failures            []failureView            `json:"failures"`
	TransitionDecisions []transitionDecisionView `json:"transition_decisions"`
}

type relationView struct {
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
}

type workflowTaskDefinitionView struct {
	ID                 string                   `json:"id"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description,omitempty"`
	AcceptanceCriteria string                   `json:"acceptance_criteria,omitempty"`
	Executor           string                   `json:"executor"`
	AllowedRoles       []string                 `json:"allowed_roles"`
	Execution          string                   `json:"execution"`
	ReviewPolicy       string                   `json:"review_policy"`
	Tags               []string                 `json:"tags"`
	RelationGuidance   string                   `json:"relation_guidance,omitempty"`
	Artifacts          []artifactDefinitionView `json:"artifacts"`
}

type workflowChoiceGroupView struct {
	ID                     string                       `json:"id"`
	Kind                   string                       `json:"kind"`
	Targets                []workflowTaskDefinitionView `json:"targets"`
	SkippableOptionalTasks []workflowTaskDefinitionView `json:"skippable_optional_tasks"`
}

type workflowContextView struct {
	UpstreamTasks []taskSummaryView         `json:"upstream_tasks"`
	ChoiceGroups  []workflowChoiceGroupView `json:"choice_groups"`
}

type blackboardContextView struct {
	CurrentTaskID string            `json:"current_task_id"`
	Tasks         []taskSummaryView `json:"tasks"`
	Relations     []relationView    `json:"relations"`
	CanDecompose  bool              `json:"can_decompose"`
}

type workCandidateView struct {
	Kind       string           `json:"kind"`
	WorkItem   workItemView     `json:"work_item"`
	Definition definitionView   `json:"definition"`
	Task       *taskSummaryView `json:"task,omitempty"`
}

type findWorkOutput struct {
	Candidates []workCandidateView `json:"candidates"`
}

type taskContextOutput struct {
	WorkItem          workItemView             `json:"work_item"`
	Task              taskView                 `json:"task"`
	Claims            []claimView              `json:"claims"`
	Definition        definitionView           `json:"definition"`
	Workflow          *workflowContextView     `json:"workflow,omitempty"`
	Blackboard        *blackboardContextView   `json:"blackboard,omitempty"`
	Responsibility    responsibilityView       `json:"responsibility"`
	Outcome           outcomeView              `json:"outcome"`
	ExpectedArtifacts []artifactDefinitionView `json:"expected_artifacts"`
	Artifacts         []artifactView           `json:"artifacts"`
}

type responsibilityView struct {
	Kind  string     `json:"kind"`
	Actor *actorView `json:"actor,omitempty"`
}
type outcomeView struct {
	Kind       string     `json:"kind"`
	Actor      *actorView `json:"actor,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	OccurredAt string     `json:"occurred_at,omitempty"`
}

type workItemContextOutput struct {
	WorkItem                workItemView            `json:"work_item"`
	Definition              definitionView          `json:"definition"`
	Tasks                   []taskSummaryView       `json:"tasks"`
	Relations               []relationView          `json:"relations"`
	CoordinationClaims      []coordinationClaimView `json:"coordination_claims"`
	ActiveCoordinationClaim *coordinationClaimView  `json:"active_coordination_claim"`
	Artifacts               []artifactView          `json:"artifacts"`
}

type claimOutput struct {
	Claim claimView `json:"claim"`
}

type coordinationClaimOutput struct {
	Claim coordinationClaimView `json:"claim"`
}

type submissionOutput struct {
	Submission submissionView `json:"submission"`
}

type artifactOutput struct {
	Artifact artifactView `json:"artifact"`
}

type failureOutput struct {
	Failure failureView `json:"failure"`
}

type taskOutput struct {
	Task taskSummaryView `json:"task"`
}

type relationOutput struct {
	Relation relationView `json:"relation"`
}
type decompositionOutput struct {
	Parent   taskSummaryView   `json:"parent"`
	Children []taskSummaryView `json:"children"`
}
type workItemOutput struct {
	WorkItem workItemView `json:"work_item"`
}

func relationViewFrom(value domain.TaskRelation) relationView {
	return relationView{FromTaskID: string(value.FromTaskID), ToTaskID: string(value.ToTaskID)}
}

type releasedOutput struct {
	Released bool `json:"released"`
}

func findWorkView(candidates []application.WorkCandidate) findWorkOutput {
	result := findWorkOutput{Candidates: make([]workCandidateView, 0, len(candidates))}
	for _, candidate := range candidates {
		view := workCandidateView{
			Kind:       string(candidate.Kind),
			WorkItem:   workItemViewFrom(candidate.WorkItem),
			Definition: definitionViewFrom(candidate.Definition),
		}
		if candidate.Kind == application.WorkCandidateTask && candidate.Task != nil {
			task := taskSummaryViewFrom(*candidate.Task)
			view.Task = &task
		}
		result.Candidates = append(result.Candidates, view)
	}
	return result
}

func taskContextView(value application.TaskExecutionContext) taskContextOutput {
	result := taskContextOutput{
		WorkItem:          workItemViewFrom(value.WorkItem),
		Task:              taskViewFrom(value.Task),
		Claims:            claimViews(value.Claims),
		Definition:        definitionViewFrom(value.Definition),
		ExpectedArtifacts: artifactDefinitionViews(value.ExpectedArtifacts),
		Artifacts:         artifactViews(value.Artifacts),
	}
	result.Responsibility = responsibilityView{Kind: value.Responsibility.Kind, Actor: actorViewPtr(value.Responsibility.Actor)}
	result.Outcome = outcomeView{Kind: value.Outcome.Kind, Actor: actorViewPtr(value.Outcome.Actor), Reason: value.Outcome.Reason}
	if value.Outcome.OccurredAt != nil {
		result.Outcome.OccurredAt = value.Outcome.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	if value.Workflow != nil {
		workflow := workflowContextView{
			UpstreamTasks: taskSummaryViews(value.Workflow.UpstreamTasks),
			ChoiceGroups:  make([]workflowChoiceGroupView, 0, len(value.Workflow.ChoiceGroups)),
		}
		for _, group := range value.Workflow.ChoiceGroups {
			targets := workflowChoiceTargetViews(group.Relations)
			if len(targets) == 0 && len(group.Targets) > 0 {
				targets = workflowTaskDefinitionViews(group.Targets)
			}
			workflow.ChoiceGroups = append(workflow.ChoiceGroups, workflowChoiceGroupView{
				ID: string(group.ID), Kind: string(group.Kind),
				Targets:                targets,
				SkippableOptionalTasks: workflowTaskDefinitionViews(group.SkippableOptionalTasks),
			})
		}
		result.Workflow = &workflow
	}
	if value.Blackboard != nil {
		blackboard := blackboardContextView{
			CurrentTaskID: string(value.Blackboard.CurrentTaskID),
			Tasks:         taskSummaryViews(value.Blackboard.Tasks),
			Relations:     relationViews(value.Blackboard.Relations),
			CanDecompose:  value.Blackboard.CanDecompose,
		}
		result.Blackboard = &blackboard
	}
	return result
}

func workItemContextView(value application.WorkItemExecutionContext) workItemContextOutput {
	result := workItemContextOutput{
		WorkItem:           workItemViewFrom(value.WorkItem),
		Definition:         definitionViewFrom(value.Definition),
		Tasks:              taskSummaryViews(value.Tasks),
		Relations:          relationViews(value.Relations),
		CoordinationClaims: coordinationClaimViews(value.CoordinationClaims),
		Artifacts:          artifactViews(value.Artifacts),
	}
	if value.ActiveCoordinationClaim != nil {
		claim := coordinationClaimViewFrom(*value.ActiveCoordinationClaim)
		result.ActiveCoordinationClaim = &claim
	}
	return result
}

func definitionViewFrom(value application.DefinitionExecutionContext) definitionView {
	return definitionView{
		Name: value.Name, Description: value.Description,
		AgentInstructions: value.AgentInstructions, SuggestedTags: stringSlice(value.SuggestedTags),
	}
}

func workItemViewFrom(value domain.WorkItem) workItemView {
	return workItemView{
		ID:         string(value.ID),
		Definition: definitionBindingView{ID: string(value.Definition.ID), Version: value.Definition.Version, Mode: string(value.Definition.Mode)},
		Status:     string(value.Status), AcceptanceMode: string(value.AcceptanceMode), Title: value.Title, Goal: value.Goal, Context: value.Context,
		Constraints: value.Constraints, AcceptanceCriteria: value.AcceptanceCriteria,
		Tags: stringSlice(value.Tags), Result: value.Result,
	}
}

func taskSummaryViews(values []domain.Task) []taskSummaryView {
	result := make([]taskSummaryView, 0, len(values))
	for _, value := range values {
		result = append(result, taskSummaryViewFrom(value))
	}
	return result
}

func taskSummaryViewFrom(value domain.Task) taskSummaryView {
	return taskSummaryView{
		ID: string(value.ID), WorkItemID: string(value.WorkItemID),
		ParentTaskID: optionalString(value.ParentTaskID), WorkflowTaskID: optionalString(value.WorkflowTaskID),
		Status: string(value.Status), Title: value.Title, Description: value.Description,
		AcceptanceCriteria: value.AcceptanceCriteria, Executor: string(value.Executor),
		AllowedRoles: stringSlice(value.AllowedRoles), Tags: stringSlice(value.Tags),
		LatestResult: latestResult(value), RetryPrompt: latestRetryPrompt(value),
	}
}

func taskViewFrom(value domain.Task) taskView {
	return taskView{
		ID: string(value.ID), WorkItemID: string(value.WorkItemID),
		ParentTaskID: optionalString(value.ParentTaskID), WorkflowTaskID: optionalString(value.WorkflowTaskID),
		Status: string(value.Status), ActiveClaimID: optionalString(value.ActiveClaimID),
		Title: value.Title, Description: value.Description, AcceptanceCriteria: value.AcceptanceCriteria,
		Executor: string(value.Executor), AllowedRoles: stringSlice(value.AllowedRoles), Tags: stringSlice(value.Tags),
		Execution: optionalString(value.Execution), ReviewPolicy: optionalString(value.ReviewPolicy),
		Reviews: reviewViews(value.Reviews), Submissions: submissionViews(value.Submissions),
		Failures: failureViews(value.Failures), TransitionDecisions: transitionDecisionViews(value.TransitionDecisions),
	}
}

func claimViews(values []domain.Claim) []claimView {
	result := make([]claimView, 0, len(values))
	for _, value := range values {
		result = append(result, claimViewFrom(value))
	}
	return result
}

func claimViewFrom(value domain.Claim) claimView {
	result := claimView{
		ID: string(value.ID), TaskID: string(value.TaskID), Executor: actorViewFrom(value.Executor),
		EndReason: string(value.EndReason), LeaseSeconds: value.LeaseSeconds,
	}
	if !value.LeaseUntil.IsZero() {
		result.LeaseUntil = value.LeaseUntil.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func coordinationClaimViews(values []domain.CoordinationClaim) []coordinationClaimView {
	result := make([]coordinationClaimView, 0, len(values))
	for _, value := range values {
		result = append(result, coordinationClaimViewFrom(value))
	}
	return result
}

func coordinationClaimViewFrom(value domain.CoordinationClaim) coordinationClaimView {
	return coordinationClaimView{
		ID: string(value.ID), WorkItemID: string(value.WorkItemID), Kind: string(value.Kind), Executor: actorViewFrom(value.Executor),
		EndReason: string(value.EndReason), LeaseSeconds: value.LeaseSeconds, LeaseUntil: value.LeaseUntil.UTC().Format(time.RFC3339Nano),
	}
}

func submissionViews(values []domain.TaskSubmission) []submissionView {
	result := make([]submissionView, 0, len(values))
	for _, value := range values {
		result = append(result, submissionViewFrom(value))
	}
	return result
}

func submissionViewFrom(value domain.TaskSubmission) submissionView {
	return submissionView{
		ID: string(value.ID), TaskID: string(value.TaskID), ClaimID: string(value.ClaimID),
		Result: value.Result,
	}
}

func failureViews(values []domain.TaskFailure) []failureView {
	result := make([]failureView, 0, len(values))
	for _, value := range values {
		result = append(result, failureViewFrom(value))
	}
	return result
}

func failureViewFrom(value domain.TaskFailure) failureView {
	return failureView{
		ID: string(value.ID), TaskID: string(value.TaskID), ClaimID: string(value.ClaimID),
		Action: string(value.Action), Reason: value.Reason, RetryPrompt: value.RetryPrompt,
	}
}

func reviewViews(values []domain.Review) []reviewView {
	result := make([]reviewView, 0, len(values))
	for _, value := range values {
		result = append(result, reviewView{
			ID: string(value.ID), TaskID: string(value.TaskID), SubmissionID: optionalString(value.SubmissionID),
			Status: string(value.Status), RequestedBy: string(value.RequestedBy),
			DecidedBy: optionalString(value.DecidedBy), Feedback: value.Feedback,
		})
	}
	return result
}

func transitionDecisionViews(values []domain.TransitionDecision) []transitionDecisionView {
	result := make([]transitionDecisionView, 0, len(values))
	for _, value := range values {
		result = append(result, transitionDecisionView{
			ID: string(value.ID), ChoiceGroupID: string(value.ChoiceGroupID),
			SkipTaskIDs:            stringValues(value.SkipTaskIDs),
			ReviewRequestedTaskIDs: stringValues(value.ReviewRequestedTaskIDs),
			Reason:                 value.Reason, DecidedBy: actorViewFrom(value.DecidedBy),
		})
	}
	return result
}

func relationViews(values []domain.TaskRelation) []relationView {
	result := make([]relationView, 0, len(values))
	for _, value := range values {
		result = append(result, relationView{
			FromTaskID: string(value.FromTaskID), ToTaskID: string(value.ToTaskID),
		})
	}
	return result
}

func workflowTaskDefinitionViews(values []domain.WorkflowTaskDefinition) []workflowTaskDefinitionView {
	result := make([]workflowTaskDefinitionView, 0, len(values))
	for _, value := range values {
		result = append(result, workflowTaskDefinitionViewFrom(value))
	}
	return result
}

func workflowTaskDefinitionViewFrom(value domain.WorkflowTaskDefinition) workflowTaskDefinitionView {
	return workflowTaskDefinitionView{
		ID: string(value.ID), Title: value.Title, Description: value.Description,
		AcceptanceCriteria: value.AcceptanceCriteria, Executor: string(value.Executor),
		AllowedRoles: stringSlice(value.AllowedRoles), Execution: string(value.Execution),
		ReviewPolicy: string(value.ReviewPolicy), Tags: stringSlice(value.DefaultTags),
		Artifacts: artifactDefinitionViews(value.Artifacts),
	}
}

func artifactDefinitionViews(values []domain.ArtifactDefinition) []artifactDefinitionView {
	result := make([]artifactDefinitionView, 0, len(values))
	for _, value := range values {
		result = append(result, artifactDefinitionView{Name: value.Name, Description: value.Description})
	}
	return result
}

func artifactViews(values []domain.Artifact) []artifactView {
	result := make([]artifactView, 0, len(values))
	for _, value := range values {
		result = append(result, artifactViewFrom(value))
	}
	return result
}

func artifactViewFrom(value domain.Artifact) artifactView {
	return artifactView{
		ID: string(value.ID), WorkItemID: string(value.WorkItemID), TaskID: string(value.TaskID), ClaimID: string(value.ClaimID),
		SubmissionID: optionalString(value.SubmissionID), Name: value.Name, URI: value.URI,
	}
}

func workflowChoiceTargetViews(values []application.WorkflowChoiceRelation) []workflowTaskDefinitionView {
	result := make([]workflowTaskDefinitionView, 0, len(values))
	for _, value := range values {
		target := workflowTaskDefinitionViewFrom(value.Target)
		target.RelationGuidance = value.AgentGuidance
		if target.RelationGuidance == "" {
			target.RelationGuidance = value.Label
		}
		result = append(result, target)
	}
	return result
}

func actorViewFrom(value domain.ActorRef) actorView {
	return actorView{Kind: string(value.Kind), ID: string(value.ID)}
}

func actorViewPtr(value *domain.ActorRef) *actorView {
	if value == nil {
		return nil
	}
	view := actorViewFrom(*value)
	return &view
}

func latestResult(task domain.Task) string {
	if len(task.Submissions) == 0 {
		return ""
	}
	return task.Submissions[len(task.Submissions)-1].Result
}

func latestRetryPrompt(task domain.Task) string {
	if len(task.Failures) == 0 {
		return ""
	}
	return task.Failures[len(task.Failures)-1].RetryPrompt
}

func optionalString[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func stringSlice(values []string) []string {
	return append([]string{}, values...)
}

func stringValues[T ~string](values []T) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}
