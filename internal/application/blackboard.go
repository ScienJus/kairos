package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// CreateBlackboardTaskCommand adds one planned Task to an open Blackboard.
type CreateBlackboardTaskCommand struct {
	WorkItemID  domain.WorkItemID
	Identity    Identity
	OperationID string

	Title              string
	Description        string
	AcceptanceCriteria string
	Executor           domain.ExecutorRequirement
	AllowedRoles       []string
	Tags               []string
}

// CreateBlackboardTask extends the shared plan of an open Blackboard.
func (s *Service) CreateBlackboardTask(ctx context.Context, command CreateBlackboardTaskCommand) (domain.Task, error) {
	if strings.TrimSpace(string(command.WorkItemID)) == "" {
		return domain.Task{}, invalidCommand("work item id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Task{}, err
	}

	var created domain.Task
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_blackboard_task", command, &created, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", command.WorkItemID, err)
		}
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			return conflict("work item %q is not a blackboard", workItem.ID)
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list blackboard tasks: %w", err)
		}
		id, err := s.newID("task id")
		if err != nil {
			return err
		}
		now := s.clock.Now()
		task := newBlackboardTask(
			domain.TaskID(id),
			workItem.ID,
			nil,
			BlackboardTaskSpec{
				Title: command.Title, Description: command.Description,
				AcceptanceCriteria: command.AcceptanceCriteria, Executor: command.Executor,
				AllowedRoles: command.AllowedRoles, Tags: command.Tags,
			},
			nextTaskPosition(tasks),
			now,
		)
		if err := task.Validate(domain.CoordinationModeBlackboard); err != nil {
			return err
		}
		if err := store.CreateTask(task); err != nil {
			return fmt.Errorf("create blackboard task: %w", err)
		}
		if err := advanceBlackboardRevision(store, &workItem, now); err != nil {
			return err
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskCreated, string(task.ID), &actor, ""); err != nil {
			return err
		}
		created = task
		return nil
	})
	if err != nil {
		return domain.Task{}, err
	}
	return normalizeTaskCollections(created), nil
}

// BlackboardTaskSpec describes one new executable Task.
type BlackboardTaskSpec struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Executor           domain.ExecutorRequirement
	AllowedRoles       []string
	Tags               []string
}

func newBlackboardTask(
	id domain.TaskID,
	workItemID domain.WorkItemID,
	parentTaskID *domain.TaskID,
	spec BlackboardTaskSpec,
	position int64,
	now time.Time,
) domain.Task {
	var parent *domain.TaskID
	if parentTaskID != nil {
		value := *parentTaskID
		parent = &value
	}
	return domain.Task{
		ID:                 id,
		WorkItemID:         workItemID,
		ParentTaskID:       parent,
		Status:             domain.TaskStatusPending,
		Title:              strings.TrimSpace(spec.Title),
		Description:        strings.TrimSpace(spec.Description),
		AcceptanceCriteria: strings.TrimSpace(spec.AcceptanceCriteria),
		Executor:           spec.Executor,
		AllowedRoles:       append([]string(nil), spec.AllowedRoles...),
		Tags:               append([]string(nil), spec.Tags...),
		Position:           position,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func nextTaskPosition(tasks []domain.Task) int64 {
	var next int64
	for _, task := range tasks {
		if task.Position >= next {
			next = task.Position + 1
		}
	}
	return next
}

// AddBlackboardRelationCommand records one suggested ordering relation.
type AddBlackboardRelationCommand struct {
	WorkItemID  domain.WorkItemID
	FromTaskID  domain.TaskID
	ToTaskID    domain.TaskID
	Identity    Identity
	OperationID string
}

// AddBlackboardRelation adds one relation while preserving the runtime DAG.
func (s *Service) AddBlackboardRelation(ctx context.Context, command AddBlackboardRelationCommand) (domain.TaskRelation, error) {
	if strings.TrimSpace(string(command.WorkItemID)) == "" ||
		strings.TrimSpace(string(command.FromTaskID)) == "" ||
		strings.TrimSpace(string(command.ToTaskID)) == "" {
		return domain.TaskRelation{}, invalidCommand("work item id, from task id and to task id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.TaskRelation{}, err
	}

	var created domain.TaskRelation
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "add_blackboard_relation", command, &created, func(store WriteStore) error {
		workItem, err := store.GetWorkItem(command.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", command.WorkItemID, err)
		}
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			return conflict("work item %q is not a blackboard", workItem.ID)
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			return conflict("work item %q is %s", workItem.ID, workItem.Status)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return fmt.Errorf("list blackboard tasks: %w", err)
		}
		relations, err := store.ListTaskRelations(workItem.ID)
		if err != nil {
			return fmt.Errorf("list blackboard relations: %w", err)
		}
		now := s.clock.Now()
		relation := domain.TaskRelation{
			WorkItemID: workItem.ID,
			FromTaskID: command.FromTaskID,
			ToTaskID:   command.ToTaskID,
			CreatedAt:  now,
		}
		if err := relation.Validate(); err != nil {
			return err
		}
		if err := domain.ValidateRuntimeTaskGraph(workItem.ID, tasks, append(relations, relation)); err != nil {
			return err
		}
		if err := store.CreateTaskRelation(relation); err != nil {
			return fmt.Errorf("create blackboard relation: %w", err)
		}
		if err := advanceBlackboardRevision(store, &workItem, now); err != nil {
			return err
		}
		actor := command.Identity.Actor
		message := fmt.Sprintf("%s -> %s", relation.FromTaskID, relation.ToTaskID)
		if err := s.appendEvent(store, workItem.ID, &relation.ToTaskID, domain.WorkItemEventRelationAdded, message, &actor, ""); err != nil {
			return err
		}
		created = relation
		return nil
	})
	if err != nil {
		return domain.TaskRelation{}, err
	}
	return created, nil
}

// SkipBlackboardTaskCommand removes a no-longer-useful Task from the active plan.
type SkipBlackboardTaskCommand struct {
	TaskID      domain.TaskID
	Identity    Identity
	OperationID string
	Reason      string
}

// SkipBlackboardTask ends an unclaimed pending Task without a Submission.
func (s *Service) SkipBlackboardTask(ctx context.Context, command SkipBlackboardTaskCommand) (domain.Task, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" {
		return domain.Task{}, invalidCommand("task id is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Task{}, err
	}
	if strings.TrimSpace(command.Reason) == "" {
		return domain.Task{}, invalidCommand("reason is required")
	}

	var skipped domain.Task
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "skip_blackboard_task", command, &skipped, func(store WriteStore) error {
		task, err := store.GetTask(command.TaskID)
		if err != nil {
			return fmt.Errorf("get task %q: %w", command.TaskID, err)
		}
		workItem, err := store.GetWorkItem(task.WorkItemID)
		if err != nil {
			return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
		}
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			return conflict("task %q does not belong to a blackboard", task.ID)
		}
		if workItem.Status != domain.WorkItemStatusOpen || task.Status != domain.TaskStatusPending || task.ActiveClaimID != nil {
			return conflict("task %q is not skippable", task.ID)
		}
		now := s.clock.Now()
		task.Status = domain.TaskStatusSkipped
		task.CompletedAt = &now
		task.UpdatedAt = now
		task.Version++
		claims, err := store.ListClaims(task.ID)
		if err != nil {
			return fmt.Errorf("list claims for task %q: %w", task.ID, err)
		}
		if err := domain.ValidateTaskContext(domain.CoordinationModeBlackboard, task, claims); err != nil {
			return err
		}
		if err := store.SaveTask(task); err != nil {
			return fmt.Errorf("save skipped task: %w", err)
		}
		actor := command.Identity.Actor
		if err := s.appendEvent(store, workItem.ID, &task.ID, domain.WorkItemEventTaskSkipped, string(task.ID), &actor, strings.TrimSpace(command.Reason)); err != nil {
			return err
		}
		if err := s.completeBlackboardAncestors(store, workItem, task, &actor, now); err != nil {
			return err
		}
		if err := s.completeWorkItemIfDone(store, &workItem, &actor); err != nil {
			return err
		}
		skipped = task
		return nil
	})
	if err != nil {
		return domain.Task{}, err
	}
	return normalizeTaskCollections(skipped), nil
}
