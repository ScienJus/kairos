package domain

// ValidateBlackboardTaskHierarchy verifies the open task forest of one WorkItem.
func ValidateBlackboardTaskHierarchy(workItemID WorkItemID, tasks []Task) error {
	byID := make(map[TaskID]Task, len(tasks))
	children := make(map[TaskID][]Task)
	for _, task := range tasks {
		if task.WorkItemID != workItemID {
			return invalid("tasks.work_item_id", "task %q belongs to another work item", task.ID)
		}
		if _, exists := byID[task.ID]; exists {
			return invalid("tasks", "contains duplicate task %q", task.ID)
		}
		byID[task.ID] = task
	}
	for _, task := range tasks {
		if task.ParentTaskID == nil {
			continue
		}
		parent, exists := byID[*task.ParentTaskID]
		if !exists {
			return invalid("parent_task_id", "task %q references missing parent %q", task.ID, *task.ParentTaskID)
		}
		if parent.DecomposedAt == nil {
			return invalid("parent_task_id", "parent task %q is not decomposed", parent.ID)
		}
		children[parent.ID] = append(children[parent.ID], task)
	}

	for _, task := range tasks {
		seen := map[TaskID]struct{}{task.ID: {}}
		current := task
		for current.ParentTaskID != nil {
			if _, exists := seen[*current.ParentTaskID]; exists {
				return invalid("parent_task_id", "task hierarchy contains a cycle at %q", *current.ParentTaskID)
			}
			seen[*current.ParentTaskID] = struct{}{}
			current = byID[*current.ParentTaskID]
		}
	}

	for _, task := range tasks {
		direct := children[task.ID]
		if task.DecomposedAt == nil {
			if len(direct) > 0 {
				return invalid("tasks", "execution task %q must not contain child tasks", task.ID)
			}
			continue
		}
		if len(direct) == 0 {
			return invalid("tasks", "decomposed task %q requires at least one child", task.ID)
		}
		allEnded := true
		for _, child := range direct {
			if child.Status != TaskStatusCompleted && child.Status != TaskStatusSkipped {
				allEnded = false
				break
			}
		}
		if task.Status == TaskStatusCompleted && !allEnded {
			return invalid("status", "aggregate task %q completed before its children", task.ID)
		}
		if task.Status == TaskStatusWaitingChildren && allEnded {
			return invalid("status", "aggregate task %q must complete after its children", task.ID)
		}
	}
	return nil
}
