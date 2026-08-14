package domain

import (
	"fmt"
	"strings"
)

// WorkflowTaskDefinition describes one repeatable Task in a Workflow Definition.
type WorkflowTaskDefinition struct {
	ID WorkflowTaskID

	Title              string
	Description        string
	AcceptanceCriteria string

	Executor     ExecutorRequirement
	AllowedRoles []string

	// Execution applies when this Task is reached through an Exit Group. A
	// selected Continue Group always keeps its target Task.
	Execution    ExecutionPolicy
	ReviewPolicy ReviewPolicy

	// DefaultTags are copied to each runtime Task instance. Executors may add or
	// adjust tags during execution.
	DefaultTags []string
}

// Validate checks the WorkflowTaskDefinition invariants.
func (t WorkflowTaskDefinition) Validate() error {
	if strings.TrimSpace(string(t.ID)) == "" {
		return invalid("workflow.tasks.id", "is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return invalid("workflow.tasks.title", "is required for task %q", t.ID)
	}
	if !t.Executor.Valid() {
		return invalid("workflow.tasks.executor", "unsupported value %q for task %q", t.Executor, t.ID)
	}
	if !t.Execution.Valid() {
		return invalid("workflow.tasks.execution", "unsupported value %q for task %q", t.Execution, t.ID)
	}
	if !t.ReviewPolicy.Valid() {
		return invalid("workflow.tasks.review_policy", "unsupported value %q for task %q", t.ReviewPolicy, t.ID)
	}
	if err := validateStringSet("workflow.tasks.allowed_roles", t.AllowedRoles); err != nil {
		return err
	}
	if err := validateStringSet("workflow.tasks.default_tags", t.DefaultTags); err != nil {
		return err
	}
	if t.Executor == ExecutorHuman && len(t.AllowedRoles) > 0 {
		return invalid("workflow.tasks.allowed_roles", "must be empty for human-only task %q", t.ID)
	}
	return nil
}

// WorkflowRelationDefinition is a directed edge between two Task Definitions.
type WorkflowRelationDefinition struct {
	ID WorkflowRelationID

	FromTaskID WorkflowTaskID
	ToTaskID   WorkflowTaskID
}

// Validate checks the WorkflowRelationDefinition's local invariants.
func (r WorkflowRelationDefinition) Validate() error {
	if strings.TrimSpace(string(r.ID)) == "" {
		return invalid("workflow.relations.id", "is required")
	}
	if strings.TrimSpace(string(r.FromTaskID)) == "" {
		return invalid("workflow.relations.from_task_id", "is required for relation %q", r.ID)
	}
	if strings.TrimSpace(string(r.ToTaskID)) == "" {
		return invalid("workflow.relations.to_task_id", "is required for relation %q", r.ID)
	}
	return nil
}

// WorkflowGraph contains the author-provided Workflow structure.
type WorkflowGraph struct {
	// StartTaskIDs are instantiated when a WorkItem is created.
	StartTaskIDs []WorkflowTaskID

	Tasks     []WorkflowTaskDefinition
	Relations []WorkflowRelationDefinition

	// MaxTaskExecutions is a safety limit for one WorkItem. Zero uses the system default.
	MaxTaskExecutions int
}

// WorkflowChoiceGroupKind describes a derived continuation or exit choice.
type WorkflowChoiceGroupKind string

const (
	WorkflowChoiceGroupContinue WorkflowChoiceGroupKind = "continue"
	WorkflowChoiceGroupExit     WorkflowChoiceGroupKind = "exit"
)

// WorkflowChoiceGroup is derived from graph topology when a Definition is published.
type WorkflowChoiceGroup struct {
	ID          WorkflowChoiceGroupID
	Kind        WorkflowChoiceGroupKind
	RelationIDs []WorkflowRelationID
}

// CompiledWorkflowGraph contains deterministic runtime choices for each Task Definition.
type CompiledWorkflowGraph struct {
	ChoiceGroups map[WorkflowTaskID][]WorkflowChoiceGroup
}

// GroupsFor returns a copy of the choice groups derived for a Task Definition.
func (g CompiledWorkflowGraph) GroupsFor(taskID WorkflowTaskID) []WorkflowChoiceGroup {
	groups := g.ChoiceGroups[taskID]
	result := make([]WorkflowChoiceGroup, len(groups))
	for i, group := range groups {
		result[i] = group
		result[i].RelationIDs = append([]WorkflowRelationID(nil), group.RelationIDs...)
	}
	return result
}

// Validate checks the WorkflowGraph invariants required for publication.
func (g WorkflowGraph) Validate() error {
	_, err := g.analyze()
	return err
}

// Compile validates the graph and derives mutually exclusive choice groups.
func (g WorkflowGraph) Compile() (CompiledWorkflowGraph, error) {
	analysis, err := g.analyze()
	if err != nil {
		return CompiledWorkflowGraph{}, err
	}

	compiled := CompiledWorkflowGraph{
		ChoiceGroups: make(map[WorkflowTaskID][]WorkflowChoiceGroup, len(g.Tasks)),
	}
	for _, task := range g.Tasks {
		var groups []WorkflowChoiceGroup
		var exits []WorkflowRelationID
		for _, relation := range analysis.outgoing[task.ID] {
			fromComponent := analysis.componentByTask[relation.FromTaskID]
			toComponent := analysis.componentByTask[relation.ToTaskID]
			if fromComponent == toComponent && analysis.components[fromComponent].cyclic {
				groups = append(groups, WorkflowChoiceGroup{
					ID:          WorkflowChoiceGroupID("continue:" + string(relation.ID)),
					Kind:        WorkflowChoiceGroupContinue,
					RelationIDs: []WorkflowRelationID{relation.ID},
				})
				continue
			}
			exits = append(exits, relation.ID)
		}
		if len(exits) > 0 {
			groups = append(groups, WorkflowChoiceGroup{
				ID:          WorkflowChoiceGroupID("exit:" + string(task.ID)),
				Kind:        WorkflowChoiceGroupExit,
				RelationIDs: exits,
			})
		}
		compiled.ChoiceGroups[task.ID] = groups
	}

	return compiled, nil
}

// ValidateDecision checks an applied runtime decision against this Definition graph.
func (g WorkflowGraph) ValidateDecision(sourceTaskID WorkflowTaskID, decision TransitionDecision) error {
	if err := decision.Validate(); err != nil {
		return err
	}
	compiled, err := g.Compile()
	if err != nil {
		return err
	}

	var selected *WorkflowChoiceGroup
	for _, group := range compiled.GroupsFor(sourceTaskID) {
		if group.ID == decision.ChoiceGroupID {
			group := group
			selected = &group
			break
		}
	}
	if selected == nil {
		return invalid(
			"transition_decision.choice_group_id",
			"group %q does not belong to workflow task %q",
			decision.ChoiceGroupID,
			sourceTaskID,
		)
	}

	triggered := make(map[WorkflowRelationID]struct{}, len(decision.TriggeredRelationIDs))
	for _, relationID := range decision.TriggeredRelationIDs {
		triggered[relationID] = struct{}{}
	}
	skipped := make(map[WorkflowRelationID]struct{}, len(decision.SkippedRelationIDs))
	for _, relationID := range decision.SkippedRelationIDs {
		skipped[relationID] = struct{}{}
	}
	reviewRequested := make(map[WorkflowRelationID]struct{}, len(decision.ReviewRequestedRelationIDs))
	for _, relationID := range decision.ReviewRequestedRelationIDs {
		reviewRequested[relationID] = struct{}{}
	}
	intendedSkips := make(map[WorkflowTaskID]struct{}, len(decision.SkipTaskIDs))
	for _, taskID := range decision.SkipTaskIDs {
		intendedSkips[taskID] = struct{}{}
	}
	intendedReviews := make(map[WorkflowTaskID]struct{}, len(decision.ReviewRequestedTaskIDs))
	for _, taskID := range decision.ReviewRequestedTaskIDs {
		intendedReviews[taskID] = struct{}{}
	}
	if len(triggered)+len(skipped) != len(selected.RelationIDs) {
		return invalid("transition_decision.relations", "must partition the selected choice group")
	}

	relations := make(map[WorkflowRelationID]WorkflowRelationDefinition, len(g.Relations))
	for _, relation := range g.Relations {
		relations[relation.ID] = relation
	}
	tasks := make(map[WorkflowTaskID]WorkflowTaskDefinition, len(g.Tasks))
	for _, task := range g.Tasks {
		tasks[task.ID] = task
	}
	for _, relationID := range selected.RelationIDs {
		_, isTriggered := triggered[relationID]
		_, isSkipped := skipped[relationID]
		if isTriggered == isSkipped {
			return invalid("transition_decision.relations", "relation %q must be triggered or skipped", relationID)
		}
		if selected.Kind == WorkflowChoiceGroupContinue && isSkipped {
			return invalid("transition_decision.skipped_relation_ids", "continue relation %q cannot be skipped", relationID)
		}
		if selected.Kind != WorkflowChoiceGroupExit || !isSkipped {
			if selected.Kind == WorkflowChoiceGroupExit {
				target := tasks[relations[relationID].ToTaskID]
				if _, intended := intendedSkips[target.ID]; intended {
					return invalid("transition_decision.skip_task_ids", "task %q must be skipped in the selected group", target.ID)
				}
			}
			continue
		}
		target := tasks[relations[relationID].ToTaskID]
		if target.Execution != ExecutionOptional {
			return invalid("transition_decision.skipped_relation_ids", "required relation %q cannot be skipped", relationID)
		}
		if _, intended := intendedSkips[target.ID]; !intended {
			return invalid("transition_decision.skip_task_ids", "task %q must be present for skipped relation %q", target.ID, relationID)
		}
		_, actualReview := reviewRequested[relationID]
		_, intendedReview := intendedReviews[target.ID]
		if actualReview != intendedReview {
			return invalid("transition_decision.review_requested_task_ids", "task %q review intent does not match relation %q", target.ID, relationID)
		}
	}
	return nil
}

type workflowComponent struct {
	tasks  []WorkflowTaskID
	cyclic bool
}

type workflowGraphAnalysis struct {
	outgoing        map[WorkflowTaskID][]WorkflowRelationDefinition
	components      []workflowComponent
	componentByTask map[WorkflowTaskID]int
}

func (g WorkflowGraph) analyze() (workflowGraphAnalysis, error) {
	if len(g.Tasks) == 0 {
		return workflowGraphAnalysis{}, invalid("workflow.tasks", "must not be empty")
	}
	if len(g.StartTaskIDs) == 0 {
		return workflowGraphAnalysis{}, invalid("workflow.start_task_ids", "must not be empty")
	}
	if g.MaxTaskExecutions < 0 {
		return workflowGraphAnalysis{}, invalid("workflow.max_task_executions", "must not be negative")
	}
	if g.MaxTaskExecutions > 0 && g.MaxTaskExecutions < len(g.StartTaskIDs) {
		return workflowGraphAnalysis{}, invalid("workflow.max_task_executions", "must cover all start tasks")
	}

	tasks := make(map[WorkflowTaskID]WorkflowTaskDefinition, len(g.Tasks))
	for _, task := range g.Tasks {
		if err := task.Validate(); err != nil {
			return workflowGraphAnalysis{}, err
		}
		if _, ok := tasks[task.ID]; ok {
			return workflowGraphAnalysis{}, invalid("workflow.tasks", "contains duplicate task %q", task.ID)
		}
		tasks[task.ID] = task
	}

	seenStarts := make(map[WorkflowTaskID]struct{}, len(g.StartTaskIDs))
	for _, taskID := range g.StartTaskIDs {
		task, ok := tasks[taskID]
		if !ok {
			return workflowGraphAnalysis{}, invalid("workflow.start_task_ids", "task %q does not exist", taskID)
		}
		if _, ok := seenStarts[taskID]; ok {
			return workflowGraphAnalysis{}, invalid("workflow.start_task_ids", "contains duplicate task %q", taskID)
		}
		seenStarts[taskID] = struct{}{}
		if task.Execution != ExecutionRequired {
			return workflowGraphAnalysis{}, invalid("workflow.start_task_ids", "task %q must be required", taskID)
		}
	}

	outgoing := make(map[WorkflowTaskID][]WorkflowRelationDefinition, len(g.Tasks))
	seenRelations := make(map[WorkflowRelationID]struct{}, len(g.Relations))
	type endpoints struct {
		from WorkflowTaskID
		to   WorkflowTaskID
	}
	seenEndpoints := make(map[endpoints]struct{}, len(g.Relations))
	for _, relation := range g.Relations {
		if err := relation.Validate(); err != nil {
			return workflowGraphAnalysis{}, err
		}
		if _, ok := seenRelations[relation.ID]; ok {
			return workflowGraphAnalysis{}, invalid("workflow.relations", "contains duplicate relation %q", relation.ID)
		}
		seenRelations[relation.ID] = struct{}{}
		if _, ok := tasks[relation.FromTaskID]; !ok {
			return workflowGraphAnalysis{}, invalid("workflow.relations", "from task %q does not exist", relation.FromTaskID)
		}
		if _, ok := tasks[relation.ToTaskID]; !ok {
			return workflowGraphAnalysis{}, invalid("workflow.relations", "to task %q does not exist", relation.ToTaskID)
		}
		key := endpoints{from: relation.FromTaskID, to: relation.ToTaskID}
		if _, ok := seenEndpoints[key]; ok {
			return workflowGraphAnalysis{}, invalid("workflow.relations", "contains duplicate edge %q -> %q", key.from, key.to)
		}
		seenEndpoints[key] = struct{}{}
		outgoing[relation.FromTaskID] = append(outgoing[relation.FromTaskID], relation)
	}

	reachable := make(map[WorkflowTaskID]struct{}, len(g.Tasks))
	queue := append([]WorkflowTaskID(nil), g.StartTaskIDs...)
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		if _, ok := reachable[taskID]; ok {
			continue
		}
		reachable[taskID] = struct{}{}
		for _, relation := range outgoing[taskID] {
			queue = append(queue, relation.ToTaskID)
		}
	}
	if len(reachable) != len(tasks) {
		for _, task := range g.Tasks {
			if _, ok := reachable[task.ID]; !ok {
				return workflowGraphAnalysis{}, invalid("workflow.tasks", "task %q is unreachable from the start tasks", task.ID)
			}
		}
	}

	components, componentByTask := workflowStronglyConnectedComponents(g.Tasks, outgoing)
	hasExternalExit := make([]bool, len(components))
	externalEntries := make([][]WorkflowRelationDefinition, len(components))
	cycleExitsByTarget := make(map[WorkflowTaskID]map[int][]WorkflowRelationDefinition)
	for _, relation := range g.Relations {
		from := componentByTask[relation.FromTaskID]
		to := componentByTask[relation.ToTaskID]
		if from != to {
			hasExternalExit[from] = true
			externalEntries[to] = append(externalEntries[to], relation)
			if components[from].cyclic {
				if cycleExitsByTarget[relation.ToTaskID] == nil {
					cycleExitsByTarget[relation.ToTaskID] = make(map[int][]WorkflowRelationDefinition)
				}
				cycleExitsByTarget[relation.ToTaskID][from] = append(
					cycleExitsByTarget[relation.ToTaskID][from],
					relation,
				)
			}
		}
	}
	for i, component := range components {
		if component.cyclic && !hasExternalExit[i] {
			return workflowGraphAnalysis{}, invalid(
				"workflow.relations",
				"cycle containing tasks %s has no exit",
				formatWorkflowTaskIDs(component.tasks),
			)
		}
	}
	startsByComponent := make([]int, len(components))
	for _, taskID := range g.StartTaskIDs {
		startsByComponent[componentByTask[taskID]]++
	}
	for index, component := range components {
		if !component.cyclic {
			continue
		}
		if len(externalEntries[index])+startsByComponent[index] > 1 {
			return workflowGraphAnalysis{}, invalid(
				"workflow.relations",
				"cycle containing tasks %s has multiple entry paths",
				formatWorkflowTaskIDs(component.tasks),
			)
		}
	}
	for targetTaskID, exitsByComponent := range cycleExitsByTarget {
		for componentIndex, relations := range exitsByComponent {
			if len(relations) <= 1 {
				continue
			}
			return workflowGraphAnalysis{}, invalid(
				"workflow.relations",
				"task %q has multiple alternative exits from cycle containing tasks %s",
				targetTaskID,
				formatWorkflowTaskIDs(components[componentIndex].tasks),
			)
		}
	}

	return workflowGraphAnalysis{
		outgoing:        outgoing,
		components:      components,
		componentByTask: componentByTask,
	}, nil
}

func workflowStronglyConnectedComponents(
	tasks []WorkflowTaskDefinition,
	outgoing map[WorkflowTaskID][]WorkflowRelationDefinition,
) ([]workflowComponent, map[WorkflowTaskID]int) {
	index := 0
	indices := make(map[WorkflowTaskID]int, len(tasks))
	lowlink := make(map[WorkflowTaskID]int, len(tasks))
	onStack := make(map[WorkflowTaskID]bool, len(tasks))
	stack := make([]WorkflowTaskID, 0, len(tasks))
	components := make([]workflowComponent, 0)
	componentByTask := make(map[WorkflowTaskID]int, len(tasks))

	var visit func(WorkflowTaskID)
	visit = func(taskID WorkflowTaskID) {
		indices[taskID] = index
		lowlink[taskID] = index
		index++
		stack = append(stack, taskID)
		onStack[taskID] = true

		for _, relation := range outgoing[taskID] {
			next := relation.ToTaskID
			if _, ok := indices[next]; !ok {
				visit(next)
				lowlink[taskID] = min(lowlink[taskID], lowlink[next])
			} else if onStack[next] {
				lowlink[taskID] = min(lowlink[taskID], indices[next])
			}
		}

		if lowlink[taskID] != indices[taskID] {
			return
		}

		componentIndex := len(components)
		component := workflowComponent{}
		for {
			last := len(stack) - 1
			member := stack[last]
			stack = stack[:last]
			onStack[member] = false
			component.tasks = append(component.tasks, member)
			componentByTask[member] = componentIndex
			if member == taskID {
				break
			}
		}
		component.cyclic = len(component.tasks) > 1
		if len(component.tasks) == 1 {
			for _, relation := range outgoing[component.tasks[0]] {
				if relation.ToTaskID == component.tasks[0] {
					component.cyclic = true
					break
				}
			}
		}
		components = append(components, component)
	}

	for _, task := range tasks {
		if _, ok := indices[task.ID]; !ok {
			visit(task.ID)
		}
	}
	return components, componentByTask
}

func formatWorkflowTaskIDs(ids []WorkflowTaskID) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = string(id)
	}
	return fmt.Sprintf("[%s]", strings.Join(values, ", "))
}
