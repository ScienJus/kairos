package application

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ScienJus/kairos/internal/domain"
)

func currentWorkflowAttempts(tasks []domain.Task) []domain.Task {
	replaced := make(map[domain.TaskID]bool)
	for _, task := range tasks {
		if task.RetryOfTaskID != nil {
			replaced[*task.RetryOfTaskID] = true
		}
	}
	result := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if !replaced[task.ID] {
			result = append(result, task)
		}
	}
	return result
}

// Generated summaries are bounded separately from user-authored instructions.
func recoverySummary(value string) string {
	return boundedRecoveryText(value, domain.MaxHistoryTextBytes)
}

func boundedRecoveryText(value string, budget int) string {
	const suffix = "\n[Earlier details omitted; the source retains the full history.]"
	if len(value) <= budget {
		return value
	}
	value = value[:budget-len(suffix)]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + suffix
}

type workflowRetryLimitError struct {
	Node         domain.WorkflowTaskID
	Count, Limit int
}

func (e *workflowRetryLimitError) Error() string {
	return fmt.Sprintf("workflow node %q has %d task instances at limit %d; increase max_task_instances_per_node to retry", e.Node, e.Count, e.Limit)
}

// workflowRetryState belongs to one transaction's replacement batch. Historical
// snapshots are indexed once; successful replacements advance counts and position.
type workflowRetryState struct {
	definitions   map[domain.WorkflowTaskID]domain.WorkflowTaskDefinition
	activations   map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation
	taskInstances map[domain.WorkflowTaskID]int
	replaced      map[domain.TaskID]bool
	incoming      map[domain.TaskID][]domain.TaskRelation
	position      int64
	limit         int
}

func newWorkflowRetryState(store ReadStore, work domain.WorkItem, definition domain.WorkflowDefinition, tasks []domain.Task, activations []domain.WorkflowTaskActivation) (*workflowRetryState, error) {
	relations, err := store.ListTaskRelations(work.ID)
	if err != nil {
		return nil, err
	}
	state := &workflowRetryState{
		definitions:   make(map[domain.WorkflowTaskID]domain.WorkflowTaskDefinition, len(definition.Graph.Tasks)),
		activations:   make(map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation, len(activations)),
		taskInstances: make(map[domain.WorkflowTaskID]int),
		replaced:      make(map[domain.TaskID]bool),
		incoming:      make(map[domain.TaskID][]domain.TaskRelation),
		position:      nextTaskPosition(tasks),
		limit:         effectiveWorkflowTaskInstanceLimit(work, definition),
	}
	for _, node := range definition.Graph.Tasks {
		state.definitions[node.ID] = node
	}
	for _, activation := range activations {
		state.activations[activation.ID] = activation
		if activation.Status == domain.WorkflowActivationResolved {
			state.taskInstances[activation.WorkflowTaskID]++
		}
	}
	for _, task := range tasks {
		if task.RetryOfTaskID != nil {
			state.replaced[*task.RetryOfTaskID] = true
		}
	}
	for _, relation := range relations {
		state.incoming[relation.ToTaskID] = append(state.incoming[relation.ToTaskID], relation)
	}
	return state, nil
}

// Automatic retry prepares the same state as Human recovery, for one Task.
func loadWorkflowRetryState(store ReadStore, work domain.WorkItem) (*workflowRetryState, error) {
	tasks, err := store.ListTasks(work.ID)
	if err != nil {
		return nil, err
	}
	definition, err := store.GetWorkflowDefinition(work.Definition.ID, work.Definition.Version)
	if err != nil {
		return nil, err
	}
	activations, err := store.ListWorkflowTaskActivations(work.ID)
	if err != nil {
		return nil, err
	}
	return newWorkflowRetryState(store, work, definition, tasks, activations)
}

// retryWorkflowTask preserves the activation's correlation and input receipts:
// A2 therefore joins the successful B1, without replaying B or upstream decisions.
func (s *Service) retryWorkflowTask(store WriteStore, state *workflowRetryState, work domain.WorkItem, source domain.Task, actor domain.ActorRef, instructions, workflowFailure string) (domain.Task, error) {
	if work.CoordinationMode() != domain.CoordinationModeWorkflow || source.WorkflowTaskID == nil || source.WorkflowActivationID == nil {
		return domain.Task{}, conflict("only Workflow tasks can create a replacement attempt")
	}
	if source.Status != domain.TaskStatusFailed && source.Status != domain.TaskStatusPending {
		return domain.Task{}, conflict("only failed or interrupted attempts can be retried")
	}
	if state.replaced[source.ID] {
		return domain.Task{}, conflict("task already has a replacement attempt")
	}
	count := state.taskInstances[*source.WorkflowTaskID]
	if count >= state.limit {
		return domain.Task{}, &workflowRetryLimitError{Node: *source.WorkflowTaskID, Count: count, Limit: state.limit}
	}
	original, ok := state.activations[*source.WorkflowActivationID]
	if !ok {
		return domain.Task{}, conflict("source workflow activation is missing")
	}
	id, err := s.newID("workflow activation id")
	if err != nil {
		return domain.Task{}, err
	}
	now := s.clock.Now()
	activation := original
	activation.ID = domain.WorkflowTaskActivationID(id)
	activation.CreatedAt = now
	activation.UpdatedAt = now
	activation.ResolvedAt = &now
	activation.Status = domain.WorkflowActivationResolved
	activation.Outcome = domain.WorkflowActivationCreated
	def, ok := state.definitions[*source.WorkflowTaskID]
	if !ok {
		return domain.Task{}, conflict("source workflow node is missing")
	}
	task, err := s.newWorkflowTask(work.ID, def, activation.ID, state.position, now)
	if err != nil {
		return domain.Task{}, err
	}
	task.RetryOfTaskID = &source.ID
	task.Tags = append([]string{}, source.Tags...)
	var context strings.Builder
	fmt.Fprintf(&context, "Retry of Task %s. Inspect existing external actions before repeating them.\n", source.ID)
	task.RetryInstructions = instructions
	// A limit can block automatic retry before its prompt reaches a replacement.
	if task.RetryInstructions == "" && len(source.Failures) > 0 {
		failure := source.Failures[len(source.Failures)-1]
		if failure.Action == domain.TaskFailureRetry {
			task.RetryInstructions = failure.RetryPrompt
		}
	}
	if task.RetryInstructions == "" {
		task.RetryInstructions = source.RetryInstructions
	}
	if feedback := rejectedReviewFeedback(source); feedback != "" {
		fmt.Fprintf(&context, "Previous review rejection:\n%s\n", boundedRecoveryText(feedback, domain.MaxHistoryTextBytes/4))
	}
	// Current causes precede bounded history. Retry instructions already have
	// their own field and must not consume the summary's space a second time.
	latestFailure := ""
	if len(source.Failures) > 0 {
		latestFailure = source.Failures[len(source.Failures)-1].Reason
		fmt.Fprintf(&context, "Previous failure: %s\n", boundedRecoveryText(latestFailure, domain.MaxHistoryTextBytes/4))
	}
	if workflowFailure != "" && workflowFailure != latestFailure {
		fmt.Fprintf(&context, "Workflow interruption: %s\n", boundedRecoveryText(workflowFailure, domain.MaxHistoryTextBytes/4))
	}
	if source.RetryContext != "" {
		fmt.Fprintln(&context, boundedRecoveryText(source.RetryContext, domain.MaxHistoryTextBytes/2))
	}
	artifacts, err := store.ListArtifacts(ArtifactFilter{WorkItemID: work.ID, TaskID: source.ID, SubmittedOnly: true})
	if err != nil {
		return domain.Task{}, err
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(&context, "Previous artifact %s: %s\n", artifact.Name, artifact.URI)
	}
	if len(source.Submissions) > 0 {
		fmt.Fprintf(&context, "Previous result:\n%s\n", source.Submissions[len(source.Submissions)-1].Result)
	}
	task.RetryContext = recoverySummary(context.String())
	if err := task.Validate(domain.CoordinationModeWorkflow); err != nil {
		return domain.Task{}, err
	}
	if err := activation.Validate(); err != nil {
		return domain.Task{}, err
	}
	if err := store.CreateWorkflowTaskActivation(activation); err != nil {
		return domain.Task{}, err
	}
	if err := store.CreateTask(task); err != nil {
		return domain.Task{}, err
	}
	for _, relation := range state.incoming[source.ID] {
		relation.ToTaskID = task.ID
		relation.CreatedAt = now
		if err := store.CreateTaskRelation(relation); err != nil {
			return domain.Task{}, err
		}
	}
	if err := s.appendEvent(store, work.ID, &task.ID, domain.WorkItemEventTaskCreated, string(task.ID), &actor, "retry of task "+string(source.ID)); err != nil {
		return domain.Task{}, err
	}
	state.taskInstances[*source.WorkflowTaskID]++
	state.position++
	state.replaced[source.ID] = true
	return task, nil
}

// Recovery applies to the whole current failure frontier, never an arbitrary Task.
func workflowRecoveryAvailable(work domain.WorkItem, tasks []domain.Task) bool {
	if work.CoordinationMode() != domain.CoordinationModeWorkflow {
		return false
	}
	if work.Status == domain.WorkItemStatusFailed {
		return true
	}
	if work.Status != domain.WorkItemStatusOpen {
		return false
	}
	for _, task := range currentWorkflowAttempts(tasks) {
		if task.Status == domain.TaskStatusFailed {
			return true
		}
	}
	return false
}

func interruptedWorkflowTask(claims []domain.Claim) bool {
	// Task Claims are revoked only by failWorkItem. A continued revoked attempt
	// is replaced, so a current (unreplaced) instance cannot contain an older
	// revocation followed by continued execution. Released/expired Claims are normal.
	for _, claim := range claims {
		if claim.EndReason == domain.ClaimEndRevoked && !claim.Active() {
			return true
		}
	}
	// A claim-cap failure happens before a Claim is acquired or revoked. This
	// exhausted Pending instance also needs replacement to become executable.
	return len(claims) >= MaxClaimsPerTask
}

// workflowRecoveryTasks is shared by execution and the console projection.
func workflowRecoveryTasks(work domain.WorkItem, tasks []domain.Task, claims []domain.Claim) []domain.Task {
	result := make([]domain.Task, 0)
	if !workflowRecoveryAvailable(work, tasks) {
		return result
	}
	byTask := make(map[domain.TaskID][]domain.Claim)
	for _, claim := range claims {
		byTask[claim.TaskID] = append(byTask[claim.TaskID], claim)
	}
	for _, task := range currentWorkflowAttempts(tasks) {
		if task.Status == domain.TaskStatusFailed || (work.Status == domain.WorkItemStatusFailed && task.Status == domain.TaskStatusPending && interruptedWorkflowTask(byTask[task.ID])) {
			result = append(result, task)
		}
	}
	return result
}

func rejectedReviewFeedback(task domain.Task) string {
	if len(task.Reviews) == 0 {
		return ""
	}
	review := task.Reviews[len(task.Reviews)-1]
	if review.Status != domain.ReviewStatusRejected {
		return ""
	}
	return review.Feedback
}
