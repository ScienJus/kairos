package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWorkflowGraphCompileCycleChoices(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"start"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("start", ExecutionRequired),
			workflowTask("implementation", ExecutionRequired),
			workflowTask("test", ExecutionRequired),
			workflowTask("fix", ExecutionRequired),
			workflowTask("documentation", ExecutionOptional),
			workflowTask("release", ExecutionRequired),
		},
		Relations: []WorkflowRelationDefinition{
			workflowRelation("start-implementation", "start", "implementation"),
			workflowRelation("implementation-test", "implementation", "test"),
			workflowRelation("test-implementation", "test", "implementation"),
			workflowRelation("implementation-fix", "implementation", "fix"),
			workflowRelation("fix-implementation", "fix", "implementation"),
			workflowRelation("implementation-documentation", "implementation", "documentation"),
			workflowRelation("implementation-release", "implementation", "release"),
		},
		MaxTaskExecutions: 20,
	}

	compiled, err := graph.Compile()
	if err != nil {
		t.Fatalf("compile valid workflow graph: %v", err)
	}

	groups := compiled.GroupsFor("implementation")
	if len(groups) != 3 {
		t.Fatalf("implementation choice groups: got %d, want 3", len(groups))
	}
	assertWorkflowChoiceGroup(t, groups[0], WorkflowChoiceGroupContinue, "implementation-test")
	assertWorkflowChoiceGroup(t, groups[1], WorkflowChoiceGroupContinue, "implementation-fix")
	assertWorkflowChoiceGroup(
		t,
		groups[2],
		WorkflowChoiceGroupExit,
		"implementation-documentation",
		"implementation-release",
	)

	groups[2].RelationIDs[0] = "changed"
	if got := compiled.GroupsFor("implementation")[2].RelationIDs[0]; got != "implementation-documentation" {
		t.Fatalf("GroupsFor returned mutable relation IDs: got %q", got)
	}
}

func TestWorkflowGraphCompileDAGExitGroup(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"design"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("design", ExecutionRequired),
			workflowTask("frontend", ExecutionRequired),
			workflowTask("backend", ExecutionRequired),
		},
		Relations: []WorkflowRelationDefinition{
			workflowRelation("design-frontend", "design", "frontend"),
			workflowRelation("design-backend", "design", "backend"),
		},
	}

	compiled, err := graph.Compile()
	if err != nil {
		t.Fatalf("compile valid DAG: %v", err)
	}
	groups := compiled.GroupsFor("design")
	if len(groups) != 1 {
		t.Fatalf("design choice groups: got %d, want 1", len(groups))
	}
	assertWorkflowChoiceGroup(t, groups[0], WorkflowChoiceGroupExit, "design-frontend", "design-backend")
}

func TestWorkflowGraphSupportsMultipleRequiredStarts(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"frontend", "backend"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("frontend", ExecutionRequired),
			workflowTask("backend", ExecutionRequired),
		},
		MaxTaskExecutions: 2,
	}

	if err := graph.Validate(); err != nil {
		t.Fatalf("validate multiple starts: %v", err)
	}
}

func TestWorkflowGraphRejectsOptionalStart(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"start"},
		Tasks:        []WorkflowTaskDefinition{workflowTask("start", ExecutionOptional)},
	}

	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("optional start: got %v", err)
	}
}

func TestWorkflowGraphAcceptsAllDefinitionTasksAsStarts(t *testing.T) {
	t.Parallel()

	tasks := make([]WorkflowTaskDefinition, MaxWorkflowTasks)
	starts := make([]WorkflowTaskID, MaxWorkflowTasks)
	for index := range tasks {
		id := WorkflowTaskID(fmt.Sprintf("start-%d", index))
		tasks[index] = workflowTask(id, ExecutionRequired)
		starts[index] = id
	}
	graph := WorkflowGraph{StartTaskIDs: starts, Tasks: tasks, MaxTaskExecutions: MaxWorkflowTasks}
	if err := graph.Validate(); err != nil {
		t.Fatalf("validate all definition tasks as starts: %v", err)
	}
}

func TestWorkflowGraphRejectsTooManyTasks(t *testing.T) {
	t.Parallel()

	tasks := make([]WorkflowTaskDefinition, MaxWorkflowTasks+1)
	for index := range tasks {
		tasks[index] = workflowTask(WorkflowTaskID(fmt.Sprintf("task-%d", index)), ExecutionRequired)
	}
	graph := WorkflowGraph{StartTaskIDs: []WorkflowTaskID{"task-0"}, Tasks: tasks}
	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) || !strings.Contains(err.Error(), "workflow.tasks: must contain at most") {
		t.Fatalf("too many tasks: got %v", err)
	}
}

func TestWorkflowGraphRejectsTooManyRelations(t *testing.T) {
	t.Parallel()

	relations := make([]WorkflowRelationDefinition, MaxWorkflowRelations+1)
	for index := range relations {
		relations[index] = workflowRelation(
			WorkflowRelationID(fmt.Sprintf("relation-%d", index)),
			"start",
			"target",
		)
	}
	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"start"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("start", ExecutionRequired),
			workflowTask("target", ExecutionRequired),
		},
		Relations: relations,
	}
	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) || !strings.Contains(err.Error(), "workflow.relations: must contain at most") {
		t.Fatalf("too many relations: got %v", err)
	}
}

func TestWorkflowGraphRejectsTaskExecutionLimitAboveMaximum(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs:      []WorkflowTaskID{"start"},
		Tasks:             []WorkflowTaskDefinition{workflowTask("start", ExecutionRequired)},
		MaxTaskExecutions: MaxWorkflowTaskExecutions + 1,
	}
	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("execution limit above maximum: got %v", err)
	}
}

func TestWorkflowGraphRejectsUnreachableTask(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"start"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("start", ExecutionRequired),
			workflowTask("orphan", ExecutionRequired),
		},
	}

	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("unreachable task: got %v", err)
	}
}

func TestWorkflowGraphRejectsCycleWithoutExit(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"implementation"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("implementation", ExecutionRequired),
			workflowTask("test", ExecutionRequired),
		},
		Relations: []WorkflowRelationDefinition{
			workflowRelation("implementation-test", "implementation", "test"),
			workflowRelation("test-implementation", "test", "implementation"),
		},
	}

	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("cycle without exit: got %v", err)
	}
}

func TestWorkflowGraphRejectsAmbiguousCycleExitJoin(t *testing.T) {
	t.Parallel()

	graph := WorkflowGraph{
		StartTaskIDs: []WorkflowTaskID{"a"},
		Tasks: []WorkflowTaskDefinition{
			workflowTask("a", ExecutionRequired),
			workflowTask("b", ExecutionRequired),
			workflowTask("joined", ExecutionRequired),
		},
		Relations: []WorkflowRelationDefinition{
			workflowRelation("a-b", "a", "b"),
			workflowRelation("b-a", "b", "a"),
			workflowRelation("a-joined", "a", "joined"),
			workflowRelation("b-joined", "b", "joined"),
		},
	}

	if err := graph.Validate(); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("ambiguous cycle exits: got %v", err)
	}
}

func workflowTask(id WorkflowTaskID, execution ExecutionPolicy) WorkflowTaskDefinition {
	return WorkflowTaskDefinition{
		ID:           id,
		Title:        string(id),
		Executor:     ExecutorAgent,
		Execution:    execution,
		ReviewPolicy: ReviewNone,
	}
}

func workflowRelation(id WorkflowRelationID, from, to WorkflowTaskID) WorkflowRelationDefinition {
	return WorkflowRelationDefinition{ID: id, FromTaskID: from, ToTaskID: to}
}

func assertWorkflowChoiceGroup(
	t *testing.T,
	group WorkflowChoiceGroup,
	kind WorkflowChoiceGroupKind,
	relationIDs ...WorkflowRelationID,
) {
	t.Helper()
	if group.Kind != kind {
		t.Fatalf("choice group kind: got %q, want %q", group.Kind, kind)
	}
	if len(group.RelationIDs) != len(relationIDs) {
		t.Fatalf("choice group relations: got %v, want %v", group.RelationIDs, relationIDs)
	}
	for i, relationID := range relationIDs {
		if group.RelationIDs[i] != relationID {
			t.Fatalf("choice group relation %d: got %q, want %q", i, group.RelationIDs[i], relationID)
		}
	}
}
