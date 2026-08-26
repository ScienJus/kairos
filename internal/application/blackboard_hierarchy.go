package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// DecomposeBlackboardTaskCommand turns one claimed execution Task into an aggregate Task.
type DecomposeBlackboardTaskCommand struct {
	TaskID      domain.TaskID
	ClaimID     domain.ClaimID
	Identity    Identity
	OperationID string
	Children    []BlackboardTaskSpec
}

// BlackboardTaskDecomposition contains the aggregate Task and its initial children.
type BlackboardTaskDecomposition struct {
	Parent   domain.Task   `json:"parent"`
	Children []domain.Task `json:"children"`
}

// DecomposeBlackboardTask atomically transfers responsibility from a claimed Task to child Tasks.
func (s *Service) DecomposeBlackboardTask(
	ctx context.Context,
	command DecomposeBlackboardTaskCommand,
) (BlackboardTaskDecomposition, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ClaimID)) == "" {
		return BlackboardTaskDecomposition{}, invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return BlackboardTaskDecomposition{}, err
	}
	if len(command.Children) == 0 {
		return BlackboardTaskDecomposition{}, invalidCommand("at least one child task is required")
	}

	var result BlackboardTaskDecomposition
	err := s.replayableCreate(ctx, command.Identity, command.OperationID, "decompose_blackboard_task", command, &result, func(store WriteStore) error {
		parent, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(parent.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", parent.WorkItemID, err)
		}
		claims, err := store.ListClaims(parent.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", parent.ID, err)
		}
		if err := validateBlackboardTaskDecomposition(workItem, parent, claims, command.Identity, command.ClaimID); err != nil {
			return err
		}
		claimIndex := findClaim(claims, command.ClaimID)
		claim := claims[claimIndex]

		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list blackboard tasks: %w", err)
		}
		if err := ensureBlackboardTaskCapacity(len(tasks), len(command.Children)); err != nil {
			return err
		}
		now := s.clock.Now()
		nextPosition := nextTaskPosition(tasks)
		children := make([]domain.Task, 0, len(command.Children))
		for index, spec := range command.Children {
			id, err := s.newID("child task id")
			if err != nil {
				return err
			}
			child := newBlackboardTask(
				domain.TaskID(id), workItem.ID, &parent.ID, spec, nextPosition+int64(index), now,
			)
			if err := child.Validate(domain.CoordinationModeBlackboard); err != nil {
				return err
			}
			children = append(children, child)
		}

		claim.EndedAt = &now
		claim.EndReason = domain.ClaimEndTaskDecomposed
		claims[claimIndex] = claim
		parent.Status = domain.TaskStatusWaitingChildren
		parent.ActiveClaimID = nil
		parent.DecomposedAt = &now
		parent.UpdatedAt = now
		parent.Version++
		if err := domain.ValidateTaskContext(domain.CoordinationModeBlackboard, parent, claims); err != nil {
			return err
		}
		prospective := replaceTask(tasks, parent)
		prospective = append(prospective, children...)
		if err := domain.ValidateBlackboardTaskHierarchy(workItem.ID, prospective); err != nil {
			return err
		}

		if err := store.SaveClaim(claim); err != nil {
			return fmt.Errorf("save decomposition claim: %w", err)
		}
		if err := store.SaveTask(parent); err != nil {
			return fmt.Errorf("save aggregate task: %w", err)
		}
		for _, child := range children {
			if err := store.CreateTask(child); err != nil {
				return fmt.Errorf("create child task: %w", err)
			}
		}
		if err := advanceBlackboardRevision(store, &workItem, now); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &parent.ID, domain.WorkItemEventTaskDecomposed, string(parent.ID), &actor, ""); err != nil {
			return err
		}
		for _, child := range children {
			if err := s.appendEvent(store, workItem.ID, &child.ID, domain.WorkItemEventTaskCreated, string(child.ID), &actor, ""); err != nil {
				return err
			}
		}
		result = BlackboardTaskDecomposition{Parent: parent, Children: children}
		return nil
	})
	if err != nil {
		return BlackboardTaskDecomposition{}, err
	}
	return normalizeBlackboardTaskDecomposition(result), nil
}

func validateBlackboardTaskDecomposition(
	workItem domain.WorkItem,
	task domain.Task,
	claims []domain.Claim,
	identity Identity,
	claimID domain.ClaimID,
) error {
	if err := rejectCancelledWorkItem(workItem); err != nil {
		return err
	}
	if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
		return conflict("task %q does not belong to a blackboard", task.ID)
	}
	if workItem.Status != domain.WorkItemStatusOpen {
		return conflict("work item %q is %s", workItem.ID, workItem.Status)
	}
	claimIndex := findClaim(claims, claimID)
	if claimIndex < 0 {
		return fmt.Errorf("%w: claim %q", ErrNotFound, claimID)
	}
	claim := claims[claimIndex]
	if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || *task.ActiveClaimID != claim.ID || !claim.Active() {
		return conflict("claim %q is not active for task %q", claim.ID, task.ID)
	}
	if !sameActor(claim.Executor, identity.Actor) {
		return forbidden("actor does not own claim %q", claim.ID)
	}
	if task.DecomposedAt != nil || len(task.Submissions) > 0 || len(task.Reviews) > 0 || len(task.Failures) > 0 || len(task.TransitionDecisions) > 0 {
		return conflict("task %q already contains execution history", task.ID)
	}
	return nil
}

// AddBlackboardChildTaskCommand appends one Task to an open aggregate Task.
type AddBlackboardChildTaskCommand struct {
	ParentTaskID domain.TaskID
	Identity     Identity
	OperationID  string
	Task         BlackboardTaskSpec
}

// AddBlackboardChildTask appends one child while the aggregate Task remains open.
func (s *Service) AddBlackboardChildTask(
	ctx context.Context,
	command AddBlackboardChildTaskCommand,
) (domain.Task, error) {
	if strings.TrimSpace(string(command.ParentTaskID)) == "" {
		return domain.Task{}, invalidCommand("parent task id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Task{}, err
	}

	var created domain.Task
	err := s.replayableCreate(ctx, command.Identity, command.OperationID, "add_blackboard_child_task", command, &created, func(store WriteStore) error {
		parent, err := store.GetTask(command.ParentTaskID)
		if err != nil {
			return fmt.Errorf("get parent task %q: %w", command.ParentTaskID, err)
		}
		workItem, err := store.GetWorkItem(parent.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", parent.WorkItemID, err)
		}
		if err := rejectCancelledWorkItem(workItem); err != nil {
			return err
		}
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			return conflict("task %q does not belong to a blackboard", parent.ID)
		}
		if workItem.Status != domain.WorkItemStatusOpen || parent.Status != domain.TaskStatusWaitingChildren || parent.DecomposedAt == nil {
			return conflict("aggregate task %q is closed", parent.ID)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list blackboard tasks: %w", err)
		}
		if err := ensureBlackboardTaskCapacity(len(tasks), 1); err != nil {
			return err
		}
		id, err := s.newID("child task id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		child := newBlackboardTask(
			domain.TaskID(id), workItem.ID, &parent.ID, command.Task, nextTaskPosition(tasks), now,
		)
		if err := child.Validate(domain.CoordinationModeBlackboard); err != nil {
			return err
		}
		parent.UpdatedAt = now
		parent.Version++
		claims, err := store.ListClaims(parent.ID)
		if err != nil {
			return fmt.Errorf("list claims for parent task %q: %w", parent.ID, err)
		}
		if err := domain.ValidateTaskContext(domain.CoordinationModeBlackboard, parent, claims); err != nil {
			return err
		}
		prospective := replaceTask(tasks, parent)
		prospective = append(prospective, child)
		if err := domain.ValidateBlackboardTaskHierarchy(workItem.ID, prospective); err != nil {
			return err
		}
		if err := store.SaveTask(parent); err != nil {
			return fmt.Errorf("save aggregate task: %w", err)
		}
		if err := store.CreateTask(child); err != nil {
			return fmt.Errorf("create child task: %w", err)
		}
		if err := advanceBlackboardRevision(store, &workItem, now); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &child.ID, domain.WorkItemEventTaskCreated, string(child.ID), &actor, ""); err != nil {
			return err
		}
		created = child
		return nil
	})
	if err != nil {
		return domain.Task{}, err
	}
	return normalizeTaskCollections(created), nil
}

func (s *Service) completeBlackboardAncestors(
	store WriteStore,
	workItem domain.WorkItem,
	task domain.Task,
	actor *domain.ActorRef,
	now time.Time,
) error {
	if workItem.CoordinationMode() != domain.CoordinationModeBlackboard || task.ParentTaskID == nil {
		return nil
	}
	tasks, err := store.ListTasks(workItem.ID)
	if err != nil {
		return fmt.Errorf("list blackboard hierarchy: %w", err)
	}
	byID := make(map[domain.TaskID]domain.Task, len(tasks))
	for _, existing := range tasks {
		byID[existing.ID] = existing
	}
	parentID := task.ParentTaskID
	for parentID != nil {
		parent, exists := byID[*parentID]
		if !exists {
			return invalidCommand("parent task %q does not exist", *parentID)
		}
		if parent.Status != domain.TaskStatusWaitingChildren || parent.DecomposedAt == nil {
			break
		}
		allEnded := true
		childCount := 0
		for _, child := range byID {
			if child.ParentTaskID == nil || *child.ParentTaskID != parent.ID {
				continue
			}
			childCount++
			if child.Status != domain.TaskStatusCompleted && child.Status != domain.TaskStatusSkipped {
				allEnded = false
				break
			}
		}
		if childCount == 0 || !allEnded {
			break
		}
		parent.Status = domain.TaskStatusCompleted
		parent.CompletedAt = &now
		parent.UpdatedAt = now
		parent.Version++
		claims, err := store.ListClaims(parent.ID)
		if err != nil {
			return fmt.Errorf("list aggregate claims: %w", err)
		}
		if err := domain.ValidateTaskContext(domain.CoordinationModeBlackboard, parent, claims); err != nil {
			return err
		}
		if err := store.SaveTask(parent); err != nil {
			return fmt.Errorf("complete aggregate task: %w", err)
		}
		byID[parent.ID] = parent
		if err := s.appendEvent(store, workItem.ID, &parent.ID, domain.WorkItemEventTaskCompleted, string(parent.ID), actor, "child tasks completed"); err != nil {
			return err
		}
		parentID = parent.ParentTaskID
	}
	finalTasks := make([]domain.Task, 0, len(byID))
	for _, current := range byID {
		finalTasks = append(finalTasks, current)
	}
	return domain.ValidateBlackboardTaskHierarchy(workItem.ID, finalTasks)
}

func advanceBlackboardRevision(store WriteStore, workItem *domain.WorkItem, now time.Time) error {
	workItem.Version++
	workItem.UpdatedAt = now
	if err := workItem.Validate(); err != nil {
		return err
	}
	if err := store.SaveWorkItem(*workItem); err != nil {
		return fmt.Errorf("save blackboard revision: %w", err)
	}
	return nil
}

func replaceTask(tasks []domain.Task, replacement domain.Task) []domain.Task {
	result := append([]domain.Task(nil), tasks...)
	for index := range result {
		if result[index].ID == replacement.ID {
			result[index] = replacement
			break
		}
	}
	return result
}
