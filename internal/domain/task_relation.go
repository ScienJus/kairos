package domain

import (
	"strings"
	"time"
)

// TaskRelation is a directed edge between two concrete Task instances.
type TaskRelation struct {
	// WorkItemID is the parent WorkItem of both Tasks. [Both]
	WorkItemID WorkItemID

	// FromTaskID is the preceding Task instance. [Both]
	FromTaskID TaskID

	// ToTaskID is the subsequent Task instance. [Both]
	ToTaskID TaskID

	CreatedAt time.Time
}

// Validate checks the TaskRelation invariants.
func (r TaskRelation) Validate() error {
	if strings.TrimSpace(string(r.WorkItemID)) == "" {
		return invalid("relation.work_item_id", "is required")
	}
	if strings.TrimSpace(string(r.FromTaskID)) == "" {
		return invalid("relation.from_task_id", "is required")
	}
	if strings.TrimSpace(string(r.ToTaskID)) == "" {
		return invalid("relation.to_task_id", "is required")
	}
	if r.FromTaskID == r.ToTaskID {
		return invalid("relation", "must not point a task to itself")
	}
	if r.CreatedAt.IsZero() {
		return invalid("relation.created_at", "is required")
	}
	return nil
}

// ValidateRuntimeTaskGraph verifies that concrete Task instances and their
// relations belong to one WorkItem and form an acyclic execution history.
func ValidateRuntimeTaskGraph(workItemID WorkItemID, tasks []Task, relations []TaskRelation) error {
	if strings.TrimSpace(string(workItemID)) == "" {
		return invalid("work_item_id", "is required")
	}

	indegree := make(map[TaskID]int, len(tasks))
	adjacency := make(map[TaskID][]TaskID, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(string(task.ID)) == "" {
			return invalid("tasks", "contains a task without an id")
		}
		if task.WorkItemID != workItemID {
			return invalid("tasks", "task %q belongs to another work item", task.ID)
		}
		if _, exists := indegree[task.ID]; exists {
			return invalid("tasks", "contains duplicate task %q", task.ID)
		}
		indegree[task.ID] = 0
	}

	type edge struct {
		from TaskID
		to   TaskID
	}
	seen := make(map[edge]struct{}, len(relations))
	for _, relation := range relations {
		if err := relation.Validate(); err != nil {
			return err
		}
		if relation.WorkItemID != workItemID {
			return invalid("relations", "relation belongs to another work item")
		}
		if _, exists := indegree[relation.FromTaskID]; !exists {
			return invalid("relations", "from task %q does not exist", relation.FromTaskID)
		}
		if _, exists := indegree[relation.ToTaskID]; !exists {
			return invalid("relations", "to task %q does not exist", relation.ToTaskID)
		}

		key := edge{from: relation.FromTaskID, to: relation.ToTaskID}
		if _, exists := seen[key]; exists {
			return invalid("relations", "contains duplicate relation %q -> %q", key.from, key.to)
		}
		seen[key] = struct{}{}
		adjacency[key.from] = append(adjacency[key.from], key.to)
		indegree[key.to]++
	}

	queue := make([]TaskID, 0, len(tasks))
	for taskID, count := range indegree {
		if count == 0 {
			queue = append(queue, taskID)
		}
	}

	visited := 0
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adjacency[taskID] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(tasks) {
		return invalid("relations", "runtime task graph contains a cycle")
	}
	return nil
}
